package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/subprocess"
	"github.com/liza-mas/liza/internal/toolresult"
)

// Deferred validation (ADR-0204) runs the canonical commands admission
// deferred as one batch at integration HEAD, outside any task session and
// outside the state lock, and bisects a failing command over the merges since
// it last passed. Each command runs in its own lane, a worktree where it is
// prepared, probed and bisected; lanes run concurrently, bounded only by the
// admission classes (D-210). One runner at a time holds an OS lock that every
// child it starts inherits where the platform allows, so no successor can
// touch a lane worktree while a predecessor's process still runs.

const (
	deferredValidationOperation      = "deferred-validation"
	deferredValidationMaxRunsPerCall = 3
	deferredValidationGitTimeout     = 5 * time.Minute
	deferredValidationDirName        = "deferred-validation"
	// IntegrationProgressWaitingDeferredValidation is the stable integration
	// wait on which the orchestrator supervisor retries a pending batch.
	IntegrationProgressWaitingDeferredValidation = "deferred_validation_pending"

	integrationProgressBlockedDeferredFailed    = "deferred_validation_failed"
	integrationProgressBlockedDeferredInfra     = "deferred_validation_infra"
	integrationProgressBlockedDeferredUnsettled = "deferred_validation_unsettled"

	// deferredValidationReasonUnstoppable ends a batch whose step left
	// processes the runner could not establish had ended.
	deferredValidationReasonUnstoppable = "descendants_unstoppable"
	// deferredValidationReasonAdmissionSlotWait ends a batch whose probe found
	// its admission class full until its wait bound (ADR-0209).
	deferredValidationReasonAdmissionSlotWait = "admission_slot_wait_exceeded"
	// deferredValidationReasonAdmissionSlotError ends a batch whose probe could
	// not use its admission slot files.
	deferredValidationReasonAdmissionSlotError = "admission_slot_error"
)

// errDeferredValidationHalted marks a start refused by a halt or stop.
var errDeferredValidationHalted = errors.New("deferred validation halted")

// errDeferredValidationUnstoppable stops every later start once a step's
// descendants could not be shown to have ended.
var errDeferredValidationUnstoppable = errors.New("descendants of a batch step outlived it and could not be stopped")

// errDeferredValidationBatchEnded refuses a lane's start once another lane
// ended the batch; it is a halt like a committed one.
var errDeferredValidationBatchEnded = fmt.Errorf("%w: another lane ended the batch", errDeferredValidationHalted)

// deferredValidationPlatformSupported reports whether the runner can
// establish that a step's descendants ended. Windows cannot: its process
// tree is unreachable by a possibly reused PID once the root exited.
func deferredValidationPlatformSupported() bool { return runtime.GOOS != "windows" }

// deferredValidationHooks are the platform facilities a runner uses; tests
// substitute them per invocation.
type deferredValidationHooks struct {
	reap              func(pid int, wait time.Duration) bool
	platformSupported bool
}

// deferredValidationLockFollowsChildren reports whether a child started with
// the runner lock's descriptor keeps the lock held (Unix). Where it does not
// (Windows), a dead runner's live child would lose the fence, so predecessor
// recovery requires operator settlement. A variable so tests can force either.
var deferredValidationLockFollowsChildren = func(held *filelock.Held) bool { return held.File() != nil }

