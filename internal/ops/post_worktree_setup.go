package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

const (
	// postWorktreeSetupAttempts bounds runs per call: one retry absorbs a
	// transient failure (D-74, as D-223 does for deferred lanes).
	postWorktreeSetupAttempts = 2
	// postWorktreeSetupOutputTailBytes caps the output kept per attempt.
	postWorktreeSetupOutputTailBytes = 64 << 10
	// postWorktreeSetupWaitDelay bounds the wait for output pipes after the
	// shell exits, so a background process the command started cannot hold
	// the setup open.
	postWorktreeSetupWaitDelay = 10 * time.Second
	// Outcome record statuses persisted in the worktree's private outcome file.
	worktreeSetupOutcomeOK     = "ok"
	worktreeSetupOutcomeFailed = "failed"
)

var (
	postWorktreeSetupRetryDelayNanos atomic.Int64
	worktreeSetupLockSlackNanos      atomic.Int64
)

func init() {
	postWorktreeSetupRetryDelayNanos.Store(int64(2 * time.Second))
	// A waiter waits for a holder's full setup bound plus this slack (output
	// draining, cleanup, bookkeeping); one that outlasts it fails closed like
	// any other setup failure.
	worktreeSetupLockSlackNanos.Store(int64(time.Minute))
}

// SetWorktreeSetupLockSlackForTest temporarily changes how much longer than its
// own setup bound a setup waits for another setup of the same worktree.
func SetWorktreeSetupLockSlackForTest(slack time.Duration) func() {
	previous := worktreeSetupLockSlackNanos.Swap(int64(slack))
	return func() { worktreeSetupLockSlackNanos.Store(previous) }
}

// SetPostWorktreeSetupRetryDelayForTest temporarily changes the pause before
// the retry.
func SetPostWorktreeSetupRetryDelayForTest(delay time.Duration) func() {
	previous := postWorktreeSetupRetryDelayNanos.Swap(int64(delay))
	return func() { postWorktreeSetupRetryDelayNanos.Store(previous) }
}

// RunPostWorktreeCmd runs the configured post-worktree shell command in the given directory.
// It is idempotent and safe to call on both new and existing worktrees.
//
// Failure returns *PostWorktreeSetupError. Callers fail closed on it: a worktree
// whose setup did not succeed is not build-ready, and handing it to a provider
// session burns agent iterations on environment errors.
//
// Setups of one worktree are serialized across processes, and a failed run is
// retried once (ADR-0117, D-74 amendment). The child's output is kept only in
// the worktree's private output file (see PostWorktreeSetupError); a run that
// passes records an outcome that clears setup degradation it disproves, with
// the caller's authority (nil for operator and CLI paths).
//
// timeout bounds the whole call once it holds the worktree, retry included
// (D-34 amendment); a setup that outlives it is killed and fails closed.
//
// Trust model: the command comes from state.yaml in the project runtime directory in
// the project root. Write access to state.yaml implies write access to the
// repo (same trust boundary as Makefile, .github/workflows/, package.json
// scripts). No additional confirmation gate is needed.
func RunPostWorktreeCmd(projectRoot, cmdStr, dir string, timeout time.Duration, authority *models.AgentAuthority) error {
	artifacts, err := newWorktreeSetupArtifacts(projectRoot, dir)
	if err != nil {
		return newPostWorktreeSetupError(cmdStr, dir, err)
	}
	lock, err := worktreeSetupLock(projectRoot, artifacts.digest)
	if err != nil {
		return newPostWorktreeSetupError(cmdStr, dir, err)
	}
	lockWait := timeout + time.Duration(worktreeSetupLockSlackNanos.Load())
	var setupErr *PostWorktreeSetupError
	var passedSeq uint64
	entered := false
	lockErr := lock.WithTimeout(lockWait).WithLockOperation("worktree-setup", func() error {
		entered = true
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		setupErr, passedSeq = runPostWorktreeSetupLocked(ctx, timeout, cmdStr, dir, artifacts)
		return nil
	})
	if !entered {
		if filelock.IsLockErrorType(lockErr, filelock.LockErrorTimeout) {
			lockErr = fmt.Errorf("another setup in this worktree is still running after %s: %w", lockWait, lockErr)
		}
		return newPostWorktreeSetupError(cmdStr, dir, lockErr)
	}
	if setupErr != nil {
		return setupErr
	}
	if passedSeq > 0 {
		reconcileSetupDegradation(projectRoot, dir, artifacts.outcomePath, passedSeq, authority)
	}
	return nil
}

