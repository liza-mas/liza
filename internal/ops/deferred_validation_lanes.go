package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/toolresult"
)

// Deferred validation lane and probe execution: bounded process runs, lane
// worktree preparation, output storage and the batch lanes (ADR-0204).

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
// step also inherits extra, a probe's admission slots (ADR-0209). An operator
// abort kills an admitted step and ends it as a halt, so it is recorded not
// run; a finishing read is never killed by it (D-181).
func (r *deferredValidationRunner) run(ctx context.Context, timeout time.Duration, admit bool, build func(context.Context) (*exec.Cmd, error), extra ...*os.File) execResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	kill := r.killContext()
	bound := admit && kill != nil
	// stopKill reports, by returning false, that the abort's kill already
	// fired for this step; called right after Wait, it tells an interrupted
	// execution from one that ended before the abort.
	stopKill := func() bool { return true }
	if bound {
		stopKill = context.AfterFunc(kill, cancel)
	}
	defer stopKill()
	cmd, err := build(ctx)
	if err != nil {
		return execResult{exitCode: acceptanceExitUnknown, err: err}
	}
	output := &acceptanceOutputBuffer{remaining: acceptanceOutputLimit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, output
	if r.lockFile != nil {
		cmd.ExtraFiles = []*os.File{r.lockFile}
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, extra...)
	configProcessGroupKill(cmd)
	cmd.WaitDelay = deferredValidationWaitDelay
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
		// A start refused once the abort fired never ran: it is a halt.
		halted := errors.Is(err, errDeferredValidationHalted) || errors.Is(err, errDeferredValidationUnstoppable) || (bound && kill.Err() != nil)
		return execResult{exitCode: acceptanceExitUnknown, err: err, halted: halted}
	}
	err = cmd.Wait()
	interrupted := !stopKill()
	raw := output.buffer.String()
	result := execResult{exitCode: acceptanceExitUnknown, err: err, output: r.mask(raw), raw: raw}
	if cmd.ProcessState != nil {
		result.exitCode = cmd.ProcessState.ExitCode()
	}
	if !r.reap(cmd.Process.Pid, cmd.WaitDelay) {
		r.endBatch(true, "")
		result.err, result.halted, result.orphaned = errDeferredValidationUnstoppable, true, true
		return result
	}
	if err != nil && interrupted {
		result.halted = true
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
	if res.err != nil && !res.halted && res.exitCode == deferredGitExitNegative {
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
		return filepath.Join(r.dir, deferredLaneWorktreeName)
	}
	return filepath.Join(r.dir, fmt.Sprintf(deferredLaneWorktreeFormat, i))
}