// startDeferredValidationProcess spawns the detached runner. A variable so
// tests can observe spawning without starting a process.
var startDeferredValidationProcess = spawnDeferredValidationRunner

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
// not complete until a batch at HEAD passed every effective deferred command.
// It does not hold a generation request (D-230). It reads no Git.
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
			return nil, &IntegrationProgressReason{
				Code:     integrationProgressBlockedDeferredFailed,
				TaskIDs:  deferredCulpritTasks(latest),
				Guidance: fmt.Sprintf("deferred-validation batch %d failed at integration HEAD; merge a correction or retire an obsolete command (%s)", latest.ID, brand.Command(deferredValidationOperation, "status")),
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
// effective set is not covered by a passed batch there, no failed batch or
// exhausted attempts at head suppress it (unless forced), and it is either
// periodically due or required by the integration gate.
func deferredValidationWanted(projectRoot string, state *models.State, head string, force, explicit bool) (bool, string, error) {
	ledger := deferredValidationLedger(state)
	commands := ledger.EffectiveCommands()
	if len(commands) == 0 {
		return false, "nothing_deferred", nil
	}
	latest := latestDeferredBatchAt(ledger, head)
	if latest != nil && latest.Covers(commands) {
		return false, "covered", nil
	}
	if !force {
		if latest != nil && latest.Status == models.DeferredValidationBatchFailed {
			return false, "failed_at_head", nil
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
	logFile, err := os.OpenFile(filepath.Join(deferredValidationDir(projectRoot), "runner.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
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

// RunDeferredValidationOptions configures one runner invocation.
type RunDeferredValidationOptions struct {
	// Force retries a batch at a HEAD whose latest batch failed or whose
	// attempts are exhausted.
	Force bool
}

// RunDeferredValidationResult reports what one invocation did.
type RunDeferredValidationResult struct {
	Outcome string                           `json:"outcome"`
	Reason  string                           `json:"reason,omitempty"`
	Batches []models.DeferredValidationBatch `json:"batches,omitempty"`
}

// RunDeferredValidation runs batches while one is wanted at the current
// integration HEAD, at most deferredValidationMaxRunsPerCall per invocation.
func RunDeferredValidation(ctx context.Context, projectRoot string, opts RunDeferredValidationOptions) (*RunDeferredValidationResult, error) {
	return runDeferredValidation(ctx, projectRoot, opts, deferredValidationHooks{
		reap: reapProcessGroup, platformSupported: deferredValidationPlatformSupported(),
	})
}

func runDeferredValidation(ctx context.Context, projectRoot string, opts RunDeferredValidationOptions, hooks deferredValidationHooks) (*RunDeferredValidationResult, error) {
	if !hooks.platformSupported {
		return nil, &PreconditionError{Reason: fmt.Sprintf("deferred-validation batches are not supported on %s: the runner cannot establish that a command's descendants ended; run the batch from a supported host or retire the command", runtime.GOOS)}
	}
	dir := deferredValidationDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	held, acquired, err := filelock.New(filepath.Join(dir, "runner")).TryHold("deferred-validation-run")
	if err != nil {
		return nil, err
	}
	if !acquired {
		return &RunDeferredValidationResult{Outcome: "skipped", Reason: "runner_active"}, nil
	}

	r := &deferredValidationRunner{
		root:     projectRoot,
		dir:      dir,
		bb:       db.For(paths.New(projectRoot).StatePath()),
		lockFile: held.File(),
		outputs:  openDeferredValidationOutputs(projectRoot),
		mask:     acceptanceExecutionMask(os.Environ()),
		follows:  deferredValidationLockFollowsChildren(held),
		reap:     hooks.reap,
	}
	defer func() {
		if r.isOrphaned() {
			// Leave the lock with the surviving descendants: close this
			// process's descriptor without unlocking the description they
			// share. The unsettled ledger record fences successors even if
			// they closed it.
			_ = held.File().Close()
			return
		}
		_ = held.Release()
	}()
	result := &RunDeferredValidationResult{Outcome: "skipped"}
	for run := 0; run < deferredValidationMaxRunsPerCall; run++ {
		state, err := r.bb.Read()
		if err != nil {
			return result, err
		}
		head, err := ResolveIntegrationHEAD(projectRoot, state.Config.IntegrationBranch)
		if err != nil {
			return result, err
		}
		if run > 0 && head == result.Batches[len(result.Batches)-1].Commit {
			// Catch-up follows a moved HEAD only. Retrying the same HEAD at once
			// would repeat the same environment; the next merge or
			// reconciliation reconsiders it within the attempt cap.
			return result, nil
		}
		force := opts.Force && run == 0
		wanted, reason, err := deferredValidationWanted(projectRoot, state, head, force, run == 0)
		if err != nil {
			return result, err
		}
		if !wanted {
			result.Reason = reason
			return result, nil
		}
		token, err := r.claim(head)
		if err != nil {
			if errors.Is(err, errDeferredValidationHalted) {
				result.Reason = "halted"
				return result, nil
			}
			return result, err
		}
		batch := r.execute(ctx, state, head, force)
		published, err := r.publish(token, batch)
		if err != nil {
			return result, err
		}
		result.Outcome = "ran"
		result.Batches = append(result.Batches, published)
		if r.isOrphaned() {
			result.Reason = published.Reason
			return result, &PreconditionError{Reason: fmt.Sprintf("a deferred-validation step left processes that could not be stopped; the runner record stays unsettled: confirm none still runs, then run %s", brand.Command(deferredValidationOperation, "settle"))}
		}
		if published.Status == models.DeferredValidationBatchAborted {
			result.Reason = published.Reason
			return result, nil
		}
	}
	return result, nil
}

type deferredValidationRunner struct {
	root string
	// dir holds the lane worktrees (laneWorktree).
	dir      string
	bb       *db.Blackboard
	lockFile *os.File
	// outputs keeps failed commands' full masked output; nil when the store
	// cannot open, which costs the reference, never the verdict.
	outputs *toolresult.Store
	mask    func(string) string
	follows bool
	reap    func(pid int, wait time.Duration) bool

	// mu serialises the fence below with every process start, so no lane
	// starts a process once another lane set it.
	mu sync.Mutex
	// orphaned is set once a step's descendants could not be shown to have
	// ended; no further process starts and the lock stays with them.
	orphaned bool
	// ended is set once a lane ended the current batch (halt, orphaned step,
	// admission slot abort): no lane starts admitted work any more, while a
	// step already running finishes and keeps its result.
	ended bool
	// abortReason names why a probe ended the current batch when that was
	// its admission slot, not a halt or stop.
	abortReason string
	// waits bounds the lanes' admission slot waits; endWaits cancels it
	// when the batch ends. Running commands never see it.
	waits    context.Context
	endWaits context.CancelFunc
}

func (r *deferredValidationRunner) isOrphaned() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.orphaned
}

// beginBatch clears the per-batch fence; orphaned survives, so no batch
// starts after one.
func (r *deferredValidationRunner) beginBatch(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ended, r.abortReason = false, ""
	r.waits, r.endWaits = context.WithCancel(ctx)
}

// endBatch stops every lane from starting admitted work and cancels slot
// waits. The first non-empty reason is kept.
func (r *deferredValidationRunner) endBatch(orphaned bool, reason string) {
	r.mu.Lock()
	r.ended = true
	r.orphaned = r.orphaned || orphaned
	if r.abortReason == "" {
		r.abortReason = reason
	}
	endWaits := r.endWaits
	r.mu.Unlock()
	if endWaits != nil {
		endWaits()
	}
}

// startUnlessFenced starts cmd unless the fence refuses it: an orphaned step
// refuses every start, an ended batch every admitted one.
func (r *deferredValidationRunner) startUnlessFenced(cmd *exec.Cmd, admit bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.orphaned {
		return errDeferredValidationUnstoppable
	}
	if admit && r.ended {
		return errDeferredValidationBatchEnded
	}
	return cmd.Start()
}

// claim records this invocation's token. A record left by another token is a
// predecessor that did not finish: where the lock follows children, holding
// it proves none of its processes still runs, so the record is cleared as an
// aborted batch; elsewhere recovery refuses until an operator settles it.
func (r *deferredValidationRunner) claim(head string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	err := r.bb.Modify(func(state *models.State) error {
		previous := db.CloneState(state)
		if state.Goal.Integration == nil {
			state.Goal.Integration = &models.IntegrationLifecycle{}
		}
		if state.Goal.Integration.DeferredValidation == nil {
			state.Goal.Integration.DeferredValidation = &models.DeferredValidationLedger{}
		}
		ledger := state.Goal.Integration.DeferredValidation
		if predecessor := ledger.Runner; predecessor != nil {
			if !r.follows || predecessor.Unsettled != "" {
				// An unsettled record is a runner that knew its processes
				// might outlive it; holding the lock proves nothing then.
				return &PreconditionError{Reason: fmt.Sprintf("predecessor deferred-validation runner (pid %d, token %s) did not finish: confirm none of its processes still run, then run %s", predecessor.PID, predecessor.Token, brand.Command(deferredValidationOperation, "settle"))}
			}
			now := time.Now().UTC()
			ledger.AppendBatch(models.DeferredValidationBatch{
				Commit: predecessor.Commit, StartedAt: predecessor.StartedAt, FinishedAt: now,
				Status: models.DeferredValidationBatchAborted, Reason: "runner_died",
			})
			ledger.Runner = nil
		}
		if err := deferredValidationAdmitted(state); err != nil {
			return err
		}
		ledger.Runner = &models.DeferredValidationRunner{Token: token, PID: os.Getpid(), Commit: head, StartedAt: time.Now().UTC()}
		return validateIntegrationLifecycleTransition(previous, state)
	})
	return token, err
}

// publish appends the batch and clears this invocation's runner record. A
// record holding another token means this runner no longer owns publication.
func (r *deferredValidationRunner) publish(token string, batch models.DeferredValidationBatch) (models.DeferredValidationBatch, error) {
	var newCulprits []models.DeferredValidationCulprit
	err := r.bb.Modify(func(state *models.State) error {
		previous := db.CloneState(state)
		ledger := deferredValidationLedger(state)
		if ledger == nil || ledger.Runner == nil || ledger.Runner.Token != token {
			return &PreconditionError{Reason: "deferred-validation runner no longer owns publication"}
		}
		batch = ledger.AppendBatch(batch)
		if r.isOrphaned() {
			// Keep the record as the fence: a successor refuses until an
			// operator confirms nothing survives and settles it.
			ledger.Runner.Unsettled = deferredValidationReasonUnstoppable
		} else {
			ledger.Runner = nil
		}
		newCulprits = recordDeferredValidationDiscoveries(state, batch)
		return validateIntegrationLifecycleTransition(previous, state)
	})
	if err != nil {
		return batch, err
	}
	for _, culprit := range newCulprits {
		_ = alerts.Write(paths.New(r.root).AlertsLogPath(), alerts.Alert{
			Timestamp: time.Now().UTC(),
			Level:     alerts.AlertLevelCritical,
			Category:  "DEFERRED VALIDATION FAILED",
			Message:   deferredCulpritSummary(batch, culprit),
			OnceKey:   fmt.Sprintf("deferred-validation:%d:%s", batch.ID, culprit.CommandSHA256),
		})
	}
	return batch, nil
}

// recordDeferredValidationDiscoveries appends one immediate discovery per
// culprit not already reported, which wakes the orchestrator through the
// existing IMMEDIATE_DISCOVERY route.
func recordDeferredValidationDiscoveries(state *models.State, batch models.DeferredValidationBatch) []models.DeferredValidationCulprit {
	var recorded []models.DeferredValidationCulprit
	for i, culprit := range batch.Culprits {
		owner := culprit.TaskID
		if owner == "" {
			owner = "unattributed"
		}
		if deferredDiscoveryExists(state, owner, culprit.CommandSHA256) {
			continue
		}
		state.Discovered = append(state.Discovered, models.Discovery{
			ID:             fmt.Sprintf("deferred-validation-%d-%d", batch.ID, i+1),
			By:             models.DeferredValidationDiscoveryBy,
			During:         owner,
			Description:    deferredCulpritSummary(batch, culprit),
			Severity:       "high",
			Urgency:        "immediate",
			Recommendation: deferredCulpritRecommendation(culprit),
			Created:        time.Now().UTC(),
			Extra: map[string]any{
				"batch":          batch.ID,
				"batch_commit":   batch.Commit,
				"command_sha256": culprit.CommandSHA256,
				"culprit_task":   owner,
				"merge_commit":   culprit.MergeCommit,
			},
		})
		recorded = append(recorded, culprit)
	}
	return recorded
}

func deferredDiscoveryExists(state *models.State, owner, commandSHA string) bool {
	for _, discovery := range state.Discovered {
		if discovery.By == models.DeferredValidationDiscoveryBy && discovery.Extra["culprit_task"] == owner && discovery.Extra["command_sha256"] == commandSHA {
			return true
		}
	}
	return false
}

func deferredCulpritSummary(batch models.DeferredValidationBatch, culprit models.DeferredValidationCulprit) string {
	command := culprit.CommandSHA256
	for _, result := range batch.Results {
		if result.CommandSHA256 == culprit.CommandSHA256 {
			command = result.Command
		}
	}
	if culprit.TaskID != "" {
		return fmt.Sprintf("Deferred-validation batch %d at %s: %q fails; bisect reproduced a pass→fail transition at task %s (merge %s). Bisect assumes the failure persists once introduced.", batch.ID, batch.Commit, command, culprit.TaskID, culprit.MergeCommit)
	}
	return fmt.Sprintf("Deferred-validation batch %d at %s: %q fails; unattributed (%s).", batch.ID, batch.Commit, command, culprit.Reason)
}

func deferredCulpritRecommendation(culprit models.DeferredValidationCulprit) string {
	status := brand.Command(deferredValidationOperation, "status")
	if culprit.TaskID != "" {
		return fmt.Sprintf("Create a correction for %s that makes the command pass. If the command is obsolete (renamed or removed), ask the operator to retire it. Details: %s", culprit.TaskID, status)
	}
	return fmt.Sprintf("Inspect the batch with %s; if the command is obsolete, ask the operator to retire it, otherwise create a correction for the failing behaviour.", status)
}

// execResult is one bounded, admitted process run. output is masked for
// persistence; raw is for Git metadata reads only and is never persisted.
type execResult struct {
	exitCode int
	output   string
	raw      string
	err      error
	halted   bool
	// orphaned marks the run whose own process group could not be emptied.
	orphaned bool
}

// run runs one process. With admit, the halt/stop check and start happen
// under the shared work-admission lock against a lock-free state snapshot;
// waiting happens outside it. Without admit, it starts unchecked by halt or
// stop: only for a read that finishes a step already started, because a halt
// starts no new work but lets running work finish (D-158). Either way the
// runner's fence is checked atomically with the start (startUnlessFenced).
// The child inherits the runner lock where possible, runs in its own process
// group and is killed on timeout or cancellation. Once it exits, what remains
// of its group is killed and awaited, so no descendant outlives the step that
// started it: releasing the runner lock unlocks the description the
// descendants share. A descendant that leaves the group (setsid) escapes
// this; validation commands must not daemonize. When the group cannot be
// emptied, the step ends as a halt and no later step starts in any lane. The
// step also inherits extra, a probe's admission slots (ADR-0209).
func (r *deferredValidationRunner) run(ctx context.Context, timeout time.Duration, admit bool, build func(context.Context) (*exec.Cmd, error), extra ...*os.File) execResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := build(ctx)
	if err != nil {
		return execResult{exitCode: -1, err: err}
	}
	output := &acceptanceOutputBuffer{remaining: acceptanceOutputLimit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, output
	if r.lockFile != nil {
		cmd.ExtraFiles = []*os.File{r.lockFile}
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, extra...)
	configProcessGroupKill(cmd)
	cmd.WaitDelay = 5 * time.Second
	if admit {
		err = WithWorkAdmissionSharedLock(ctx, r.root, deferredValidationOperation, func() error {
			state, readErr := r.bb.ReadSnapshot()
			if readErr != nil {
				return fmt.Errorf("%w: cannot read state: %w", errDeferredValidationHalted, readErr)
			}
			if admitErr := deferredValidationAdmitted(state); admitErr != nil {
				return admitErr
			}
			return r.startUnlessFenced(cmd, true)
		})
	} else {
		err = r.startUnlessFenced(cmd, false)
	}
	if err != nil {
		halted := errors.Is(err, errDeferredValidationHalted) || errors.Is(err, errDeferredValidationUnstoppable)
		return execResult{exitCode: -1, err: err, halted: halted}
	}
	err = cmd.Wait()
	raw := output.buffer.String()
	result := execResult{exitCode: -1, err: err, output: r.mask(raw), raw: raw}
	if cmd.ProcessState != nil {
		result.exitCode = cmd.ProcessState.ExitCode()
	}
	if !r.reap(cmd.Process.Pid, cmd.WaitDelay) {
		r.endBatch(true, "")
		result.err, result.halted, result.orphaned = errDeferredValidationUnstoppable, true, true
		return result
	}
	if output.overflow {
		result.err = errAcceptanceOutputLimit
	} else if ctx.Err() != nil && err != nil {
		result.err = fmt.Errorf("timed out after %s: %w", timeout, err)
	}
	return result
}

func (r *deferredValidationRunner) git(ctx context.Context, dir string, args ...string) execResult {
	return r.gitRun(ctx, true, dir, args...)
}

// finishingGit is git for a read that finishes a step already started; a
// halt or stop does not refuse it (see run).
func (r *deferredValidationRunner) finishingGit(ctx context.Context, dir string, args ...string) execResult {
	return r.gitRun(ctx, false, dir, args...)
}

func (r *deferredValidationRunner) gitRun(ctx context.Context, admit bool, dir string, args ...string) execResult {
	return r.run(ctx, deferredValidationGitTimeout, admit, func(ctx context.Context) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		cmd.Env = gitenv.Env()
		return cmd, nil
	})
}

// isAncestor is `merge-base --is-ancestor` through the bounded executor. A
// halted or failed probe is returned in res; exit 1 means "not an ancestor".
func (r *deferredValidationRunner) isAncestor(ctx context.Context, ancestor, descendant string) (bool, execResult) {
	res := r.git(ctx, r.root, "merge-base", "--is-ancestor", ancestor, descendant)
	if res.err != nil && !res.halted && res.exitCode == 1 {
		res.err = nil
		return false, res
	}
	return res.err == nil, res
}

// deferralBefore resolves the commit a deferral's merge started from, like
// the package-level deferralBefore, through the bounded executor.
func (r *deferredValidationRunner) deferralBefore(ctx context.Context, deferral models.DeferredValidationDeferral) (string, execResult) {
	if deferral.BeforeCommit != "" {
		return deferral.BeforeCommit, execResult{}
	}
	res := r.git(ctx, r.root, "rev-parse", "--verify", deferral.MergeCommit+"^1")
	return strings.TrimSpace(res.raw), res
}

// laneWorktree is lane i's worktree. The first keeps the single-lane path,
// so a worktree an earlier runner left is reused rather than leaked.
func (r *deferredValidationRunner) laneWorktree(i int) string {
	if i == 0 {
		return filepath.Join(r.dir, "worktree")
	}
	return filepath.Join(r.dir, fmt.Sprintf("worktree-%d", i))
}

// resetLanes recreates n lane worktrees from scratch at commit, so nothing a
// predecessor left survives, and removes lanes a larger batch left. Git's
// worktree bookkeeping is shared, so this runs before any lane starts, one
// worktree at a time.
func (r *deferredValidationRunner) resetLanes(ctx context.Context, commit string, n int) ([]string, execResult) {
	if res := r.git(ctx, r.root, "worktree", "prune"); res.err != nil {
		return nil, res
	}
	extra, err := filepath.Glob(filepath.Join(r.dir, "worktree-*"))
	if err != nil {
		return nil, execResult{exitCode: -1, err: err}
	}
	for _, worktree := range append([]string{r.laneWorktree(0)}, extra...) {
		if res := r.removeWorktree(ctx, worktree); res.err != nil {
			return nil, res
		}
	}
	worktrees := make([]string, n)
	for i := range worktrees {
		worktrees[i] = r.laneWorktree(i)
		if res := r.git(ctx, r.root, "worktree", "add", "--detach", worktrees[i], commit); res.err != nil {
			return nil, res
		}
	}
	return worktrees, execResult{}
}

// removeWorktree removes a lane worktree if it exists.
func (r *deferredValidationRunner) removeWorktree(ctx context.Context, worktree string) execResult {
	if _, err := os.Stat(worktree); err != nil {
		return execResult{}
	}
	res := r.git(ctx, r.root, "worktree", "remove", "--force", worktree)
	if res.err == nil {
		return res
	}
	// A halt may mean a descendant still uses the directory.
	if res.halted {
		return res
	}
	if removeErr := os.RemoveAll(worktree); removeErr != nil {
		return res
	}
	_ = r.git(ctx, r.root, "worktree", "prune")
	return execResult{}
}

// prepareProbe checks out rev in a lane worktree with no residue (ignored
// files included, so dependencies installed for another revision cannot leak
// in), provisions it as task worktrees are, and requires a clean tree at rev.
func (r *deferredValidationRunner) prepareProbe(ctx context.Context, state *models.State, worktree, rev string) execResult {
	if res := r.git(ctx, worktree, "checkout", "--force", "--detach", rev); res.err != nil {
		return res
	}
	if res := r.git(ctx, worktree, "clean", "-ffdx"); res.err != nil {
		return res
	}
	if state.Config.CopyWorktreeEnvFiles {
		ProvisionWorktreeEnvFiles(r.root, worktree)
	}
	if setup := state.Config.PostWorktreeCmd; setup != nil && strings.TrimSpace(*setup) != "" {
		res := r.run(ctx, models.EffectiveDeferredValidationSetupTimeout(state.Config), true, func(ctx context.Context) (*exec.Cmd, error) {
			return shellCommandContext(ctx, *setup, worktree)
		})
		if res.err != nil {
			return res
		}
	}
	return r.checkClean(ctx, worktree, rev, false)
}

// checkClean requires a lane worktree clean at rev. afterStep marks the read
// that finishes a step already run, which a halt does not refuse; the reads
// never write the worktree, its index included.
func (r *deferredValidationRunner) checkClean(ctx context.Context, worktree, rev string, afterStep bool) execResult {
	git := r.git
	if afterStep {
		git = r.finishingGit
	}
	head := git(ctx, worktree, "rev-parse", "HEAD")
	if head.err != nil {
		return head
	}
	if strings.TrimSpace(head.raw) != rev {
		return execResult{exitCode: -1, err: fmt.Errorf("batch worktree HEAD is not %s", rev)}
	}
	status := git(ctx, worktree, "--no-optional-locks", "status", "--short")
	if status.err != nil {
		return status
	}
	if strings.TrimSpace(status.raw) != "" {
		return execResult{exitCode: -1, err: fmt.Errorf("batch worktree has changes: %s", status.output)}
	}
	return execResult{exitCode: 0}
}

// probe runs one command at a prepared lane worktree and classifies it. With
// keepOutput, a failure's full masked output is stored and referenced; bisect
// steps keep only their status, so they store nothing. The command first takes
// the admission slots of config's limits it matches, from the pool canonical
// admission shares; a class that stays full ends the batch (ADR-0209), and an
// ended batch stops the wait.
func (r *deferredValidationRunner) probe(ctx context.Context, config models.Config, worktree, rev string, command models.DeferredValidationCommand, keepOutput bool) (models.DeferredValidationResult, bool) {
	result := models.DeferredValidationResult{Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, ExitCode: -1}
	slots := newAdmissionSlots(command.TimeoutSeconds)
	if err := slots.acquire(r.waits, r.root, models.AdmissionLimitsFor(config, []string{command.Command})); err != nil {
		reason := ""
		var wait *AdmissionSlotWaitError
		switch {
		case !errors.As(err, &wait):
			reason = deferredValidationReasonAdmissionSlotError
		case wait.Err == nil:
			reason = deferredValidationReasonAdmissionSlotWait
		}
		r.endBatch(false, reason)
		result.Status = models.DeferredValidationResultNotRun
		return result, true
	}
	result.StartedAt = time.Now().UTC()
	res := r.run(ctx, time.Duration(command.TimeoutSeconds)*time.Second, true, func(ctx context.Context) (*exec.Cmd, error) {
		return shellCommandContext(ctx, command.Command, worktree)
	}, slots.files()...)
	if res.orphaned {
		slots.abandon() // The slots stay with the descendants, like the runner lock.
	} else {
		slots.release()
	}
	result.FinishedAt = time.Now().UTC()
	if res.halted {
		result.Status = models.DeferredValidationResultNotRun
		return result, true
	}
	result.ExitCode = res.exitCode
	result.Status = models.DeferredValidationResultPassed
	if res.err != nil {
		result.Status = models.DeferredValidationResultFailed
		if errors.Is(res.err, errAcceptanceOutputLimit) {
			// Masking cannot recognize a known secret whose end was never
			// captured: keep none of an overflowed capture.
			result.Excerpt = deferredStateText(r.mask(fmt.Sprintf("%v\noutput omitted: capture exceeded the %d MiB limit", res.err, acceptanceOutputLimit>>20)))
		} else {
			full := r.mask(fmt.Sprintf("%v\n%s", res.err, res.output))
			result.Excerpt = deferredStateText(full)
			if keepOutput {
				result.OutputSHA256 = r.storeOutput(full)
			}
		}
	}
	clean := r.checkClean(ctx, worktree, rev, true)
	if clean.halted {
		result.Status = models.DeferredValidationResultNotRun
		return result, true
	}
	if clean.err != nil {
		result.Status = models.DeferredValidationResultContaminated
		result.Excerpt = deferredStateText(r.mask(clean.err.Error()))
	}
	return result, false
}

// deferredStateTextMarker replaces the middle of runner text over the state
// limit; the tail is kept because test runners summarize failures last.
const deferredStateTextMarker = "\n[... %d bytes omitted ...]\n"

// deferredStateTranscriptOmitted replaces runner text that state would refuse
// as a provider transcript.
const deferredStateTranscriptOmitted = "output omitted from state: it resembles a provider transcript"

// deferredStateText makes runner-composed text one that state accepts: valid
// UTF-8, at most statehygiene.MaxStateTextBytes with head and tail kept, and
// not transcript-shaped. Call it on masked text, so no cut can split a
// secret before masking sees it.
func deferredStateText(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	if limit := statehygiene.MaxStateTextBytes; len(text) > limit {
		// The marker's count never has more digits than len(text).
		keep := limit - len(fmt.Sprintf(deferredStateTextMarker, len(text)))
		head, tail := keep/2, len(text)-(keep-keep/2)
		for head > 0 && !utf8.RuneStart(text[head]) {
			head--
		}
		for tail < len(text) && !utf8.RuneStart(text[tail]) {
			tail++
		}
		text = text[:head] + fmt.Sprintf(deferredStateTextMarker, tail-head) + text[tail:]
	}
	if statehygiene.IsTranscriptPayload(text) {
		return deferredStateTranscriptOmitted
	}
	return text
}

func deferredValidationOutputsDir(projectRoot string) string {
	return filepath.Join(projectRoot, paths.ProjectDirName(), "tool-results")
}

func openDeferredValidationOutputs(projectRoot string) *toolresult.Store {
	store, err := toolresult.New(deferredValidationOutputsDir(projectRoot), toolresult.Config{}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deferred-validation: failed command output will not be stored: %v\n", err)
		return nil
	}
	return store
}

// storeOutput returns full's tool-result hash, or "" when it cannot be kept.
func (r *deferredValidationRunner) storeOutput(full string) string {
	if r.outputs == nil {
		return ""
	}
	hash, err := r.outputs.Persist(full)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deferred-validation: failed command output not stored: %v\n", err)
		return ""
	}
	return hash
}

// execute runs one batch at head, one lane per command. It never returns an
// error: every outcome, including infrastructure failure and halt, is a
// recorded batch. Results and culprits keep the effective command order.
func (r *deferredValidationRunner) execute(ctx context.Context, state *models.State, head string, forced bool) models.DeferredValidationBatch {
	ledger := deferredValidationLedger(state)
	commands := ledger.EffectiveCommands()
	batch := models.DeferredValidationBatch{Commit: head, StartedAt: time.Now().UTC(), Forced: forced}
	r.beginBatch(ctx)
	defer r.endWaits()
	finish := func(status models.DeferredValidationBatchStatus, reason string) models.DeferredValidationBatch {
		for _, command := range commands[len(batch.Results):] {
			batch.Results = append(batch.Results, r.notRun(command))
		}
		r.mu.Lock()
		orphaned, abortReason := r.orphaned, r.abortReason
		r.mu.Unlock()
		if orphaned {
			// Nothing a batch observed is certified once a step's processes
			// may still run.
			status, reason = models.DeferredValidationBatchAborted, deferredValidationReasonUnstoppable
		} else if status == models.DeferredValidationBatchAborted && abortReason != "" {
			reason = abortReason
		}
		batch.Status, batch.Reason, batch.FinishedAt = status, reason, time.Now().UTC()
		return batch
	}
	worktrees, res := r.resetLanes(ctx, head, len(commands))
	if res.err != nil {
		if res.halted {
			return finish(models.DeferredValidationBatchAborted, "halted")
		}
		return finish(models.DeferredValidationBatchInfraFailed, deferredStateText(r.mask(fmt.Sprintf("prepare batch worktree: %v", res.err))))
	}
	lanes := make([]deferredValidationLane, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lanes[i] = r.runLane(ctx, state, worktrees[i], head, command)
			if lanes[i].halted {
				r.endBatch(false, "")
			}
		}()
	}
	wg.Wait()
	status, reason, halted := models.DeferredValidationBatchPassed, "", false
	for _, lane := range lanes {
		batch.Results = append(batch.Results, lane.result)
		if lane.culprit != nil {
			batch.Culprits = append(batch.Culprits, *lane.culprit)
		}
		halted = halted || lane.halted
		switch {
		case lane.result.Status == models.DeferredValidationResultFailed:
			status = models.DeferredValidationBatchFailed
		case lane.result.Status != models.DeferredValidationResultPassed && status == models.DeferredValidationBatchPassed:
			status = models.DeferredValidationBatchInfraFailed
		}
		if reason == "" {
			reason = lane.infra
		}
	}
	if halted {
		return finish(models.DeferredValidationBatchAborted, "halted")
	}
	if status != models.DeferredValidationBatchInfraFailed {
		reason = ""
	}
	return finish(status, reason)
}

func (r *deferredValidationRunner) notRun(command models.DeferredValidationCommand) models.DeferredValidationResult {
	return models.DeferredValidationResult{Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, Status: models.DeferredValidationResultNotRun, ExitCode: -1}
}

// deferredValidationLane is what one lane observed: its command's result at
// head, the culprit when it failed and was bisected, why its preparation
// failed (infra), and whether a halt ended it.
type deferredValidationLane struct {
	result  models.DeferredValidationResult
	culprit *models.DeferredValidationCulprit
	infra   string
	halted  bool
}

// runLane prepares a lane worktree at head, probes the command there and, if
// it failed, bisects it in the same worktree. A failed preparation leaves the
// command not run. A contaminated lane needs no re-preparation: no other
// command shares it, and each bisect step prepares its own revision.
func (r *deferredValidationRunner) runLane(ctx context.Context, state *models.State, worktree, head string, command models.DeferredValidationCommand) deferredValidationLane {
	lane := deferredValidationLane{result: r.notRun(command)}
	if res := r.prepareProbe(ctx, state, worktree, head); res.err != nil {
		if res.halted {
			lane.halted = true
		} else {
			lane.infra = deferredStateText(r.mask(fmt.Sprintf("setup: %v", res.err)))
		}
		return lane
	}
	result, halted := r.probe(ctx, state.Config, worktree, head, command, true)
	if halted {
		lane.halted = true
		return lane
	}
	lane.result = result
	if result.Status == models.DeferredValidationResultFailed {
		culprit, halted := r.bisect(ctx, state, worktree, head, command)
		if halted {
			lane.halted = true
			return lane
		}
		lane.culprit = &culprit
	}
	return lane
}

type bisectCandidate struct {
	taskID string
	commit string
}

// bisect attributes a command that failed at head, probing in the command's
// lane worktree. The baseline is the
// latest batch that passed it, or where its earliest effective deferral's
// merge started. Candidates are forward merges between baseline and head in
// integration ancestry order. A culprit is named only when its predecessor
// was executed and passed and its own commit was executed and failed; under
// non-monotonic history the result is a reproduced transition, not
// necessarily the first.
func (r *deferredValidationRunner) bisect(ctx context.Context, state *models.State, worktree, head string, command models.DeferredValidationCommand) (models.DeferredValidationCulprit, bool) {
	culprit := models.DeferredValidationCulprit{CommandSHA256: command.CommandSHA256}
	unattributed := func(reason string) (models.DeferredValidationCulprit, bool) {
		culprit.TaskID, culprit.MergeCommit, culprit.Unattributed, culprit.Reason = "", "", true, reason
		return culprit, false
	}
	ledger := deferredValidationLedger(state)
	// Every Git read below goes through the bounded, admitted executor: a
	// halt aborts the batch and a hung read ends as an unattributed culprit.
	baseline := ""
	if passed := ledger.LatestPassFor(command.CommandSHA256); passed != nil {
		ancestor, res := r.isAncestor(ctx, passed.Commit, head)
		if res.halted {
			return culprit, true
		}
		if ancestor {
			baseline = passed.Commit
		}
	}
	if baseline == "" {
		halted := false
		earliest := earliestDeferral(ledger.EffectiveDeferrals(command.CommandSHA256), func(ancestor, descendant string) (bool, error) {
			before, res := r.isAncestor(ctx, ancestor, descendant)
			halted = halted || res.halted
			return before, res.err
		})
		if halted {
			return culprit, true
		}
		if earliest != nil {
			before, res := r.deferralBefore(ctx, *earliest)
			if res.halted {
				return culprit, true
			}
			if res.err == nil {
				baseline = before
			}
		}
	}
	if baseline == "" {
		return unattributed("no_baseline")
	}
	ancestor, res := r.isAncestor(ctx, baseline, head)
	if res.halted {
		return culprit, true
	}
	if !ancestor {
		return unattributed("baseline_not_in_ancestry")
	}
	candidates, res := r.bisectCandidates(ctx, state, baseline, head)
	if res.halted {
		return culprit, true
	}
	if res.err != nil {
		return unattributed("history_unavailable")
	}
	probes := map[string]models.DeferredValidationResultStatus{head: models.DeferredValidationResultFailed}
	step := func(rev string) (models.DeferredValidationResultStatus, bool) {
		if status, ok := probes[rev]; ok {
			return status, false
		}
		status := models.DeferredValidationResultUnavailable
		exitCode := -1
		if res := r.prepareProbe(ctx, state, worktree, rev); res.halted {
			return status, true
		} else if res.err == nil {
			result, halted := r.probe(ctx, state.Config, worktree, rev, command, false)
			if halted {
				return status, true
			}
			status, exitCode = result.Status, result.ExitCode
		}
		probes[rev] = status
		culprit.Steps = append(culprit.Steps, models.DeferredValidationProbe{Commit: rev, Status: status, ExitCode: exitCode})
		return status, false
	}
	status, halted := step(baseline)
	if halted {
		return culprit, true
	}
	switch status {
	case models.DeferredValidationResultPassed:
	case models.DeferredValidationResultFailed:
		return unattributed("fails_at_baseline")
	default:
		return unattributed(fmt.Sprintf("setup_unavailable at %s", baseline))
	}
	lo, hi := -1, len(candidates)
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		status, halted := step(candidates[mid].commit)
		if halted {
			return culprit, true
		}
		switch status {
		case models.DeferredValidationResultPassed:
			lo = mid
		case models.DeferredValidationResultFailed:
			hi = mid
		default:
			return unattributed(fmt.Sprintf("setup_unavailable at %s", candidates[mid].commit))
		}
	}
	if hi == len(candidates) {
		return unattributed("fails_after_last_merge")
	}
	// hi-lo == 1: the predecessor (baseline or candidates[lo]) was executed and
	// passed, and candidates[hi] was executed and failed (at head, by the batch).
	culprit.TaskID, culprit.MergeCommit = candidates[hi].taskID, candidates[hi].commit
	return culprit, false
}

// bisectCandidates lists forward merges whose resulting commit lies on the
// ancestry path baseline..head, ordered by that path.
func (r *deferredValidationRunner) bisectCandidates(ctx context.Context, state *models.State, baseline, head string) ([]bisectCandidate, execResult) {
	list := r.git(ctx, r.root, "rev-list", "--ancestry-path", "--reverse", baseline+".."+head)
	if list.err != nil {
		return nil, list
	}
	position := map[string]int{}
	for i, commit := range strings.Fields(list.raw) {
		position[commit] = i
	}
	byCommit := map[string]bisectCandidate{}
	for _, receipt := range state.Goal.Integration.MutationReceipts {
		if _, onPath := position[receipt.AfterCommit]; !onPath || receipt.BeforeCommit == receipt.AfterCommit {
			continue
		}
		forward, res := r.isAncestor(ctx, receipt.BeforeCommit, receipt.AfterCommit)
		if res.halted {
			return nil, res
		}
		if !forward {
			continue
		}
		byCommit[receipt.AfterCommit] = bisectCandidate{taskID: receipt.TaskID, commit: receipt.AfterCommit}
	}
	candidates := make([]bisectCandidate, 0, len(byCommit))
	for _, candidate := range byCommit {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return position[candidates[i].commit] < position[candidates[j].commit] })
	return candidates, execResult{}
}