// runPostWorktreeSetupLocked runs the attempts and records their evidence. It
// returns the failure, or the outcome sequence of a pass (0 when unrecorded).
func runPostWorktreeSetupLocked(ctx context.Context, timeout time.Duration, cmdStr, dir string, artifacts worktreeSetupArtifacts) (*PostWorktreeSetupError, uint64) {
	var failures []postWorktreeSetupAttempt
	var lastErr error
	for attempt := 1; attempt <= postWorktreeSetupAttempts; attempt++ {
		if attempt > 1 && !pauseBeforeSetupRetry(ctx) {
			lastErr = fmt.Errorf("timed out after %s before the retry: %w", timeout, lastErr)
			break
		}
		result := runPostWorktreeSetupAttempt(ctx, timeout, cmdStr, dir, attempt)
		if result.err == nil {
			lastErr = nil
			break
		}
		lastErr = result.err
		failures = append(failures, result)
		log.Printf("WARNING: post-worktree setup attempt %d/%d failed in %s: %s: %v",
			attempt, postWorktreeSetupAttempts, dir, boundPostWorktreeCmd(secretmask.New().MaskText(cmdStr)), result.err)
		if result.final {
			break
		}
	}

	outputPath := ""
	if len(failures) > 0 {
		if err := writePostWorktreeSetupOutput(artifacts.outputPath, failures); err != nil {
			log.Printf("WARNING: post-worktree setup output for %s not kept at %s: %v", dir, artifacts.outputPath, err)
		} else {
			outputPath = artifacts.outputPath
		}
	}
	seq, err := recordWorktreeSetupOutcome(artifacts.outcomePath, lastErr == nil, time.Now().UTC())
	if err != nil {
		log.Printf("WARNING: post-worktree setup outcome for %s not recorded at %s: %v", dir, artifacts.outcomePath, err)
	}
	if lastErr == nil {
		return nil, seq
	}
	setupErr := newPostWorktreeSetupError(cmdStr, dir, lastErr)
	setupErr.OutputPath = outputPath
	setupErr.OutcomeSeq = seq
	setupErr.outcomePath = artifacts.outcomePath
	return setupErr, 0
}

// pauseBeforeSetupRetry waits the retry delay and reports whether the setup
// bound leaves time to retry.
func pauseBeforeSetupRetry(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(time.Duration(postWorktreeSetupRetryDelayNanos.Load()))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}

type postWorktreeSetupAttempt struct {
	attempt int
	at      time.Time
	err     error
	output  *outputTail
	// final marks a failure no retry may follow: the bound expired, or the
	// attempt's processes could not be stopped.
	final bool
}

