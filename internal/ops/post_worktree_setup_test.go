package ops

import (
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Tests here change package-level setup timing, so they do not run in parallel.

// testSetupTimeout bounds setups that are not testing the bound.
const testSetupTimeout = time.Minute

type setupFixture struct {
	root      string
	statePath string
	worktree  string
	outcome   string
	output    string
}

func newSetupFixture(t *testing.T) setupFixture {
	t.Helper()
	t.Cleanup(SetPostWorktreeSetupRetryDelayForTest(0))
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	for id, generation := range map[string]string{"coder-1": "gen-a", "code-reviewer-1": "gen-b"} {
		state.Agents[id] = models.Agent{Role: strings.TrimSuffix(id, "-1"), Status: models.AgentStatusIdle, Generation: generation, Heartbeat: now, RegisteredAt: now}
	}
	testhelpers.WriteInitialState(t, statePath, state)
	worktree := filepath.Join(root, paths.WorktreesDirName, "task-1")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	artifacts, err := newWorktreeSetupArtifacts(root, worktree)
	if err != nil {
		t.Fatal(err)
	}
	return setupFixture{root: root, statePath: statePath, worktree: worktree, outcome: artifacts.outcomePath, output: artifacts.outputPath}
}

func (f setupFixture) run(t *testing.T, cmd string) *PostWorktreeSetupError {
	t.Helper()
	return f.runWithin(t, cmd, testSetupTimeout)
}

func (f setupFixture) runWithin(t *testing.T, cmd string, timeout time.Duration) *PostWorktreeSetupError {
	t.Helper()
	err := RunPostWorktreeCmd(f.root, cmd, f.worktree, timeout, nil)
	if err == nil {
		return nil
	}
	var setupErr *PostWorktreeSetupError
	if !stderrors.As(err, &setupErr) {
		t.Fatalf("RunPostWorktreeCmd() error = %v, want *PostWorktreeSetupError", err)
	}
	return setupErr
}

func (f setupFixture) health(t *testing.T) map[string]models.AgentHealth {
	t.Helper()
	state, err := db.For(f.statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state.AgentHealth
}

func (f setupFixture) seedHealth(t *testing.T, agentID string, reason AgentDegradedReason, task string, at time.Time) {
	t.Helper()
	if err := db.For(f.statePath).Modify(func(state *models.State) error {
		if state.AgentHealth == nil {
			state.AgentHealth = map[string]models.AgentHealth{}
		}
		state.AgentHealth[agentID] = models.AgentHealth{State: models.AgentHealthDegraded, Role: "coder", DegradedAt: at, Reason: string(reason), LastTask: task, LastError: "seeded"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func (f setupFixture) seedOutcome(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(f.outcome, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f setupFixture) readOutcome(t *testing.T) worktreeSetupOutcome {
	t.Helper()
	outcome, exists, err := readWorktreeSetupOutcome(f.outcome)
	if err != nil || !exists {
		t.Fatalf("outcome record: exists %v, err %v", exists, err)
	}
	return outcome
}

// degrade publishes a setup failure as the claim path does and reports
// whether the agent was degraded.
func (f setupFixture) degrade(t *testing.T, agentID string, setupErr *PostWorktreeSetupError) bool {
	t.Helper()
	degraded := degradeOnPostWorktreeSetupFailure(f.root, "task-1", agentID, nil, setupErr)
	if degraded != nil && !stderrors.Is(degraded, ErrAgentDegraded) {
		t.Fatalf("degrade error = %v, want ErrAgentDegraded or nil", degraded)
	}
	_, recorded := f.health(t)[agentID]
	if recorded != (degraded != nil) {
		t.Fatalf("degraded error %v but health recorded = %v", degraded, recorded)
	}
	return recorded
}

// The load-bearing security property: a secret the child knows and this process
// does not must not reach the error, the degraded record, its anomaly or the
// log. It reaches only the private output file.
//
// DATABASE_URL is chosen deliberately. secretmask recognizes credential-shaped
// key names (API_KEY, *_TOKEN, *_SECRET, *_PASSWORD, ...) and DATABASE_URL
// matches none of them, so any masking-based defense would fail here.
func TestRunPostWorktreeCmd_KeepsChildOutputOutOfDiagnostic(t *testing.T) {
	f := newSetupFixture(t)
	const secret = "postgres://user:hunter2@db.internal/app"
	if err := os.WriteFile(filepath.Join(f.worktree, ".env"), []byte("DATABASE_URL="+secret+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if strings.Contains(strings.Join(os.Environ(), " "), secret) {
		t.Fatal("precondition: the secret must not be in the parent environment")
	}

	// A realistic failing setup command: source the env file, print a connection
	// diagnostic, then fail.
	var setupErr *PostWorktreeSetupError
	logs := captureLogOutput(t, func() {
		setupErr = f.run(t, ". ./.env && echo \"connecting to $DATABASE_URL\" && exit 1")
		if setupErr != nil {
			f.degrade(t, "coder-1", setupErr)
		}
	})
	if setupErr == nil {
		t.Fatal("RunPostWorktreeCmd() error = nil, want failure")
	}

	state, err := db.For(f.statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	surfaces := map[string]string{
		"error":   setupErr.Error(),
		"health":  fmt.Sprintf("%+v", state.AgentHealth),
		"anomaly": fmt.Sprintf("%+v", state.Anomalies),
		"log":     logs,
	}
	for name, text := range surfaces {
		if strings.Contains(text, "hunter2") {
			t.Errorf("%s leaks a child-known credential", name)
		}
	}
	// The diagnostic still identifies what to rerun and where, and where the
	// output is.
	if !strings.Contains(setupErr.Error(), f.worktree) || setupErr.OutputPath != f.output || !strings.Contains(setupErr.Error(), f.output) {
		t.Errorf("Error() = %q, want the worktree and output file %s named", setupErr.Error(), f.output)
	}
	if !strings.Contains(state.AgentHealth["coder-1"].RecoverHint, f.output) {
		t.Errorf("recover hint = %q, want the output file named", state.AgentHealth["coder-1"].RecoverHint)
	}

	data, err := os.ReadFile(f.output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "connecting to "+secret) {
		t.Error("output file lacks the child's output")
	}
	if runtime.GOOS == "windows" {
		return // Windows file modes carry no owner-only permission bits.
	}
	for path, want := range map[string]os.FileMode{f.output: 0o600, filepath.Dir(f.output): 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", path, got, want)
		}
	}
}

func TestRunPostWorktreeCmd_KeepsOnlyTheOutputTail(t *testing.T) {
	f := newSetupFixture(t)
	over := postWorktreeSetupOutputTailBytes + 4096

	setupErr := f.run(t, fmt.Sprintf("printf PREFIX-MARK; head -c %d /dev/zero | tr '\\0' x; printf SUFFIX-MARK; exit 1", over))

	if setupErr == nil {
		t.Fatal("RunPostWorktreeCmd() = nil, want failure")
	}
	data, err := os.ReadFile(f.output)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "PREFIX-MARK") || strings.Count(text, "SUFFIX-MARK") != postWorktreeSetupAttempts {
		t.Errorf("output file kept the prefix or lost a suffix: prefix %v, suffixes %d", strings.Contains(text, "PREFIX-MARK"), strings.Count(text, "SUFFIX-MARK"))
	}
	if strings.Count(text, "output truncated") != postWorktreeSetupAttempts {
		t.Errorf("output file lacks a truncation marker per attempt")
	}
	if max := postWorktreeSetupAttempts * (postWorktreeSetupOutputTailBytes + 512); len(data) > max {
		t.Errorf("output file is %d bytes, want at most %d", len(data), max)
	}
}

func TestRunPostWorktreeCmd_RetryAbsorbsTransientFailure(t *testing.T) {
	f := newSetupFixture(t)

	setupErr := f.run(t, "if [ -f once ]; then exit 0; fi; echo first-attempt-noise; touch once; exit 3")

	if setupErr != nil {
		t.Fatalf("RunPostWorktreeCmd() = %v, want the retry to pass", setupErr)
	}
	data, err := os.ReadFile(f.output)
	if err != nil {
		t.Fatalf("absorbed failure left no output file: %v", err)
	}
	if !strings.Contains(string(data), "attempt 1/2") || !strings.Contains(string(data), "first-attempt-noise") || strings.Contains(string(data), "attempt 2/2") {
		t.Errorf("output file = %q, want only attempt 1's failure", data)
	}
	if got := f.readOutcome(t); !got.OK || got.Seq != 1 {
		t.Errorf("outcome = %+v, want seq 1 ok", got)
	}
}

func TestRunPostWorktreeCmd_PersistentFailureRunsTwoAttempts(t *testing.T) {
	f := newSetupFixture(t)

	setupErr := f.run(t, "echo run >> attempts; exit 4")

	if setupErr == nil {
		t.Fatal("RunPostWorktreeCmd() = nil, want failure")
	}
	data, err := os.ReadFile(filepath.Join(f.worktree, "attempts"))
	if err != nil || strings.Count(string(data), "run") != postWorktreeSetupAttempts {
		t.Errorf("attempts = %q (%v), want %d runs", data, err, postWorktreeSetupAttempts)
	}
	if !strings.Contains(setupErr.Err.Error(), "exit status 4") || setupErr.OutputPath != f.output || setupErr.OutcomeSeq != 1 {
		t.Errorf("setup error = %+v, want exit 4, output path and outcome seq 1", setupErr)
	}
	if got := f.readOutcome(t); got.OK || got.Seq != 1 {
		t.Errorf("outcome = %+v, want seq 1 failed", got)
	}
}

func TestRunPostWorktreeCmd_SerializesOneWorktreeOnly(t *testing.T) {
	f := newSetupFixture(t)
	other := filepath.Join(f.root, paths.WorktreesDirName, "task-2")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	marks := t.TempDir()

	// Same worktree: a run that finds another in progress records an overlap.
	sameCmd := "if [ -e running ]; then touch overlapped; fi; touch running; sleep 0.3; rm running"
	// Two worktrees: each waits (up to 5 s) for the other to start, and records
	// when it had to give up, which only serialization causes.
	pairCmd := fmt.Sprintf(`touch '%[1]s'/"$(basename "$PWD")"; i=0; while [ "$(ls '%[1]s' | wc -l)" -lt 2 ] && [ $i -lt 50 ]; do sleep 0.1; i=$((i+1)); done; [ "$(ls '%[1]s' | wc -l)" -ge 2 ] || touch gave-up`, filepath.ToSlash(marks))
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for _, call := range []struct{ cmd, dir string }{{sameCmd, f.worktree}, {sameCmd, f.worktree}, {pairCmd, other}, {pairCmd, f.root}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- RunPostWorktreeCmd(f.root, call.cmd, call.dir, testSetupTimeout, nil)
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("RunPostWorktreeCmd() = %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "overlapped")); err == nil {
		t.Error("two setups of one worktree overlapped")
	}
	for _, dir := range []string{other, f.root} {
		if _, err := os.Stat(filepath.Join(dir, "gave-up")); err == nil {
			t.Errorf("setups of different worktrees were serialized (%s waited out)", dir)
		}
	}
}

func TestRunPostWorktreeCmd_LockTimeoutDegradesDespiteOlderSuccess(t *testing.T) {
	f := newSetupFixture(t)
	f.seedOutcome(t, "5 ok "+time.Now().UTC().Format(time.RFC3339Nano)+"\n")
	t.Cleanup(SetWorktreeSetupLockSlackForTest(100 * time.Millisecond))
	artifacts, err := newWorktreeSetupArtifacts(f.root, f.worktree)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := worktreeSetupLock(f.root, artifacts.digest)
	if err != nil {
		t.Fatal(err)
	}
	held, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- lock.WithLock(func() error { close(held); <-release; return nil })
	}()
	<-held

	// The waiter gives up after its own bound plus the slack.
	setupErr := f.runWithin(t, "touch ran", 100*time.Millisecond)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if setupErr == nil || setupErr.OutcomeSeq != 0 || !strings.Contains(setupErr.Error(), "still running") {
		t.Fatalf("setup error = %v, want a lock timeout with no outcome seq", setupErr)
	}
	if _, err := os.Stat(filepath.Join(f.worktree, "ran")); err == nil {
		t.Error("command ran without the setup lock")
	}
	if got := f.readOutcome(t); !got.OK || got.Seq != 5 {
		t.Errorf("outcome = %+v, want the seeded seq 5 ok untouched", got)
	}
	if !f.degrade(t, "coder-1", setupErr) {
		t.Error("an older success suppressed a lock-timeout degrade")
	}
}

func TestRunPostWorktreeCmd_UnrecordedFailureDegradesDespiteOlderSuccess(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, f setupFixture){
		"outcome write fails": func(t *testing.T, f setupFixture) {
			f.seedOutcome(t, "5 ok "+time.Now().UTC().Format(time.RFC3339Nano)+"\n")
			previous := writeWorktreeSetupFile
			writeWorktreeSetupFile = func(path string, data []byte) error {
				if path == f.outcome {
					return errString("injected outcome write failure")
				}
				return previous(path, data)
			}
			t.Cleanup(func() { writeWorktreeSetupFile = previous })
		},
		"outcome record corrupt": func(t *testing.T, f setupFixture) {
			f.seedOutcome(t, "not a record\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newSetupFixture(t)
			prepare(t, f)

			setupErr := f.run(t, "exit 1")

			if setupErr == nil || setupErr.OutcomeSeq != 0 || !strings.Contains(setupErr.Err.Error(), "exit status 1") {
				t.Fatalf("setup error = %v, want the command's failure with no outcome seq", setupErr)
			}
			if !f.degrade(t, "coder-1", setupErr) {
				t.Error("an unrecorded failure was not degraded")
			}
		})
	}
}

func TestPostWorktreeSetupOrdering(t *testing.T) {
	const passWhenMarked = "[ -f pass ]"
	mark := func(t *testing.T, f setupFixture) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.worktree, "pass"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	unmark := func(t *testing.T, f setupFixture) {
		t.Helper()
		if err := os.Remove(filepath.Join(f.worktree, "pass")); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("a failure published after a later pass is not degraded", func(t *testing.T) {
		f := newSetupFixture(t)
		failed := f.run(t, passWhenMarked)
		mark(t, f)
		if err := f.run(t, passWhenMarked); err != nil {
			t.Fatal(err)
		}
		if failed == nil || failed.OutcomeSeq >= f.readOutcome(t).Seq {
			t.Fatalf("failure %+v, want an outcome seq below the pass's", failed)
		}
		if f.degrade(t, "coder-1", failed) {
			t.Error("degraded for a failure a later setup disproved")
		}
	})

	t.Run("a failure after an earlier pass is degraded", func(t *testing.T) {
		f := newSetupFixture(t)
		mark(t, f)
		if err := f.run(t, passWhenMarked); err != nil {
			t.Fatal(err)
		}
		unmark(t, f)
		failed := f.run(t, passWhenMarked)
		if !f.degrade(t, "coder-1", failed) {
			t.Error("an earlier success suppressed a later failure")
		}
	})

	t.Run("an ok record not later than the failure does not supersede it", func(t *testing.T) {
		f := newSetupFixture(t)
		failed := f.run(t, passWhenMarked)
		if failed == nil || failed.OutcomeSeq == 0 {
			t.Fatalf("failure %+v, want a recorded outcome seq", failed)
		}
		f.seedOutcome(t, fmt.Sprintf("%d ok %s\n", failed.OutcomeSeq, time.Now().UTC().Format(time.RFC3339Nano)))
		if !f.degrade(t, "coder-1", failed) {
			t.Error("an ok record at the failure's own seq suppressed it")
		}
	})

	t.Run("a published failure is cleared by a later pass", func(t *testing.T) {
		f := newSetupFixture(t)
		failed := f.run(t, passWhenMarked)
		if !f.degrade(t, "coder-1", failed) {
			t.Fatal("failure not degraded")
		}
		mark(t, f)
		if err := f.run(t, passWhenMarked); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.health(t)["coder-1"]; ok {
			t.Error("a passing setup left the disproved degradation")
		}
	})

	t.Run("a delayed reconcile keeps a later failure", func(t *testing.T) {
		f := newSetupFixture(t)
		mark(t, f)
		if err := f.run(t, passWhenMarked); err != nil {
			t.Fatal(err)
		}
		passSeq := f.readOutcome(t).Seq
		unmark(t, f)
		if !f.degrade(t, "coder-1", f.run(t, passWhenMarked)) {
			t.Fatal("later failure not degraded")
		}

		if err := clearSupersededSetupDegradation(f.root, "task-1", f.outcome, passSeq, nil); err != nil {
			t.Fatal(err)
		}
		if _, ok := f.health(t)["coder-1"]; !ok {
			t.Error("a delayed reconcile removed a later failure's degradation")
		}
	})

	t.Run("a pass clears only earlier setup degradation of its task", func(t *testing.T) {
		f := newSetupFixture(t)
		past, future := time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour)
		f.seedHealth(t, "coder-1", AgentDegradedWorktreeSetupFailed, "task-1", past)
		f.seedHealth(t, "coder-2", AgentDegradedWorktreeSetupFailed, "task-2", past)
		f.seedHealth(t, "coder-3", AgentDegradedWorktreeCreateFailed, "task-1", past)
		f.seedHealth(t, "coder-4", AgentDegradedWorktreeSetupFailed, "task-1", future)
		mark(t, f)

		if err := f.run(t, passWhenMarked); err != nil {
			t.Fatal(err)
		}

		health := f.health(t)
		if _, ok := health["coder-1"]; ok {
			t.Error("earlier setup degradation of the task kept")
		}
		for _, kept := range []string{"coder-2", "coder-3", "coder-4"} {
			if _, ok := health[kept]; !ok {
				t.Errorf("%s removed: other task, other reason, or recorded after the pass", kept)
			}
		}
	})
}

func TestWorktreeSetupArtifactsKeyByFullPath(t *testing.T) {
	f := newSetupFixture(t)
	twin := filepath.Join(f.root, "elsewhere", "task-1")
	if err := os.MkdirAll(twin, 0o755); err != nil {
		t.Fatal(err)
	}

	if f.run(t, "echo first-worktree; exit 1") == nil {
		t.Fatal("first setup passed, want failure")
	}
	if err := RunPostWorktreeCmd(f.root, "echo second-worktree; exit 1", twin, testSetupTimeout, nil); err == nil {
		t.Fatal("second setup passed, want failure")
	}

	twinArtifacts, err := newWorktreeSetupArtifacts(f.root, twin)
	if err != nil {
		t.Fatal(err)
	}
	if twinArtifacts.outputPath == f.output || twinArtifacts.outcomePath == f.outcome {
		t.Fatal("worktrees sharing a basename share setup artifacts")
	}
	first, err := os.ReadFile(f.output)
	if err != nil || !strings.Contains(string(first), "first-worktree") || strings.Contains(string(first), "second-worktree") {
		t.Errorf("first worktree output = %q (%v), want only its own", first, err)
	}
}

func TestReadWorktreeSetupOutcomeRejectsInvalidRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcome")
	at := time.Now().UTC().Format(time.RFC3339Nano)
	for _, content := range []string{
		"0 ok " + at,
		"-1 ok " + at,
		"18446744073709551616 ok " + at, // overflows uint64
		"1 maybe " + at,
		"1 ok yesterday",
		"1 ok",
		"1 ok " + at + " extra",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readWorktreeSetupOutcome(path); !stderrors.Is(err, errMalformedWorktreeSetupOutcome) {
			t.Errorf("record %q: err = %v, want malformed", content, err)
		}
		if seq, err := recordWorktreeSetupOutcome(path, true, time.Now()); err == nil || seq != 0 {
			t.Errorf("record %q: recorded seq %d over an unreadable record", content, seq)
		}
	}

	if err := os.WriteFile(path, []byte(fmt.Sprintf("18446744073709551615 ok %s\n", at)), 0o600); err != nil {
		t.Fatal(err)
	}
	if seq, err := recordWorktreeSetupOutcome(path, true, time.Now()); err == nil || seq != 0 {
		t.Errorf("exhausted sequence recorded seq %d, want refusal", seq)
	}
}
