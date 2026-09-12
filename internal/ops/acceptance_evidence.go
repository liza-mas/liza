package ops

import (
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// AcceptanceEvidenceError is a repairable admission precondition, never an
// integration failure. It identifies the task and field requiring correction.
type AcceptanceEvidenceError struct {
	TaskID string
	Field  string
	Reason string
}

func (e *AcceptanceEvidenceError) Error() string {
	message := fmt.Sprintf("task %s %s: %s — correct the evidence and submit, or run %s for a submitted task", e.TaskID, e.Field, e.Reason, brand.Command("update-review-commit", e.TaskID))
	return acceptanceExecutionMask(os.Environ())(message)
}

// Unwrap preserves the existing precondition classification for callers that
// do not need the more specific acceptance field diagnostic.
func (e *AcceptanceEvidenceError) Unwrap() error {
	return &PreconditionError{Reason: e.Error()}
}

func acceptanceError(taskID, field, reason string) error {
	return &AcceptanceEvidenceError{TaskID: taskID, Field: field, Reason: reason}
}

type acceptanceInput struct {
	source        models.AcceptanceSource
	contract      *referencecontract.AcceptanceContract
	specRef       string
	destructiveDB bool
}

// loadAcceptanceInput derives authority from the allocated, independently
// reviewed source at integration. Candidate worktree bytes cannot supply it.
func loadAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	// Planning can consume documents containing coding allocations. Do not
	// adopt those contracts, but never let a type change erase prior adoption.
	if task.EffectiveType() != models.TaskTypeCoding && task.AcceptanceSource == nil {
		return nil, nil
	}
	ref := task.PlanRef
	if ref == "" {
		ref = task.SpecRef
	}
	fail := func(reason string) (*acceptanceInput, error) {
		return nil, acceptanceError(task.ID, "acceptance.source", reason)
	}
	if ref == "" {
		if task.AcceptanceSource != nil {
			return fail("adopted source reference was removed")
		}
		return nil, nil
	}
	path, heading, _ := strings.Cut(ref, "#")
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		// Existing legacy artifact references are validated by their own path.
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return fail("adopted source path is invalid")
	}
	g := git.New(root)
	mode, present, err := g.TreePathMode(integrationCommit, path)
	if err != nil {
		return fail("cannot inspect integration source")
	}
	if !present {
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return fail("adopted source is missing from integration")
	}
	if mode != "100644" && mode != "100755" {
		return fail("source must be a regular committed file")
	}
	content, blob, err := readAcceptanceBlob(root, integrationCommit, path)
	if err != nil {
		return fail(err.Error())
	}
	contract, err := referencecontract.ParseAcceptance(content, heading)
	if err != nil {
		return fail(err.Error())
	}
	if contract == nil {
		if task.AcceptanceSource != nil {
			return fail("adopted acceptance declaration was removed; downgrade is forbidden")
		}
		return nil, nil
	}
	if !reflect.DeepEqual(contract.Validation, task.Validation) {
		return fail("validation must equal the reviewed ordered canonical commands")
	}
	if err := models.ValidateValidationSafety("acceptance.validation", task.Validation, task.DestructiveDB); err != nil {
		return fail(err.Error())
	}
	refs, err := referencecontract.Parse(strings.ReplaceAll(content, "\r\n", "\n"))
	if err != nil || refs == nil {
		return fail("strict Source References are required")
	}
	for _, direct := range refs.DirectReferences {
		revision := direct.EffectiveRevision(refs.SourceRevision)
		span, _, err := readAcceptanceBlob(root, revision, direct.Path)
		if err != nil {
			return fail("cannot resolve pinned reference " + direct.ID)
		}
		if _, err := referencecontract.ExtractSection(span, direct.Heading); err != nil {
			return fail("cannot resolve exact heading for reference " + direct.ID)
		}
	}

	var source *models.AcceptanceSource
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if parent == nil || parent.EffectiveType() != models.TaskTypePlanning || parent.Status != models.TaskStatusMerged || parent.BaseCommit == nil || parent.ReviewCommit == nil || parent.MergeCommit == nil || parent.AssignedTo == nil {
			continue
		}
		approved := parent.ApprovedBy != nil && *parent.ApprovedBy != "" && *parent.ApprovedBy != *parent.AssignedTo
		for _, approval := range parent.Approvals {
			approved = approved || (approval.Agent != "" && approval.Agent != *parent.AssignedTo)
		}
		if !approved {
			continue
		}
		if !validAcceptanceParentHistory(g, parent) {
			continue
		}
		allocated := false
		for _, output := range parent.Output {
			if output.PlanRef == task.PlanRef && output.SpecRef == task.SpecRef && reflect.DeepEqual(output.Validation, task.Validation) && output.DestructiveDB == task.DestructiveDB {
				allocated = true
			}
		}
		if !allocated {
			continue
		}
		ancestor, ancestorErr := g.IsAncestor(*parent.MergeCommit, integrationCommit)
		if ancestorErr != nil || !ancestor {
			continue
		}
		reviewBlob, blobErr := g.BlobOID(*parent.ReviewCommit, path)
		if blobErr != nil || reviewBlob != blob {
			continue
		}
		baseBlob, _ := g.BlobOID(*parent.BaseCommit, path)
		if baseBlob == reviewBlob {
			continue // Approval of another file does not authorize this carrier.
		}
		if source != nil {
			return fail("multiple reviewed parents claim the same allocation")
		}
		source = &models.AcceptanceSource{Ref: ref, Commit: integrationCommit, Blob: blob, ParentTask: parent.ID, ParentReviewCommit: *parent.ReviewCommit}
	}
	if source == nil {
		return fail("requires allocation by a direct independently approved merged planning parent")
	}
	// Keep the original carrier identity when unrelated integration work lands.
	if old := task.AcceptanceSource; old != nil && old.Ref == source.Ref && old.Blob == source.Blob && old.ParentTask == source.ParentTask && old.ParentReviewCommit == source.ParentReviewCommit {
		ancestor, err := g.IsAncestor(old.Commit, integrationCommit)
		if err != nil || !ancestor {
			return fail("adopted source commit is no longer in integration ancestry")
		}
		source.Commit = old.Commit
	}
	return &acceptanceInput{
		source: *source, contract: contract,
		specRef: task.SpecRef, destructiveDB: task.DestructiveDB,
	}, nil
}

