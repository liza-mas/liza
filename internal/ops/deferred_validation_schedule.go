package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/subprocess"
)

// Deferred validation recording, due and gate queries, scheduling and the
// detached runner spawn (ADR-0204).

func deferredValidationDir(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), deferredValidationDirName)
}

// deferredValidationAdmitted refuses new work in a halt mode and, unlike
// RequireWorkAdmitted, also when STOPPED: the detached runner is not an agent
// supervisor that exits on stop.
func deferredValidationAdmitted(state *models.State) error {
	if err := RequireWorkAdmitted(state, deferredValidationOperation); err != nil {
		return fmt.Errorf("%w: %w", errDeferredValidationHalted, err)
	}
	if state.Config.Mode == models.SystemModeStopped {
		return fmt.Errorf("%w: %w", errDeferredValidationHalted, &PreconditionError{Reason: "system is STOPPED; deferred validation starts no work"})
	}
	return nil
}

// recordMergedDeferrals appends the merged task's deferred commands to the
// goal ledger. It runs inside the MERGED transaction, before the receipt can
// be archived; exact commands come from the task's canonical validation.
func recordMergedDeferrals(state *models.State, task *models.Task, mergeCommit string, now time.Time) {
	receipt := task.AcceptanceReceipt
	if receipt == nil || len(receipt.Deferred) == 0 {
		return
	}
	commands := make([]models.DeferredValidationCommand, 0, len(receipt.Deferred))
	for _, deferral := range receipt.Deferred {
		if deferral.Index < 0 || deferral.Index >= len(task.Validation) {
			continue
		}
		commands = append(commands, models.DeferredValidationCommand{
			Command:        task.Validation[deferral.Index],
			CommandSHA256:  deferral.CommandSHA256,
			TimeoutSeconds: deferral.TimeoutSeconds,
			Authority:      deferral.Authority,
		})
	}
	if len(commands) == 0 {
		return
	}
	if state.Goal.Integration == nil {
		state.Goal.Integration = &models.IntegrationLifecycle{}
	}
	lifecycle := state.Goal.Integration
	before := ""
	for i := len(lifecycle.MutationReceipts) - 1; i >= 0; i-- {
		receipt := lifecycle.MutationReceipts[i]
		if receipt.TaskID == task.ID && receipt.AfterCommit == mergeCommit {
			before = receipt.BeforeCommit
			break
		}
	}
	if lifecycle.DeferredValidation == nil {
		lifecycle.DeferredValidation = &models.DeferredValidationLedger{}
	}
	lifecycle.DeferredValidation.Deferrals = append(lifecycle.DeferredValidation.Deferrals, models.DeferredValidationDeferral{
		TaskID: task.ID, MergeCommit: mergeCommit, BeforeCommit: before, RecordedAt: now, Commands: commands,
	})
}

func deferredValidationLedger(state *models.State) *models.DeferredValidationLedger {
	if state == nil || state.Goal.Integration == nil {
		return nil
	}
	return state.Goal.Integration.DeferredValidation
}

// deferredValidationAttemptsAt counts batches at commit that ended without a
// verdict on the code: aborted or infra-failed.
func deferredValidationAttemptsAt(ledger *models.DeferredValidationLedger, commit string) int {
	count := 0
	for _, batch := range ledger.BatchesAt(commit) {
		if batch.Status == models.DeferredValidationBatchAborted || batch.Status == models.DeferredValidationBatchInfraFailed {
			count++
		}
	}
	return count
}

// latestDeferredBatchAt returns the newest batch at commit, or nil.
func latestDeferredBatchAt(ledger *models.DeferredValidationLedger, commit string) *models.DeferredValidationBatch {
	batches := ledger.BatchesAt(commit)
	if len(batches) == 0 {
		return nil
	}
	return &batches[len(batches)-1]
}

