package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const repairArchRefOperation = "repair-arch-ref"

// RepairArchRefResult reports the arch_ref an operator repair set.
type RepairArchRefResult struct {
	TaskID      string   `json:"task_id"`
	ArchRef     string   `json:"arch_ref"`
	Integration string   `json:"integration"`
	Warnings    []string `json:"warnings,omitempty"`
}

// RepairArchRef sets the empty task-level arch_ref of a task nobody has
// started (D-76). No lifecycle path writes it after creation, so a task created
// without one (a replacement predating D-67) could otherwise never regain its
// architecture scope.
//
// The ref must name an exact Scope heading of an artifact on the integration
// branch: reviewed, merged architecture, never a working-tree file. The repair
// only fills a gap: a non-empty arch_ref is reviewed scope and is refused, as
// is any task outside its role pair's initial status or ever claimed. It
// changes no status and wakes no one. The CLI owns the operator boundary.
func RepairArchRef(projectRoot, taskID, archRef, reason string) (*RepairArchRefResult, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if strings.TrimSpace(reason) == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	archRef = paths.NormalizeSpecRef(strings.TrimSpace(archRef))
	if archRef == "" {
		return nil, &PreconditionError{Reason: "arch_ref is required"}
	}
	if paths.SplitRefFragment(archRef) == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("arch_ref %q requires an exact Scope heading fragment; use %s#<exact Scope heading>", archRef, archRef)}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	snapshot, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	branch := snapshot.Config.IntegrationBranch
	if branch == "" {
		return nil, &PreconditionError{Reason: "no integration branch is configured"}
	}
	g := git.New(projectRoot)
	integration, err := g.ResolveCommit(branch)
	if err != nil {
		return nil, fmt.Errorf("resolve integration branch %q: %w", branch, err)
	}
	// Git reads stay outside the state lock; the lock re-checks the branch.
	if err := validateArchScopeAt(g, branch, integration, archRef); err != nil {
		return nil, err
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	now := time.Now().UTC()
	err = bb.Modify(func(state *models.State) error {
		if state.Config.IntegrationBranch != branch {
			return &PreconditionError{Reason: fmt.Sprintf("integration branch changed from %q to %q during the repair; retry", branch, state.Config.IntegrationBranch)}
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if task.ArchRef != "" {
			return &PreconditionError{Reason: fmt.Sprintf("task %s already has arch_ref %q; %s only fills an empty arch_ref", taskID, task.ArchRef, repairArchRefOperation)}
		}
		initial, err := resolver.InitialStatus(task.RolePair)
		if err != nil {
			return err
		}
		if task.Status != initial {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is %s, not its role pair's initial status %s", taskID, task.Status, initial)}
		}
		if !models.UnstartedProviderConsumer(task, resolver) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not unstarted (assigned, leased, holding a worktree or hand-off, or claimed before); %s only repairs a task nobody has started", taskID, repairArchRefOperation)}
		}
		task.ArchRef = archRef
		note := fmt.Sprintf("arch_ref set to %s (validated at %s %s)", archRef, branch, shortSHA(integration))
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  string(models.TaskEventArchRefRepaired),
			Reason: &reason,
			Note:   &note,
			Extra: map[string]any{
				"operation":   repairArchRefOperation,
				"source":      "operator_cli",
				"arch_ref":    archRef,
				"integration": integration,
			},
		})
		return statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr)
	})
	if err != nil {
		return nil, fmt.Errorf("repair arch_ref: %w", err)
	}

	result := &RepairArchRefResult{TaskID: taskID, ArchRef: archRef, Integration: integration}
	if err := log.New(lp.LogPath()).Append(log.Entry{
		Timestamp: now,
		Agent:     "operator",
		Action:    "arch_ref_repaired",
		Task:      &taskID,
		Detail:    fmt.Sprintf("%s: %s", archRef, reason),
	}); err != nil {
		result.Warnings = append(result.Warnings, "arch_ref persisted; activity log write failed. Do not retry the repair.")
	}
	return result, nil
}

// validateArchScopeAt applies the architecture-output admission checks
// (validateOutputRefFragments) to archRef at integration, except that the
// artifact must be present: an absent file is not reviewed architecture.
func validateArchScopeAt(g *git.Git, branch, integration, archRef string) error {
	file, fragment := paths.SplitRefFile(archRef), paths.SplitRefFragment(archRef)
	_, present, err := g.TreePathMode(integration, file)
	if err != nil {
		return fmt.Errorf("inspect %s at %s: %w", file, branch, err)
	}
	if !present {
		return &PreconditionError{Reason: fmt.Sprintf("arch_ref %q: %s is absent at integration branch %s (%s)", archRef, file, branch, shortSHA(integration))}
	}
	err = ResolveRefFragmentAt(g, integration, archRef)
	if err == nil {
		var content string
		if content, err = g.ReadBlob(integration, file); err == nil {
			_, err = referencecontract.ExtractSection(content, fragment)
		}
	}
	if err != nil {
		reason := err.Error()
		var headingErr *referencecontract.HeadingMatchError
		if stderrors.As(err, &headingErr) {
			reason += "; the fragment must be the exact heading text, not a slug"
		}
		return &PreconditionError{Reason: fmt.Sprintf("arch_ref %q at integration branch %s: %s", archRef, branch, reason)}
	}
	return nil
}