// Parent metadata must identify the actual immutable reviewed history, not a
// movable ref or an unrelated commit that happens to contain the same blob.
func validAcceptanceParentHistory(g *git.Git, parent *models.Task) bool {
	for _, commit := range []string{*parent.BaseCommit, *parent.ReviewCommit, *parent.MergeCommit} {
		resolved, err := g.ResolveCommit(commit)
		if err != nil || resolved != commit {
			return false
		}
	}
	for _, pair := range [][2]string{
		{*parent.BaseCommit, *parent.ReviewCommit},
		{*parent.ReviewCommit, *parent.MergeCommit},
	} {
		ancestor, err := g.IsAncestor(pair[0], pair[1])
		if err != nil || !ancestor {
			return false
		}
	}
	return true
}

// readAcceptanceBlob bounds the object before reading it and never consults
// the worktree or follows symlinks. Used for source, proof and manifest objects.
func readAcceptanceBlob(root, commit, path string) (string, string, error) {
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		return "", "", fmt.Errorf("invalid acceptance artifact path")
	}
	g := git.New(root)
	mode, present, err := g.TreePathMode(commit, path)
	if err != nil || !present || (mode != "100644" && mode != "100755") {
		return "", "", fmt.Errorf("acceptance artifact must be a present regular committed file: %s", path)
	}
	blob, err := g.BlobOID(commit, path)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve acceptance artifact: %s", path)
	}
	sizeOutput, err := gitenv.CombinedOutput(root, "cat-file", "-s", blob)
	size, parseErr := strconv.Atoi(strings.TrimSpace(string(sizeOutput)))
	if err != nil || parseErr != nil || size > 256*1024 {
		return "", "", fmt.Errorf("acceptance artifact exceeds 256 KiB or cannot be sized: %s", path)
	}
	content, err := g.ReadBlob(commit, path)
	if err != nil {
		return "", "", fmt.Errorf("cannot read acceptance artifact: %s", path)
	}
	return content, blob, nil
}