// resetLanes recreates n lane worktrees from scratch at commit, so nothing a
// predecessor left survives, and removes lanes a larger batch left. Git's
// worktree bookkeeping is shared, so this runs before any lane starts, one
// worktree at a time.
func (r *deferredValidationRunner) resetLanes(ctx context.Context, commit string, n int) ([]string, execResult) {
	if res := r.git(ctx, r.root, "worktree", "prune"); res.err != nil {
		return nil, res
	}
	extra, err := filepath.Glob(filepath.Join(r.dir, deferredLaneWorktreeGlob))
	if err != nil {
		return nil, execResult{exitCode: acceptanceExitUnknown, err: err}
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

// deferredValidationSetupAttempts bounds how often a lane runs a failing
// post_worktree_cmd: one retry absorbs a transient failure (D-223).
const deferredValidationSetupAttempts = 2

// prepareProbe checks out rev in a lane worktree with no residue (ignored
// files included, so dependencies installed for another revision cannot leak
// in), provisions it as task worktrees are, and requires a clean tree at rev.
// Lanes prepare one at a time (setupMu).
func (r *deferredValidationRunner) prepareProbe(ctx context.Context, state *models.State, worktree, rev string) execResult {
	r.setupMu.Lock()
	res := r.restoreAndSetUp(ctx, state, worktree, rev)
	r.setupMu.Unlock()
	if res.err != nil {
		return res
	}
	return r.checkClean(ctx, worktree, rev, false)
}

// restoreAndSetUp restores rev, tracked files included, and runs the setup
// command. A failed setup is logged and retried once from a tree restored
// again, so nothing the failed attempt wrote reaches the next.
func (r *deferredValidationRunner) restoreAndSetUp(ctx context.Context, state *models.State, worktree, rev string) execResult {
	for attempt := 1; ; attempt++ {
		if res := r.git(ctx, worktree, "checkout", "--force", "--detach", rev); res.err != nil {
			return res
		}
		if res := r.git(ctx, worktree, "clean", "-ffdx"); res.err != nil {
			return res
		}
		if state.Config.CopyWorktreeEnvFiles {
			ProvisionWorktreeEnvFiles(r.root, worktree)
		}
		setup := state.Config.PostWorktreeCmd
		if setup == nil || strings.TrimSpace(*setup) == "" {
			return execResult{}
		}
		res := r.run(ctx, models.EffectiveDeferredValidationSetupTimeout(state.Config), true, func(ctx context.Context) (*exec.Cmd, error) {
			return shellCommandContext(ctx, *setup, worktree)
		})
		if res.err == nil || res.halted {
			return res
		}
		fmt.Fprintf(r.log, "deferred-validation: setup at %s in %s failed (attempt %d/%d): %s\n",
			rev, filepath.Base(worktree), attempt, deferredValidationSetupAttempts, r.mask(deferredFailureText(res)))
		if attempt == deferredValidationSetupAttempts {
			return res
		}
	}
}

// deferredFailureText is a failed step's error and output. An overflowed
// capture keeps none: masking cannot recognize a known secret whose end was
// never captured.
func deferredFailureText(res execResult) string {
	if errors.Is(res.err, errAcceptanceOutputLimit) {
		return fmt.Sprintf("%v\noutput omitted: capture exceeded the %d MiB limit", res.err, acceptanceOutputLimit>>20)
	}
	return fmt.Sprintf("%v\n%s", res.err, res.output)
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
		return execResult{exitCode: acceptanceExitUnknown, err: fmt.Errorf("batch worktree HEAD is not %s", rev)}
	}
	status := git(ctx, worktree, "--no-optional-locks", "status", "--short")
	if status.err != nil {
		return status
	}
	if strings.TrimSpace(status.raw) != "" {
		return execResult{exitCode: acceptanceExitUnknown, err: fmt.Errorf("batch worktree has changes: %s", status.output)}
	}
	return execResult{exitCode: 0}
}

// probe runs one command at a prepared lane worktree and classifies it. With
// keepOutput, a failure's full masked output is stored and referenced; bisect
// steps keep only their status, so they store nothing. The command first takes
// the admission slots of config's limits it matches, from the pool canonical
// admission shares, queued behind its waiters and ahead of admit (D-218). No
// grace bounds the runner, so it waits until they free (D-223); an ended
// batch, or a halt observed meanwhile, stops the wait.
func (r *deferredValidationRunner) probe(ctx context.Context, config models.Config, worktree, rev string, command models.DeferredValidationCommand, keepOutput bool) (models.DeferredValidationResult, bool) {
	result := models.DeferredValidationResult{Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, ExitCode: acceptanceExitUnknown}
	slots := newQueuedAdmissionSlots()
	stopWatching := r.watchHalt()
	err := slots.acquire(r.waits, r.root, models.AdmissionLimitsFor(config, []string{command.Command}))
	stopWatching()
	if err != nil {
		// A cancelled wait is the batch's end or a halt; anything else is
		// a slot I/O failure.
		reason := ""
		var wait *AdmissionSlotWaitError
		if !errors.As(err, &wait) {
			reason = string(deferredValidationReasonAdmissionSlotError)
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
		full := r.mask(deferredFailureText(res))
		result.Excerpt = deferredStateText(full)
		if keepOutput && !errors.Is(res.err, errAcceptanceOutputLimit) {
			result.OutputSHA256 = r.storeOutput(full)
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

// execute runs one batch at head, one lane per command it neither carries
// forward nor skips as known red. It never returns an error: every outcome,
// including infrastructure failure, halt and operator abort (requested on the
// record holding token), is a recorded batch. Results and culprits keep the
// effective command order.
func (r *deferredValidationRunner) execute(ctx context.Context, state *models.State, head, token string, forced bool) models.DeferredValidationBatch {
	ledger := deferredValidationLedger(state)
	commands := ledger.EffectiveCommands()
	batch := models.DeferredValidationBatch{Commit: head, StartedAt: time.Now().UTC(), Forced: forced}
	r.beginBatch(ctx)
	defer r.endWaits()
	defer r.endKill()
	stopWatching := sync.OnceFunc(r.watchAbort(token))
	defer stopWatching()
	finish := func(status models.DeferredValidationBatchStatus, reason string) models.DeferredValidationBatch {
		// An abort observed at any point before this is the batch's outcome;
		// none is observed after it.
		stopWatching()
		for _, command := range commands[len(batch.Results):] {
			batch.Results = append(batch.Results, r.notRun(command))
		}
		r.mu.Lock()
		orphaned, aborted, abortReason := r.orphaned, r.aborted, r.abortReason
		r.mu.Unlock()
		if orphaned {
			// Nothing a batch observed is certified once a step's processes
			// may still run.
			status, reason = models.DeferredValidationBatchAborted, string(deferredValidationReasonUnstoppable)
		} else if aborted {
			// Finished results stay; the batch is the operator's abort even
			// when no running step needed killing.
			status, reason = models.DeferredValidationBatchAborted, string(deferredValidationReasonOperatorAbort)
		} else if status == models.DeferredValidationBatchAborted && abortReason != "" {
			reason = abortReason
		}
		batch.Status, batch.Reason, batch.FinishedAt = status, reason, time.Now().UTC()
		return batch
	}
	reused, halted := r.carryForward(ctx, state, head, commands, forced)
	if halted {
		return finish(models.DeferredValidationBatchAborted, string(deferredValidationReasonHalted))
	}
	for sha, result := range r.knownRed(state, commands, forced) {
		reused[sha] = result
	}
	var worktrees []string
	if executed := len(commands) - len(reused); executed > 0 {
		var res execResult
		if worktrees, res = r.resetLanes(ctx, head, executed); res.err != nil {
			if res.halted {
				return finish(models.DeferredValidationBatchAborted, string(deferredValidationReasonHalted))
			}
			return finish(models.DeferredValidationBatchInfraFailed, deferredStateText(r.mask(fmt.Sprintf("prepare batch worktree: %v", res.err))))
		}
	}
	lanes := make([]deferredValidationLane, len(commands))
	var wg sync.WaitGroup
	for i, command := range commands {
		if result, ok := reused[command.CommandSHA256]; ok {
			lanes[i] = deferredValidationLane{result: result}
			continue
		}
		worktree := worktrees[0]
		worktrees = worktrees[1:]
		wg.Add(1)
		go func() {
			defer wg.Done()
			lanes[i] = r.runLane(ctx, state, worktree, head, command)
			if lanes[i].halted {
				r.endBatch(false, "")
			}
		}()
	}
	wg.Wait()
	status, reason := models.DeferredValidationBatchPassed, ""
	for _, lane := range lanes {
		batch.Results = append(batch.Results, lane.result)
		if lane.culprit != nil {
			batch.Culprits = append(batch.Culprits, *lane.culprit)
		}
		halted = halted || lane.halted
		switch {
		case lane.result.Status == models.DeferredValidationResultFailed || lane.result.Status == models.DeferredValidationResultKnownRed:
			status = models.DeferredValidationBatchFailed
		case lane.result.Status != models.DeferredValidationResultPassed && lane.result.Status != models.DeferredValidationResultCarriedForward &&
			status == models.DeferredValidationBatchPassed:
			status = models.DeferredValidationBatchInfraFailed
		}
		if reason == "" {
			reason = lane.infra
		}
	}
	if halted {
		return finish(models.DeferredValidationBatchAborted, string(deferredValidationReasonHalted))
	}
	if status != models.DeferredValidationBatchInfraFailed {
		reason = ""
	}
	return finish(status, reason)
}

func (r *deferredValidationRunner) notRun(command models.DeferredValidationCommand) models.DeferredValidationResult {
	return models.DeferredValidationResult{Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, Status: models.DeferredValidationResultNotRun, ExitCode: acceptanceExitUnknown}
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
			lane.infra = deferredStateText(strings.TrimRight(r.mask("setup: "+deferredFailureText(res)), "\n"))
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