// runPostWorktreeSetupAttempt runs one attempt in its own process group. On
// Unix the deadline kills the whole group even after the shell exits, so a
// descendant holding the output pipes cannot outlive the bound; a failed or
// timed-out attempt's group is emptied before the caller retries or releases
// the setup lock. Windows can only kill the tree while its root lives
// (TECH_DEBT): a descendant that outlives its shell there is not stopped.
func runPostWorktreeSetupAttempt(ctx context.Context, timeout time.Duration, cmdStr, dir string, attempt int) postWorktreeSetupAttempt {
	result := postWorktreeSetupAttempt{attempt: attempt, at: time.Now().UTC(), output: &outputTail{limit: postWorktreeSetupOutputTailBytes}}
	cmd, err := shellCommandContext(ctx, cmdStr, dir)
	if err != nil {
		result.err = err
		return result
	}
	cmd.Stdout = result.output
	cmd.Stderr = result.output
	configProcessGroupKill(cmd)
	cmd.WaitDelay = postWorktreeSetupWaitDelay
	if err := cmd.Start(); err != nil {
		result.err = err
		if ctx.Err() != nil {
			result.err, result.final = fmt.Errorf("timed out after %s: %w", timeout, err), true
		}
		return result
	}
	pid := cmd.Process.Pid
	groupKill := runtime.GOOS != "windows"
	stopDeadlineKill := func() bool { return true }
	if groupKill {
		killed := make(chan struct{})
		stop := context.AfterFunc(ctx, func() {
			defer close(killed)
			_ = signalProcessGroup(pid, os.Kill)
		})
		// Joins a kill already started, so none outlives the attempt.
		stopDeadlineKill = func() bool {
			if stop() {
				return true
			}
			<-killed
			return false
		}
	}
	result.err = cmd.Wait()
	deadlineKilled := !stopDeadlineKill()
	timedOut := deadlineKilled || (result.err != nil && ctx.Err() != nil)
	if !timedOut && result.err == nil {
		return result
	}
	if timedOut {
		cause := result.err
		if cause == nil {
			cause = ctx.Err()
		}
		result.err, result.final = fmt.Errorf("timed out after %s: %w", timeout, cause), true
	}
	if groupKill && !reapProcessGroup(pid, postWorktreeSetupWaitDelay) {
		result.err, result.final = fmt.Errorf("%w; setup left processes that could not be stopped", result.err), true
	}
	return result
}

// outputTail keeps the last limit bytes written to it. exec serializes writes
// when Stdout and Stderr are the same writer.
type outputTail struct {
	limit     int
	buf       []byte
	truncated bool
}

func (t *outputTail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[over:]
		t.truncated = true
	}
	return len(p), nil
}

// worktreeSetupArtifacts names one worktree's private setup files. The key
// joins the readable basename with a digest of the full path, so worktrees
// sharing a basename keep separate evidence.
//
// Ceiling: one output file (at most two attempt tails) and one outcome record
// per worktree that ever ran setup, never removed. Upgrade trigger: setup
// failures common enough for the directory to matter; remove them with the
// worktree then.
type worktreeSetupArtifacts struct {
	digest      string
	outputPath  string
	outcomePath string
}

func newWorktreeSetupArtifacts(projectRoot, dir string) (worktreeSetupArtifacts, error) {
	resolved := resolvedSetupPath(dir)
	sum := sha256.Sum256([]byte(resolved))
	digest := hex.EncodeToString(sum[:8])
	setupDir := paths.New(projectRoot).WorktreeSetupDir()
	if err := os.MkdirAll(setupDir, 0o700); err != nil {
		return worktreeSetupArtifacts{}, fmt.Errorf("create worktree setup directory: %w", err)
	}
	key := filepath.Base(resolved) + "-" + digest
	return worktreeSetupArtifacts{
		digest:      digest,
		outputPath:  filepath.Join(setupDir, key+".log"),
		outcomePath: filepath.Join(setupDir, key+".outcome"),
	}, nil
}

// resolvedSetupPath is the absolute, symlink-resolved path when it resolves,
// so every caller's spelling of one worktree maps to one key.
func resolvedSetupPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

