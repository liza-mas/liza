package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"gopkg.in/yaml.v3"
)

// acceptanceValidationMismatchReason is loadAcceptanceInput's refusal for a
// task whose validation differs from the reviewed commands. Creation attributes
// that refusal to the validation field; every other refusal to the allocation
// ref.
const acceptanceValidationMismatchReason = "validation must equal the reviewed ordered canonical commands"

// acceptanceCreationCheck is the outcome of running claim's acceptance
// predicate on a task that does not exist yet. The error is built at the
// return site so it reports the caller's current task observation.
type acceptanceCreationCheck struct {
	taskID      string
	ref         string
	refField    string
	commit      string
	reason      string
	unavailable error
	// parentFence digests the parent evidence the predicate read; empty when
	// the predicate did not apply, so there is nothing to fence.
	parentFence string
}

// checkCreatedTaskAcceptance refuses a task that claim would refuse at the
// current integration commit. It adopts nothing and executes nothing; claim and
// submission stay authoritative, because integration can move after creation.
// Git runs here, so callers invoke it outside the state lock.
func checkCreatedTaskAcceptance(root string, state *models.State, task *models.Task) acceptanceCreationCheck {
	check := acceptanceCreationCheck{taskID: task.ID, ref: acceptanceAllocationRef(task), refField: "plan_ref"}
	if task.PlanRef == "" {
		check.refField = "spec_ref"
	}
	if !acceptanceCreationApplies(task) {
		return check
	}
	fence, err := acceptanceParentFence(state, task)
	if err != nil {
		check.unavailable = err
		return check
	}
	check.parentFence = fence
	commit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		check.unavailable = err
		return check
	}
	check.commit = commit
	if _, err := loadAcceptanceInput(root, state, task, commit); err != nil {
		check.reason = err.Error()
		var evidence *AcceptanceEvidenceError
		if errors.As(err, &evidence) {
			check.reason = evidence.Reason
		}
	}
	return check
}

// acceptanceCreationApplies mirrors the gates loadAcceptanceInput applies, before
// any Git read, to a task without an adopted source. Tasks it would admit
// untouched never depend on state or integration being readable at creation.
func acceptanceCreationApplies(task *models.Task) bool {
	ref := acceptanceAllocationRef(task)
	path, _, _ := strings.Cut(ref, "#")
	return task.EffectiveType() == models.TaskTypeCoding && ref != "" && referencecontract.ValidateAcceptancePath(path) == nil
}

// lifecycleError reports a refusal as INVALID_INPUT attributed to one field,
// or an unresolvable integration commit as retryable; nil when admitted.
func (c acceptanceCreationCheck) lifecycleError(operation string, observed *models.Task) error {
	if c.unavailable != nil {
		return &LifecycleError{
			Outcome: NewLifecycleOutcome(operation, observed, models.LifecycleRetryable, "retry", "none"),
			Err:     fmt.Errorf("the acceptance check of task %s could not run; retry: %w", c.taskID, c.unavailable),
		}
	}
	if c.reason == "" {
		return nil
	}
	mask := acceptanceExecutionMask(os.Environ())
	field := c.refField
	if c.reason == acceptanceValidationMismatchReason {
		field = "validation"
	}
	commit := c.commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	message := fmt.Sprintf("task %s acceptance.source (%s %q): %s — refused at creation: claim at integration %s would refuse this task. "+
		"Use the exact heading text of the reviewed section, not a slug, and its ordered validation commands; "+
		"a section carrying an Acceptance Contract is allocated only by its planning transition or a same-pair %s of an allocated child",
		c.taskID, c.refField, c.ref, c.reason, commit, brand.Command("replace-task"))
	return NewLifecycleInvalidInputError(operation, observed, []models.FieldDiagnostic{{
		SchemaVersion: 1,
		Field:         field,
		Constraint:    mask("acceptance.source: " + c.reason),
		ValueClass:    models.FieldValueClassConflict,
		SafeAction:    models.FieldDiagnosticCorrectInput,
	}}, &PreconditionError{Reason: mask(message)})
}

// acceptanceParentFence digests every state input the acceptance predicate
// reads beyond the task itself: each effective parent's full record and the
// proof reaffirmations recorded for those parents. Full records keep it
// conservative, since any change counts.
func acceptanceParentFence(state *models.State, task *models.Task) (string, error) {
	parentIDs := task.EffectiveParentTasks()
	parents := make([]*models.Task, len(parentIDs))
	for i, id := range parentIDs {
		parents[i] = state.FindTask(id)
	}
	var reaffirmations []models.ProofReaffirmation
	for _, r := range state.ProofReaffirmations {
		if slices.Contains(parentIDs, r.ParentTask) {
			reaffirmations = append(reaffirmations, r)
		}
	}
	// The blackboard's own serializer: anything state can hold, it encodes.
	data, err := yaml.Marshal(struct {
		ParentIDs      []string                    `yaml:"parent_ids"`
		Parents        []*models.Task              `yaml:"parents"`
		Reaffirmations []models.ProofReaffirmation `yaml:"reaffirmations"`
	}{parentIDs, parents, reaffirmations})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