func prepareAcceptanceReceipt(root string, task *models.Task, input *acceptanceInput, commit string) (*models.AcceptanceReceipt, error) {
	if input == nil {
		return nil, nil
	}
	path, _, _ := strings.Cut(input.source.Ref, "#")
	sourceBlob, err := git.New(root).BlobOID(commit, path)
	if err != nil || sourceBlob != input.source.Blob {
		return nil, acceptanceError(task.ID, "acceptance.source", "candidate changes the independently reviewed source")
	}
	content, blob, err := readAcceptanceBlob(root, commit, input.contract.Manifest)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.manifest", err.Error())
	}
	manifest, err := referencecontract.ParseAcceptanceManifest(content)
	if err == nil {
		err = referencecontract.ValidateAcceptanceManifest(input.contract, manifest)
	}
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.mappings", err.Error())
	}
	for _, mapping := range manifest.Mappings {
		if mapping.File == "" {
			continue
		}
		mode, present, err := git.New(root).TreePathMode(commit, mapping.File)
		if err != nil || !present || (mode != "100644" && mode != "100755") {
			return nil, acceptanceError(task.ID, "acceptance.mappings["+mapping.ObligationID+"].file", "must name a regular committed file")
		}
	}
	return &models.AcceptanceReceipt{Version: 1, ReviewCommit: commit, Source: input.source, ManifestPath: input.contract.Manifest, ManifestBlob: blob, Mappings: manifest.Mappings}, nil
}

func checkAcceptanceWorktree(root, taskID, commit string) error {
	g := git.New(root)
	head, err := g.GetWorktreeHEAD(taskID)
	if err != nil || head != commit {
		return acceptanceError(taskID, "acceptance.review_commit", "worktree HEAD changed during validation")
	}
	status, err := g.WorktreeStatusShort(g.GetWorktreePath(taskID))
	if err != nil || status != "" {
		return acceptanceError(taskID, "acceptance.worktree", "validation requires no staged, unstaged or untracked changes")
	}
	return nil
}

func executeAcceptanceReceipt(root string, task *models.Task, input *acceptanceInput, commit string) (*models.AcceptanceReceipt, error) {
	receipt, err := prepareAcceptanceReceipt(root, task, input, commit)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	receipt.Commands, err = executeAcceptanceCommands(task.ID, git.New(root).GetWorktreePath(task.ID), input.contract.Validation, input.contract.TimeoutSeconds)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.execution", err.Error())
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Recheck the source, task allocation and candidate identity in the final
// transaction. Commands ran outside the lock; this closes the stale-input gap.
func recheckAcceptanceReceipt(root string, state *models.State, task *models.Task, input *acceptanceInput, receipt *models.AcceptanceReceipt) error {
	integrationCommit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	current, err := loadAcceptanceInput(root, state, task, integrationCommit)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, input) {
		return acceptanceError(task.ID, "acceptance.source", "allocation or reviewed source changed during validation")
	}
	if receipt == nil {
		return nil
	}
	expected, err := prepareAcceptanceReceipt(root, task, current, receipt.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	if !reflect.DeepEqual(expected, receipt) {
		return acceptanceError(task.ID, "acceptance.receipt", "candidate manifest changed during validation")
	}
	return checkAcceptanceWorktree(root, task.ID, receipt.ReviewCommit)
}

func validateAcceptanceForAssignment(root string, state *models.State, task *models.Task) error {
	if task.AcceptanceSource == nil && task.PlanRef == "" && task.SpecRef == "" {
		return nil
	}
	commit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	input, err := loadAcceptanceInput(root, state, task, commit)
	if err != nil || input == nil {
		return err
	}
	receipt := task.AcceptanceReceipt
	if task.ReviewCommit == nil || receipt == nil || receipt.ReviewCommit != *task.ReviewCommit || task.AcceptanceSource == nil || !reflect.DeepEqual(*task.AcceptanceSource, input.source) {
		return acceptanceError(task.ID, "acceptance.receipt", "missing or stale receipt at review commit")
	}
	expected, err := prepareAcceptanceReceipt(root, task, input, *task.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	if !reflect.DeepEqual(expected, receipt) || len(receipt.Commands) != len(input.contract.Validation) {
		return acceptanceError(task.ID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	for i, result := range receipt.Commands {
		commandHash := fmt.Sprintf("%x", sha256.Sum256([]byte(input.contract.Validation[i])))
		if result.CommandSHA256 != commandHash || result.ExitCode != 0 || result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) {
			return acceptanceError(task.ID, "acceptance.receipt.commands", "missing or unsuccessful canonical execution")
		}
	}
	return nil
}