func worktreeSetupLock(projectRoot, digest string) (*filelock.FileLock, error) {
	purpose := "worktree-setup-" + digest
	lock, err := projectFileLock(projectRoot, purpose)
	if err == nil {
		return lock, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	// Outside a Git repository (focused callers), lock in the runtime directory
	// instead; cross-process serialization still holds.
	return filelock.New(filepath.Join(filepath.Dir(paths.New(projectRoot).WorktreeSetupDir()), purpose)), nil
}

// writePostWorktreeSetupOutput replaces the worktree's output file with the
// failed attempts' tails. The file is private (0600 in a 0700 directory) and is
// never read back into logs, state, anomalies, alerts or error text.
func writePostWorktreeSetupOutput(path string, failures []postWorktreeSetupAttempt) error {
	var b strings.Builder
	for _, failure := range failures {
		fmt.Fprintf(&b, "=== attempt %d/%d at %s: %v\n", failure.attempt, postWorktreeSetupAttempts, failure.at.Format(time.RFC3339Nano), failure.err)
		if failure.output.truncated {
			fmt.Fprintf(&b, "=== output truncated to its last %d bytes\n", postWorktreeSetupOutputTailBytes)
		}
		b.Write(failure.output.buf)
		if n := len(failure.output.buf); n > 0 && failure.output.buf[n-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	return writeWorktreeSetupFile(path, []byte(b.String()))
}

// writeWorktreeSetupFile writes one private setup artifact; a variable so tests
// can fail one write deterministically on every platform.
var writeWorktreeSetupFile = writePrivateFile

func writePrivateFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// worktreeSetupOutcome is a worktree's latest setup outcome. Seq increases by
// one per recorded run; runs are serialized by the worktree setup lock, so a
// higher seq is a later run.
type worktreeSetupOutcome struct {
	Seq  uint64
	OK   bool
	Time time.Time
}

var errMalformedWorktreeSetupOutcome = errors.New("malformed worktree setup outcome record")

func readWorktreeSetupOutcome(path string) (worktreeSetupOutcome, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return worktreeSetupOutcome{}, false, nil
	}
	if err != nil {
		return worktreeSetupOutcome{}, false, err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 {
		return worktreeSetupOutcome{}, false, errMalformedWorktreeSetupOutcome
	}
	seq, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil || seq == 0 {
		return worktreeSetupOutcome{}, false, errMalformedWorktreeSetupOutcome
	}
	var ok bool
	switch fields[1] {
	case worktreeSetupOutcomeOK:
		ok = true
	case worktreeSetupOutcomeFailed:
	default:
		return worktreeSetupOutcome{}, false, errMalformedWorktreeSetupOutcome
	}
	at, err := time.Parse(time.RFC3339Nano, fields[2])
	if err != nil {
		return worktreeSetupOutcome{}, false, errMalformedWorktreeSetupOutcome
	}
	return worktreeSetupOutcome{Seq: seq, OK: ok, Time: at}, true, nil
}

// recordWorktreeSetupOutcome writes the next outcome and returns its seq. A
// previous record that cannot be read, or whose seq cannot advance, records
// nothing: no evidence is better than evidence out of order.
func recordWorktreeSetupOutcome(path string, ok bool, at time.Time) (uint64, error) {
	previous, _, err := readWorktreeSetupOutcome(path)
	if err != nil {
		return 0, err
	}
	if previous.Seq == math.MaxUint64 {
		return 0, fmt.Errorf("worktree setup outcome sequence exhausted")
	}
	seq := previous.Seq + 1
	status := worktreeSetupOutcomeFailed
	if ok {
		status = worktreeSetupOutcomeOK
	}
	if err := writeWorktreeSetupFile(path, fmt.Appendf(nil, "%d %s %s\n", seq, status, at.Format(time.RFC3339Nano))); err != nil {
		return 0, err
	}
	return seq, nil
}

// setupFailureSuperseded reports whether a later recorded run of the failed
// worktree passed. A failure that recorded no outcome, or an outcome that
// cannot be read, is never superseded.
func setupFailureSuperseded(setupErr *PostWorktreeSetupError) bool {
	if setupErr == nil || setupErr.OutcomeSeq == 0 || setupErr.outcomePath == "" {
		return false
	}
	latest, exists, err := readWorktreeSetupOutcome(setupErr.outcomePath)
	return err == nil && exists && latest.OK && latest.Seq > setupErr.OutcomeSeq
}

// reconcileSetupDegradation clears setup degradation a passing run in a task
// worktree disproves. Best-effort: the setup already passed.
func reconcileSetupDegradation(projectRoot, dir, outcomePath string, seq uint64, authority *models.AgentAuthority) {
	taskID := taskIDForWorktree(projectRoot, dir)
	if taskID == "" {
		return
	}
	if err := clearSupersededSetupDegradation(projectRoot, taskID, outcomePath, seq, authority); err != nil {
		log.Printf("WARNING: clearing setup degradation for %s after a passing setup: %v", taskID, err)
	}
}

// taskIDForWorktree returns the task whose canonical worktree dir is, or "".
func taskIDForWorktree(projectRoot, dir string) string {
	resolved := resolvedSetupPath(dir)
	if filepath.Dir(resolved) != resolvedSetupPath(filepath.Join(projectRoot, paths.WorktreesDirName)) {
		return ""
	}
	return filepath.Base(resolved)
}
