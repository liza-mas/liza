package commands

import (
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// warnWriter is the destination for non-fatal validation warnings.
// Defaults to os.Stderr; tests override it to capture output without
// monkey-patching the global stderr (which is not goroutine-safe).
var warnWriter io.Writer = os.Stderr

// SetWarnWriter sets the destination for non-fatal validation warnings.
func SetWarnWriter(w io.Writer) {
	warnWriter = w
}

// ValidateOptions controls live and offline validation behavior.
type ValidateOptions struct {
	SkipSpecFileCheck        bool
	SkipProcessChecks        bool
	Repair                   bool
	WarnWriter               io.Writer
	RecentlySpawnedAgentPIDs []int
}

// ValidateCommand validates the state.yaml file against all schema rules.
// Returns an error with detailed description if validation fails.
func ValidateCommand(statePath string, skipSpecFileCheck bool) error {
	return ValidateCommandWithOptions(statePath, ValidateOptions{SkipSpecFileCheck: skipSpecFileCheck})
}

// ValidateCommandWithOptions validates state.yaml and, by default, verifies
// that no live agent supervisor for this project/goal is missing from
// state.yaml. Process validation is host-local and intentionally skippable for
// archived/offline state validation.
func ValidateCommandWithOptions(statePath string, opts ValidateOptions) error {
	warnings := opts.WarnWriter
	if warnings == nil {
		warnings = warnWriter
	}

	projectRoot := filepath.Dir(filepath.Dir(statePath))
	logPath := filepath.Join(filepath.Dir(statePath), "log.yaml")
	if opts.Repair {
		if !opts.SkipProcessChecks {
			repaired, err := ops.RepairInvalidDoerOwnership(statePath, projectRoot, logPath, "validate --repair")
			if repaired > 0 {
				fmt.Fprintf(warnings, "REPAIRED: invalid active doer ownership cleared for %d task(s); worktrees remain on disk for inspection but may be removed by a later reclaim\n", repaired)
			}
			if err != nil {
				var refused *ops.DoerRepairRefusedError
				if stderrors.As(err, &refused) {
					fmt.Fprintf(warnings, "WARNING: invalid active doer ownership not repaired: %s\n", refused.Error())
				} else {
					return fmt.Errorf("repair invalid doer ownership: %w", err)
				}
			}
		}
		repaired, err := ops.RepairInvalidReviewOwnership(statePath, projectRoot, logPath, "validate --repair")
		if err != nil {
			return fmt.Errorf("repair invalid review ownership: %w", err)
		}
		if repaired > 0 {
			fmt.Fprintf(warnings, "REPAIRED: invalid active review ownership cleared for %d task(s)\n", repaired)
		}
	}

	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		schemaErr := &lizaerrors.StateSchemaError{Operation: "validate", Err: err}
		return &lizaerrors.ValidationError{Message: schemaErr.Error(), Err: schemaErr}
	}

	if err := statevalidate.ValidateState(state, projectRoot, opts.SkipSpecFileCheck, warnings); err != nil {
		return &lizaerrors.ValidationError{Message: err.Error(), Err: err}
	}
	if !opts.SkipSpecFileCheck {
		warnUnresolvedRefFragments(state, projectRoot, warnings)
	}
	if !opts.SkipProcessChecks {
		if err := validateNoZombieAgents(state, projectRoot, warnings, opts.RecentlySpawnedAgentPIDs); err != nil {
			return &lizaerrors.ValidationError{Message: err.Error(), Err: err}
		}
	}
	return nil
}