// deferredValidationGate is the completion barrier: global integration does
// not complete until a batch at HEAD passed, or carried forward, every
// effective deferred command. It does not hold a generation request (D-230).
// It reads no Git: the runner decided carry-forward when it ran the batch.
func deferredValidationGate(state *models.State, integrationHEAD string) (waiting, blocked *IntegrationProgressReason) {
	ledger := deferredValidationLedger(state)
	commands := ledger.EffectiveCommands()
	if len(commands) == 0 {
		return nil, nil
	}
	if runner := ledger.Runner; runner != nil && runner.Unsettled != "" {
		return nil, &IntegrationProgressReason{
			Code:     integrationProgressBlockedDeferredUnsettled,
			Guidance: fmt.Sprintf("a deferred-validation runner ended with %s; confirm none of its processes still runs, then run %s", runner.Unsettled, brand.Command(deferredValidationOperation, "settle")),
		}
	}
	if latest := latestDeferredBatchAt(ledger, integrationHEAD); latest != nil {
		if latest.Covers(commands) {
			return nil, nil
		}
		if latest.Status == models.DeferredValidationBatchFailed {
			guidance := fmt.Sprintf("deferred-validation batch %d failed at integration HEAD; merge a correction or retire an obsolete command (%s)", latest.ID, brand.Command(deferredValidationOperation, "status"))
			if owners := deferredKnownRedOwners(latest); len(owners) > 0 {
				guidance += fmt.Sprintf("; known red, fix in flight: %s", strings.Join(owners, ", "))
			}
			return nil, &IntegrationProgressReason{
				Code:     integrationProgressBlockedDeferredFailed,
				TaskIDs:  deferredCulpritTasks(latest),
				Guidance: guidance,
			}
		}
		if deferredAbortedByOperator(latest) {
			return nil, &IntegrationProgressReason{
				Code:     integrationProgressBlockedDeferredAborted,
				Guidance: fmt.Sprintf("deferred-validation batch %d was aborted by the operator at integration HEAD; merge, or run %s", latest.ID, brand.Command(deferredValidationOperation, "run", "--force")),
			}
		}
	}
	if deferredValidationAttemptsAt(ledger, integrationHEAD) >= models.DeferredValidationMaxAttemptsPerCommit {
		return nil, &IntegrationProgressReason{
			Code:     integrationProgressBlockedDeferredInfra,
			Guidance: fmt.Sprintf("deferred-validation batches at integration HEAD ended %d times without a verdict; fix the environment, then run %s", models.DeferredValidationMaxAttemptsPerCommit, brand.Command(deferredValidationOperation, "run", "--force")),
		}
	}
	return progressReason(IntegrationProgressWaitingDeferredValidation), nil
}

func deferredCulpritTasks(batch *models.DeferredValidationBatch) []string {
	var tasks []string
	for _, culprit := range batch.Culprits {
		if culprit.TaskID != "" {
			tasks = append(tasks, culprit.TaskID)
		}
	}
	return uniqueSortedStrings(tasks)
}

// deferredKnownRedOwners returns the in-flight fix tasks a batch's known_red
// results name. They are not culprits: the gate's task list names only
// culprits a bisect proved.
func deferredKnownRedOwners(batch *models.DeferredValidationBatch) []string {
	var owners []string
	for _, result := range batch.Results {
		if result.Status == models.DeferredValidationResultKnownRed {
			owners = append(owners, result.OwnedBy)
		}
	}
	return uniqueSortedStrings(owners)
}

// deferredAbortedByOperator reports whether an operator aborted batch.
func deferredAbortedByOperator(batch *models.DeferredValidationBatch) bool {
	return batch.Status == models.DeferredValidationBatchAborted && batch.Reason == string(deferredValidationReasonOperatorAbort)
}

// deferredValidationBaseline is the commit periodic due-ness counts merges
// from: the latest batch whatever its outcome, so a batch that ends without
// passing still spends its period (D-160); otherwise where the earliest
// effective deferral's merge started. Bisect keeps its own lower bound, the
// command's latest pass.
func deferredValidationBaseline(projectRoot string, ledger *models.DeferredValidationLedger, commands []models.DeferredValidationCommand) string {
	if n := len(ledger.Batches); n > 0 {
		return ledger.Batches[n-1].Commit
	}
	var deferrals []models.DeferredValidationDeferral
	for _, command := range commands {
		deferrals = append(deferrals, ledger.EffectiveDeferrals(command.CommandSHA256)...)
	}
	earliest := earliestDeferral(deferrals, git.New(projectRoot).IsAncestor)
	if earliest == nil {
		return ""
	}
	return deferralBefore(projectRoot, *earliest)
}

