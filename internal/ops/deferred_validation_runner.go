package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/toolresult"
)

// Deferred validation runner lifecycle: the OS-lock claim, halt and operator
// abort watch, and ledger publication (ADR-0204).

// RunDeferredValidationOptions configures one runner invocation.
type RunDeferredValidationOptions struct {
	// Force retries a batch at a HEAD whose latest batch failed, was aborted
	// by the operator or whose attempts are exhausted, and executes every
	// command: nothing is carried forward or skipped as known red.
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
	// The runner may be started by an operator as well as by a supervisor, so
	// it withholds denied host variables itself before any lane runs (ADR-0213).
	if _, err := WithholdDeniedEnvironment(projectRoot); err != nil {
		return nil, err
	}
	if !hooks.platformSupported {
		return nil, &PreconditionError{Reason: fmt.Sprintf("deferred-validation batches are not supported on %s: the runner cannot establish that a command's descendants ended; run the batch from a supported host or retire the command", runtime.GOOS)}
	}
	dir := deferredValidationDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	held, acquired, err := filelock.New(filepath.Join(dir, deferredValidationRunnerLockName)).TryHold(lockLabelDeferredValidationRun)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return &RunDeferredValidationResult{Outcome: string(deferredValidationOutcomeSkipped), Reason: string(deferredValidationSkipRunnerActive)}, nil
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
		log:      hooks.log,
	}
	if r.log == nil {
		r.log = os.Stderr
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
	result := &RunDeferredValidationResult{Outcome: string(deferredValidationOutcomeSkipped)}
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
				result.Reason = string(deferredValidationSkipHalted)
				return result, nil
			}
			return result, err
		}
		batch := r.execute(ctx, state, head, token, force)
		published, err := r.publish(token, batch)
		if err != nil {
			return result, err
		}
		result.Outcome = string(deferredValidationOutcomeRan)
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
	log     io.Writer

	// setupMu makes lanes prepare one at a time: setup commands of one
	// project share tool state (a package manager's cache or temp files)
	// and race when run together (D-223).
	setupMu sync.Mutex

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
	// abortReason names why the current batch ended when that was not a halt
	// or stop: a probe's admission slot, or an operator abort.
	abortReason string
	// waits bounds the lanes' admission slot waits; endWaits cancels it
	// when the batch ends. Running commands never see it.
	waits    context.Context
	endWaits context.CancelFunc
	// aborted is set once the runner observed an operator abort of the
	// current batch, which then ends aborted_by_operator whatever its lanes
	// observed (D-181). kill is cancelled with it; it kills the admitted
	// steps running then, never a finishing read (see run).
	aborted bool
	kill    context.Context
	endKill context.CancelFunc
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
	r.ended, r.abortReason, r.aborted = false, "", false
	r.waits, r.endWaits = context.WithCancel(ctx)
	r.kill, r.endKill = context.WithCancel(context.Background())
}

// killContext is the current batch's kill context, or nil outside a batch.
func (r *deferredValidationRunner) killContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kill
}

// abortBatch ends the batch as an operator abort: the fence refuses new
// admitted starts, slot waits end, and the admitted steps running now are
// killed. Steps already finished keep their results.
func (r *deferredValidationRunner) abortBatch() {
	r.endBatch(false, string(deferredValidationReasonOperatorAbort))
	r.mu.Lock()
	r.aborted = true
	endKill := r.endKill
	r.mu.Unlock()
	if endKill != nil {
		endKill()
	}
}

// watchAbort aborts the batch once the runner record holding token carries an
// operator's abort request. The returned func stops watching and returns
// once the watcher has.
func (r *deferredValidationRunner) watchAbort(token string) (stop func()) {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(deferredValidationHaltPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				state, err := r.bb.ReadSnapshot()
				if err != nil {
					continue
				}
				if ledger := deferredValidationLedger(state); ledger != nil && ledger.Runner != nil && ledger.Runner.Token == token && ledger.Runner.Abort != nil {
					fmt.Fprintf(r.log, "deferred-validation: batch at %s aborted by the operator\n", ledger.Runner.Commit)
					r.abortBatch()
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
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
	buf := make([]byte, deferredValidationTokenBytes)
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
				Status: models.DeferredValidationBatchAborted, Reason: string(deferredValidationReasonRunnerDied),
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
			ledger.Runner.Unsettled = string(deferredValidationReasonUnstoppable)
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
			Category:  deferredValidationAlertCategory,
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
			owner = deferredValidationUnattributedOwner
		}
		if deferredDiscoveryExists(state, owner, culprit.CommandSHA256) {
			continue
		}
		state.Discovered = append(state.Discovered, models.Discovery{
			ID:             fmt.Sprintf("deferred-validation-%d-%d", batch.ID, i+1),
			By:             models.DeferredValidationDiscoveryBy,
			During:         owner,
			Description:    deferredCulpritSummary(batch, culprit),
			Severity:       models.DiscoverySeverityHigh,
			Urgency:        models.DiscoveryUrgencyImmediate,
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

// watchHalt ends the batch once a lane waiting for its admission slots sees a
// halt or stop, which no slot release would end. endBatch sets the fence
// before it cancels the waits, so a slot won after the cancel starts nothing
// even if the system resumed meanwhile. The returned func stops watching and
// returns once the watcher has.
func (r *deferredValidationRunner) watchHalt() (stop func()) {
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		ticker := time.NewTicker(deferredValidationHaltPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if state, err := r.bb.ReadSnapshot(); err == nil && deferredValidationAdmitted(state) != nil {
					r.endBatch(false, "")
					deferredValidationHaltObserved()
					return
				}
			}
		}
	}()
	return func() {
		close(done)
		<-exited
	}
}