// warnUnresolvedRefFragments reports ref fragments that prompt context would
// fail to resolve at integration HEAD: scalar refs of tasks still to be worked,
// and output refs of merged tasks whose children are not yet created. This is
// diagnostic only; submission rejects new unresolvable fragments, while a later
// heading edit or pre-existing state can still leave one behind.
func warnUnresolvedRefFragments(state *models.State, projectRoot string, warnings io.Writer) {
	type located struct{ location, ref string }
	var refs []located
	add := func(location string, values ...string) {
		for i, field := range []string{"spec_ref", "epic_ref", "plan_ref", "arch_ref"} {
			if paths.SplitRefFragment(values[i]) != "" {
				refs = append(refs, located{location: location + field, ref: values[i]})
			}
		}
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !task.Status.IsTerminal() {
			add("task "+task.ID+" ", task.SpecRef, task.EpicRef, task.PlanRef, task.ArchRef)
		}
		// Limitation: outputs count as consumed once any transition ran. A custom
		// pipeline with several outgoing transitions from one role pair can hide
		// a pending branch here; resolve pending transitions per task through the
		// pipeline resolver when such a pipeline or a missed warning appears.
		if task.Status != models.TaskStatusMerged || len(task.TransitionsExecuted) > 0 {
			continue
		}
		for j, output := range task.Output {
			add(fmt.Sprintf("task %s output[%d].", task.ID, j), output.SpecRef, output.EpicRef, output.PlanRef, output.ArchRef)
		}
	}
	if len(refs) == 0 || state.Config.IntegrationBranch == "" {
		return
	}
	g := git.New(projectRoot)
	head, err := g.ResolveCommit(state.Config.IntegrationBranch)
	if err != nil {
		fmt.Fprintf(warnings, "WARNING: ref fragment check skipped: cannot resolve integration branch %q: %v\n", state.Config.IntegrationBranch, err)
		return
	}
	for _, r := range refs {
		if err := ops.ResolveRefFragmentAt(g, head, r.ref); err != nil {
			fmt.Fprintf(warnings, "WARNING: %s %q does not resolve at integration HEAD: %v; prompt context for that work will fail to build\n", r.location, r.ref, err)
		}
	}
}

func validateNoZombieAgents(state *models.State, projectRoot string, warnings io.Writer, recentlySpawnedPIDs []int) error {
	scan, err := findZombieAgents(procscan.ZombieScanOptions{
		ProjectRoot:    projectRoot,
		GoalID:         state.Goal.ID,
		RegisteredPIDs: registeredAgentPIDs(state),
	})
	if stderrors.Is(err, procscan.ErrProcessScanUnavailable) {
		fmt.Fprintf(warnings, "WARNING: Live %s agent process scan skipped (procfs unavailable on this host)\n", brand.BinaryName)
		return nil
	}
	if err != nil {
		return fmt.Errorf("scan %s agent processes: %w", brand.BinaryName, err)
	}
	scan.Zombies = filterRecentlySpawnedProcesses(scan.Zombies, recentlySpawnedPIDs)
	scan.UnknownScope = filterRecentlySpawnedProcesses(scan.UnknownScope, recentlySpawnedPIDs)
	writeUnknownScopeWarning(warnings, scan.UnknownScope)
	if len(scan.Zombies) == 0 {
		return nil
	}

	parts := make([]string, 0, len(scan.Zombies))
	for _, zombie := range scan.Zombies {
		role := zombie.Role
		if role == "" {
			role = "unknown"
		}
		parts = append(parts, fmt.Sprintf("pid %d role %s", zombie.PID, role))
	}
	return fmt.Errorf("zombie %s agent process detected: %s not registered in state.yaml (use %q to inspect, or %q for offline validation)", brand.BinaryName, strings.Join(parts, ", "), brand.Command("get", "agents", "--zombies"), brand.Command("validate", "--skip-process-checks"))
}

func filterRecentlySpawnedProcesses(processes []procscan.AgentProcess, recentlySpawnedPIDs []int) []procscan.AgentProcess {
	if len(processes) == 0 || len(recentlySpawnedPIDs) == 0 {
		return processes
	}

	recent := make(map[int]bool, len(recentlySpawnedPIDs))
	for _, pid := range recentlySpawnedPIDs {
		if pid > 0 {
			recent[pid] = true
		}
	}
	if len(recent) == 0 {
		return processes
	}

	filtered := processes[:0]
	for _, process := range processes {
		if recent[process.PID] {
			continue
		}
		filtered = append(filtered, process)
	}
	return filtered
}

func writeUnknownScopeWarning(w io.Writer, processes []procscan.AgentProcess) {
	if w == nil || len(processes) == 0 {
		return
	}

	parts := make([]string, 0, len(processes))
	for _, process := range processes {
		role := process.Role
		if role == "" {
			role = "unknown"
		}
		reason := process.Reason
		if reason == "" {
			reason = "scope_unavailable"
		}
		parts = append(parts, fmt.Sprintf("pid %d role %s (%s)", process.PID, role, reason))
	}

	fmt.Fprintf(w, "WARNING: Live %s agent process scan partial: unable to verify project scope for %s; these processes were not classified as zombies\n", brand.BinaryName, strings.Join(parts, ", "))
}

func validateAgentInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	return statevalidate.ValidateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter)
}

func validateAnomalies(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	return statevalidate.ValidateAnomalies(state, projectRoot, skipSpecFileCheck)
}