// earliestDeferral returns the deferral whose merge comes first in
// integration ancestry. Persistence order is not merge order: a merge's
// integration test runs between its ref update and its MERGED write, so a
// later merge can record its deferral first. Merges that ancestry cannot
// order keep their persistence order.
func earliestDeferral(deferrals []models.DeferredValidationDeferral, isAncestor func(ancestor, descendant string) (bool, error)) *models.DeferredValidationDeferral {
	var earliest *models.DeferredValidationDeferral
	for i := range deferrals {
		candidate := &deferrals[i]
		if earliest == nil {
			earliest = candidate
			continue
		}
		if candidate.MergeCommit == earliest.MergeCommit {
			continue
		}
		if before, err := isAncestor(candidate.MergeCommit, earliest.MergeCommit); err == nil && before {
			earliest = candidate
		}
	}
	return earliest
}

// deferralBefore is the integration commit a deferral's merge started from.
func deferralBefore(projectRoot string, deferral models.DeferredValidationDeferral) string {
	if deferral.BeforeCommit != "" {
		return deferral.BeforeCommit
	}
	out, err := gitenv.Output(projectRoot, "rev-parse", "--verify", deferral.MergeCommit+"^1")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// forwardMergesSince counts task-attributed forward integration mutations in
// baseline..head by ancestry, not receipt order: late-persisted receipts count
// and rollback receipts do not.
func forwardMergesSince(projectRoot string, state *models.State, baseline, head string) (int, error) {
	if baseline == "" || baseline == head {
		return 0, nil
	}
	inRange, err := revListSet(projectRoot, baseline+".."+head)
	if err != nil {
		return 0, err
	}
	g := git.New(projectRoot)
	seen := map[string]bool{}
	for _, receipt := range state.Goal.Integration.MutationReceipts {
		if !inRange[receipt.AfterCommit] || seen[receipt.AfterCommit] {
			continue
		}
		if forward, err := g.IsAncestor(receipt.BeforeCommit, receipt.AfterCommit); err != nil || !forward {
			continue
		}
		seen[receipt.AfterCommit] = true
	}
	return len(seen), nil
}

func revListSet(projectRoot string, args ...string) (map[string]bool, error) {
	out, err := gitenv.Output(projectRoot, append([]string{"rev-list"}, args...)...)
	if err != nil {
		return nil, fmt.Errorf("list integration commits: %w", err)
	}
	set := map[string]bool{}
	for _, line := range strings.Fields(string(out)) {
		set[line] = true
	}
	return set, nil
}

// deferredValidationWanted reports whether a batch should start at head: the
// effective set is not covered by a passed batch there, no failed or
// operator-aborted batch or exhausted attempts at head suppress it, and it is
// either periodically due or required by the integration gate. Force
// overrides each of these: it is the
// operator's re-execution, also of a HEAD covered by carried-forward passes
// a wrong scope or inert path admitted (D-230).
func deferredValidationWanted(projectRoot string, state *models.State, head string, force, explicit bool) (bool, string, error) {
	ledger := deferredValidationLedger(state)
	commands := ledger.EffectiveCommands()
	if len(commands) == 0 {
		return false, "nothing_deferred", nil
	}
	latest := latestDeferredBatchAt(ledger, head)
	if !force {
		if latest != nil && latest.Covers(commands) {
			return false, string(deferredValidationSkipCovered), nil
		}
		if latest != nil && latest.Status == models.DeferredValidationBatchFailed {
			return false, string(deferredValidationSkipFailedAtHead), nil
		}
		if latest != nil && deferredAbortedByOperator(latest) {
			return false, string(deferredValidationSkipAborted), nil
		}
		if deferredValidationAttemptsAt(ledger, head) >= models.DeferredValidationMaxAttemptsPerCommit {
			return false, "attempts_exhausted", nil
		}
	}
	if explicit || force {
		return true, "", nil
	}
	merges, err := forwardMergesSince(projectRoot, state, deferredValidationBaseline(projectRoot, ledger, commands), head)
	if err != nil {
		return false, "", err
	}
	if merges >= models.EffectiveBatchEveryMerges(state.Config) {
		return true, "", nil
	}
	if deferredValidationRequired(projectRoot, state, head) {
		return true, "", nil
	}
	return false, "not_due", nil
}

// deferredValidationRequired reports whether the integration gate is waiting
// on a batch at head. An evaluation error means "not known to be required":
// the periodic trigger and the next reconciliation still reconsider.
func deferredValidationRequired(projectRoot string, state *models.State, head string) bool {
	decision, err := EvaluateLiveIntegrationProgress(state, projectRoot)
	return err == nil && decision.DeferredValidationRequest == head
}

// StartDeferredValidationIfDue spawns the detached runner when a batch is
// wanted at integration HEAD and no live runner holds the lock. Callers treat
// failures as warnings: the next merge or reconciliation reconsiders.
func StartDeferredValidationIfDue(projectRoot string) (bool, error) {
	if !deferredValidationPlatformSupported() {
		return false, nil // the runner would refuse; see RunDeferredValidation
	}
	return startDeferredValidationIfDueWith(projectRoot, startDeferredValidationProcess)
}

func startDeferredValidationIfDueWith(projectRoot string, spawn func(string) error) (bool, error) {
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return false, err
	}
	ledger := deferredValidationLedger(state)
	if len(ledger.EffectiveCommands()) == 0 || deferredValidationAdmitted(state) != nil {
		return false, nil
	}
	if ledger.Runner != nil && ledger.Runner.Unsettled != "" {
		return false, nil // the gate names the settle repair
	}
	head, err := ResolveIntegrationHEAD(projectRoot, state.Config.IntegrationBranch)
	if err != nil {
		return false, err
	}
	wanted, _, err := deferredValidationWanted(projectRoot, state, head, false, false)
	if err != nil || !wanted {
		return false, err
	}
	if err := os.MkdirAll(deferredValidationDir(projectRoot), 0o755); err != nil {
		return false, err
	}
	held, acquired, err := filelock.New(filepath.Join(deferredValidationDir(projectRoot), "runner")).TryHold("deferred-validation-probe")
	if err != nil || !acquired {
		return false, err
	}
	if err := held.Release(); err != nil {
		return false, err
	}
	if err := spawn(projectRoot); err != nil {
		if errors.Is(err, errDeferredValidationHalted) {
			// Expected while halted or stopped; resume or the next merge
			// reconsiders, and the orchestrator supervisor polls the wait.
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// spawnDeferredValidationRunner starts `<binary> deferred-validation run` as a
// detached operator-class process: agent identity is removed from its
// environment. The start is admitted against halt and stop like any other.
func spawnDeferredValidationRunner(projectRoot string) error {
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate %s executable: %w", brand.BinaryName, err)
	}
	logFile, err := os.OpenFile(filepath.Join(deferredValidationDir(projectRoot), deferredValidationRunnerLogName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	cmd := exec.Command(binary, deferredValidationOperation, "run")
	cmd.Dir = projectRoot
	cmd.Env = withoutAgentIdentity(os.Environ())
	cmd.Stdout, cmd.Stderr = logFile, logFile
	subprocess.SetDetachedProcessGroup(cmd)
	bb := db.For(paths.New(projectRoot).StatePath())
	err = WithWorkAdmissionSharedLock(context.Background(), projectRoot, deferredValidationOperation, func() error {
		state, readErr := bb.ReadSnapshot()
		if readErr != nil {
			return readErr
		}
		if admitErr := deferredValidationAdmitted(state); admitErr != nil {
			return admitErr
		}
		return cmd.Start()
	})
	if err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func withoutAgentIdentity(environ []string) []string {
	drop := map[string]bool{}
	for _, name := range []string{"AGENT_ID", "AGENT_GENERATION", "TASK_ID"} {
		drop[brand.EnvName(name)] = true
		drop[brand.LegacyEnvName(name)] = true
	}
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		if !drop[key] {
			kept = append(kept, entry)
		}
	}
	return kept
}
