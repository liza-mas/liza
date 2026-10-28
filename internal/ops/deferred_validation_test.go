package ops

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/toolresult"
)

// dvFixture is an integration branch whose task merges carry mutation
// receipts, with a goal ledger the deferred-validation runner reads.
type dvFixture struct {
	t    *testing.T
	root string
	bb   *db.Blackboard
	base string
}

// waitForFile polls for a marker a shell command creates; the command blocks
// on a release file, so no timing assumption decides the outcome.
func waitForFile(t *testing.T, path string, limit time.Duration) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(limit)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("%s did not appear within %s", path, limit)
		}
	}
}

func requireShellRunner(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("deferred-validation fixtures use POSIX shell commands")
	}
}

func newDVFixture(t *testing.T, files map[string]string) *dvFixture {
	t.Helper()
	requireShellRunner(t)
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.MustGit(t, root, "checkout", "integration")
	f := &dvFixture{t: t, root: root}
	files[".gitignore"] = ".liza/\n.deps/\n"
	f.write(files)
	testhelpers.MustGit(t, root, "add", "-A", ".")
	testhelpers.MustGit(t, root, "commit", "-m", "test: base")
	f.base = testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	f.bb = testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	return f
}

func (f *dvFixture) write(files map[string]string) {
	f.t.Helper()
	for name, contents := range files {
		path := filepath.Join(f.root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

func dvCommand(command string, timeout int) models.DeferredValidationCommand {
	return models.DeferredValidationCommand{
		Command: command, CommandSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(command))),
		TimeoutSeconds: timeout, Authority: models.AcceptanceDeferralContract,
	}
}

// merge commits files on integration as taskID's merge and records its
// mutation receipt, plus a deferral when commands are given.
func (f *dvFixture) merge(taskID string, files map[string]string, commands ...models.DeferredValidationCommand) string {
	f.t.Helper()
	before := testhelpers.MustGit(f.t, f.root, "rev-parse", "HEAD")
	f.write(files)
	testhelpers.MustGit(f.t, f.root, "add", "-A", ".")
	testhelpers.MustGit(f.t, f.root, "commit", "-m", "Merge "+taskID)
	after := testhelpers.MustGit(f.t, f.root, "rev-parse", "HEAD")
	f.modify(func(state *models.State) {
		if state.Goal.Integration == nil {
			state.Goal.Integration = &models.IntegrationLifecycle{}
		}
		lifecycle := state.Goal.Integration
		lifecycle.MutationReceipts = append(lifecycle.MutationReceipts, models.IntegrationMutationReceipt{TaskID: taskID, BeforeCommit: before, AfterCommit: after})
		if len(commands) > 0 {
			if lifecycle.DeferredValidation == nil {
				lifecycle.DeferredValidation = &models.DeferredValidationLedger{}
			}
			lifecycle.DeferredValidation.Deferrals = append(lifecycle.DeferredValidation.Deferrals, models.DeferredValidationDeferral{
				TaskID: taskID, MergeCommit: after, BeforeCommit: before, RecordedAt: time.Now().UTC(), Commands: commands,
			})
		}
	})
	return after
}

func (f *dvFixture) modify(fn func(*models.State)) {
	f.t.Helper()
	if err := f.bb.Modify(func(state *models.State) error { fn(state); return nil }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *dvFixture) ledger() *models.DeferredValidationLedger {
	f.t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		f.t.Fatal(err)
	}
	return deferredValidationLedger(state)
}

func (f *dvFixture) run(force bool) *RunDeferredValidationResult {
	f.t.Helper()
	result, err := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{Force: force})
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}

func onlyBatch(t *testing.T, result *RunDeferredValidationResult) models.DeferredValidationBatch {
	t.Helper()
	if result.Outcome != "ran" || len(result.Batches) != 1 {
		t.Fatalf("want exactly one batch, got %#v", result)
	}
	return result.Batches[0]
}

const dvCheckValue = `test "$(cat value.txt)" = ok`

func TestDeferredValidationBatchPassesAndCoversHEAD(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check := dvCommand(dvCheckValue, 60)
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchPassed || batch.Commit != head || len(batch.Results) != 1 || batch.Results[0].Status != models.DeferredValidationResultPassed {
		t.Fatalf("batch = %#v", batch)
	}
	state, _ := f.bb.Read()
	if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
		t.Fatalf("gate after passed batch: waiting=%v blocked=%v", waiting, blocked)
	}
	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "covered" {
		t.Fatalf("covered HEAD must be a no-op, got %#v", again)
	}
	if f.ledger().Runner != nil {
		t.Fatal("runner record must be cleared after publication")
	}
}

// dvRendezvous announces itself by touching mine, then waits up to 15 s for
// other to appear and fails without it: two such commands pass only when they
// run at the same time.
func dvRendezvous(mine, other string) models.DeferredValidationCommand {
	return dvCommand(fmt.Sprintf(`touch %q; i=0; while [ ! -f %q ]; do i=$((i+1)); [ $i -gt 300 ] && exit 1; sleep 0.05; done`, mine, other), 60)
}

// dvGateLimit is a one-slot admission class matching only commands that
// contain "# gated", so a test holding its slot keeps such a lane from
// starting its command until the slot is released. The token must not occur
// in the class's slot paths, which dvAwaitGateQueue embeds in commands.
var dvGateLimit = models.AdmissionLimit{Class: "dv-gate", MaxConcurrent: 1, Match: []string{"# gated"}}

// holdGate configures dvGateLimit and takes its slot for the test.
func (f *dvFixture) holdGate() *admissionSlots {
	f.t.Helper()
	f.modify(func(state *models.State) { state.Config.AdmissionLimits = []models.AdmissionLimit{dvGateLimit} })
	return holdAdmissionSlots(f.t, f.root, dvGateLimit)
}

// dvAwaitGateQueue is a shell prefix that waits until a lane queues for the
// gate. A lane queues only once prepared, so the gated lane then starts no
// process until it gets the slot.
func (f *dvFixture) dvAwaitGateQueue() string {
	queue := filepath.Join(paths.New(f.root).AdmissionSlotsDir(), dvGateLimit.Class, "queue")
	return fmt.Sprintf("until ls %q/*.lock >/dev/null 2>&1; do sleep 0.02; done; ", queue)
}

// A batch runs its commands concurrently, each in its own lane, so the
// integration barrier costs the slowest command rather than their sum (D-210).
func TestDeferredValidationBatchRunsCommandsConcurrently(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	signal := t.TempDir()
	a, b := filepath.Join(signal, "a"), filepath.Join(signal, "b")
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvRendezvous(a, b), dvRendezvous(b, a))

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchPassed || batch.Commit != head || !batch.Covers(f.ledger().EffectiveCommands()) {
		t.Fatalf("batch = %#v, want a passed batch covering HEAD: the commands did not run concurrently", batch)
	}
}

// Lanes still take their admission slots: two commands of a one-slot class
// never overlap, and each ran in its own lane worktree.
func TestDeferredValidationLanesRespectAdmissionClass(t *testing.T) {
	shortAdmissionSlotPoll(t)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	busy := filepath.Join(t.TempDir(), "busy")
	exclusive := func(name string) models.DeferredValidationCommand {
		return dvCommand(fmt.Sprintf("mkdir %q || exit 1; sleep 0.3; rmdir %q # gated %s", busy, busy, name), 60)
	}
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, exclusive("a"), exclusive("b"))
	f.modify(func(state *models.State) { state.Config.AdmissionLimits = []models.AdmissionLimit{dvGateLimit} })

	if batch := onlyBatch(t, f.run(false)); batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch = %#v, want passed: the one-slot class let the commands overlap", batch)
	}
	for _, lane := range []string{"worktree", "worktree-1"} {
		if _, err := os.Stat(filepath.Join(deferredValidationDir(f.root), lane)); err != nil {
			t.Fatalf("lane worktree %s: %v", lane, err)
		}
	}
}

// Failing commands are bisected concurrently, each in its own lane; results
// and culprits keep the effective command order whichever lane ends first.
func TestDeferredValidationConcurrentBisectsNameEachCulprit(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n", "other.txt": "ok\n"})
	slow := dvCommand("sleep 0.5; "+dvCheckValue, 60)
	other := dvCommand(`test "$(cat other.txt)" = ok`, 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, slow, other)
	valueCulprit := f.merge("task-2", map[string]string{"value.txt": "broken\n"})
	otherCulprit := f.merge("task-3", map[string]string{"other.txt": "broken\n"})

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchFailed || len(batch.Results) != 2 ||
		batch.Results[0].CommandSHA256 != slow.CommandSHA256 || batch.Results[1].CommandSHA256 != other.CommandSHA256 {
		t.Fatalf("batch = %#v, want failed with results in command order", batch)
	}
	want := []models.DeferredValidationCulprit{
		{CommandSHA256: slow.CommandSHA256, TaskID: "task-2", MergeCommit: valueCulprit},
		{CommandSHA256: other.CommandSHA256, TaskID: "task-3", MergeCommit: otherCulprit},
	}
	if len(batch.Culprits) != len(want) {
		t.Fatalf("culprits = %#v", batch.Culprits)
	}
	for i, culprit := range batch.Culprits {
		if culprit.CommandSHA256 != want[i].CommandSHA256 || culprit.TaskID != want[i].TaskID || culprit.MergeCommit != want[i].MergeCommit {
			t.Fatalf("culprit %d = %#v, want %#v", i, culprit, want[i])
		}
	}
}

// A halt a queued lane observes ends the batch: queued lanes stop waiting, a
// running lane keeps the result it observed, and no lane starts admitted work
// afterwards, even when the slot frees and the system resumes before a queued
// lane's next start check (the running lane's bisect is refused too).
func TestDeferredValidationObservedHaltEndsTheBatch(t *testing.T) {
	shortAdmissionSlotPoll(t)
	shortHaltPoll(t)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	signal := t.TempDir()
	queued, ranShort, ranLong := filepath.Join(signal, "queued"), filepath.Join(signal, "short"), filepath.Join(signal, "long")
	queue := filepath.Join(paths.New(f.root).AdmissionSlotsDir(), dvGateLimit.Class, "queue")
	// Lane 0 runs ungated until it saw both gated lanes queued, signals it,
	// and fails once the queue emptied again. The test halts only after the
	// signal, so lane 0 cannot miss the queued window.
	running := dvCommand(fmt.Sprintf(`until [ "$(ls %q/*.lock 2>/dev/null | wc -l)" -ge 2 ]; do sleep 0.02; done; touch %q; while ls %q/*.lock >/dev/null 2>&1; do sleep 0.02; done; exit 1`, queue, queued, queue), 60)
	short := dvCommand(fmt.Sprintf("touch %q # gated short", ranShort), 60)
	long := dvCommand(fmt.Sprintf("touch %q # gated long", ranLong), 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, running, short, long)
	held := f.holdGate()
	var once sync.Once
	observed := deferredValidationHaltObserved
	deferredValidationHaltObserved = func() {
		once.Do(func() {
			held.release()
			if err := f.setMode(models.SystemModeRunning); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(func() { deferredValidationHaltObserved = observed })

	done := f.runInBackground()
	waitForFile(t, queued, 30*time.Second)
	if err := f.setMode(models.SystemModePaused); err != nil {
		t.Fatal(err)
	}

	batch := onlyBatch(t, awaitOutcome(t, done, 30*time.Second, "the queued lanes kept waiting after the halt").result)
	if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != "halted" {
		t.Fatalf("batch = %#v, want aborted halted", batch)
	}
	if got := batch.Results[0]; got.Status != models.DeferredValidationResultFailed || got.ExitCode != 1 {
		t.Fatalf("running lane result = %#v, want its observed failure kept", got)
	}
	if batch.Results[1].Status != models.DeferredValidationResultNotRun || batch.Results[2].Status != models.DeferredValidationResultNotRun {
		t.Fatalf("gated results = %#v, want not_run", batch.Results[1:])
	}
	for _, marker := range []string{ranShort, ranLong} {
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("%s ran after the batch observed a halt", filepath.Base(marker))
		}
	}
	if len(batch.Culprits) != 0 {
		t.Fatalf("culprits = %#v, want none: the ended batch must refuse the bisect", batch.Culprits)
	}
}

func TestDeferredValidationBisectNamesCulpritAndRecordsDiscovery(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check := dvCommand(dvCheckValue, 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
	culpritCommit := f.merge("task-2", map[string]string{"value.txt": "broken\n"})
	head := f.merge("task-3", map[string]string{"c.txt": "c\n"})

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchFailed || batch.Commit != head || len(batch.Culprits) != 1 {
		t.Fatalf("batch = %#v", batch)
	}
	culprit := batch.Culprits[0]
	if culprit.TaskID != "task-2" || culprit.MergeCommit != culpritCommit || culprit.Unattributed {
		t.Fatalf("culprit = %#v", culprit)
	}
	passed, failed := false, false
	for _, step := range culprit.Steps {
		passed = passed || step.Status == models.DeferredValidationResultPassed
		failed = failed || (step.Commit == culpritCommit && step.Status == models.DeferredValidationResultFailed)
	}
	if !passed || !failed {
		t.Fatalf("culprit lacks an executed passing predecessor and failing successor: %#v", culprit.Steps)
	}
	state, _ := f.bb.Read()
	if len(state.Discovered) != 1 || state.Discovered[0].Urgency != "immediate" || state.Discovered[0].During != "task-2" || !strings.Contains(state.Discovered[0].Recommendation, "task-2") {
		t.Fatalf("discovery = %#v", state.Discovered)
	}
	if _, blocked := deferredValidationGate(state, head); blocked == nil || blocked.Code != string(integrationProgressBlockedDeferredFailed) || len(blocked.TaskIDs) != 1 || blocked.TaskIDs[0] != "task-2" {
		t.Fatalf("gate must block on the culprit, got %#v", blocked)
	}
	if again := f.run(false); again.Outcome != "skipped" || again.Reason != "failed_at_head" {
		t.Fatalf("a failed batch at an unchanged HEAD must not auto-retry, got %#v", again)
	}
	forced := onlyBatch(t, f.run(true))
	if !forced.Forced || forced.Status != models.DeferredValidationBatchFailed {
		t.Fatalf("forced retry = %#v", forced)
	}
	state, _ = f.bb.Read()
	if len(state.Discovered) != 1 {
		t.Fatalf("a repeated culprit must not record a second discovery: %#v", state.Discovered)
	}
}

func TestDeferredValidationBisectAttributesFirstDeferringMerge(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check := dvCommand(dvCheckValue, 60)
	culpritCommit := f.merge("task-1", map[string]string{"value.txt": "broken\n"}, check)
	f.merge("task-2", map[string]string{"b.txt": "b\n"})

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-1" || batch.Culprits[0].MergeCommit != culpritCommit {
		t.Fatalf("baseline must be the first deferring merge's Before, got %#v", batch.Culprits)
	}
}

func TestDeferredValidationBisectUnattributedWhenBaselineFails(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "broken\n"})
	check := dvCommand(dvCheckValue, 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || !batch.Culprits[0].Unattributed || batch.Culprits[0].Reason != "fails_at_baseline" {
		t.Fatalf("culprits = %#v", batch.Culprits)
	}
	state, _ := f.bb.Read()
	if len(state.Discovered) != 1 || state.Discovered[0].During != "unattributed" {
		t.Fatalf("discovery = %#v", state.Discovered)
	}
}

func TestDeferredValidationBisectOrdersByAncestryNotReceiptOrder(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	check := dvCommand(dvCheckValue, 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
	culpritCommit := f.merge("task-2", map[string]string{"value.txt": "broken\n"})
	f.merge("task-3", map[string]string{"c.txt": "c\n"})
	f.merge("task-4", map[string]string{"d.txt": "d\n"})
	// Receipts are persisted after the ref moves, outside the mutation lock;
	// shuffle their append order so only ancestry can order the search.
	f.modify(func(state *models.State) {
		receipts := state.Goal.Integration.MutationReceipts
		receipts[1], receipts[3] = receipts[3], receipts[1]
	})

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-2" || batch.Culprits[0].MergeCommit != culpritCommit {
		t.Fatalf("culprits = %#v", batch.Culprits)
	}
}

func TestDeferredValidationProbeReprovisionsEachRevision(t *testing.T) {
	t.Parallel()
	// Setup installs the revision's declared dependency into an ignored
	// directory. The command passes only with the v1 dependency, so a probe
	// that reused HEAD's install would blame the wrong merge.
	f := newDVFixture(t, map[string]string{"deps.txt": "v1\n"})
	setup := "mkdir -p .deps && cp deps.txt .deps/installed"
	f.modify(func(state *models.State) { state.Config.PostWorktreeCmd = &setup })
	check := dvCommand(`test "$(cat .deps/installed)" = v1`, 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
	culpritCommit := f.merge("task-2", map[string]string{"deps.txt": "v2\n"})
	f.merge("task-3", map[string]string{"c.txt": "c\n"})

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-2" || batch.Culprits[0].MergeCommit != culpritCommit {
		t.Fatalf("culprits = %#v", batch.Culprits)
	}
}

func TestDeferredValidationContaminatingCommandIsNeverPassed(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	dirty := dvCommand("echo changed >> value.txt", 60)
	check := dvCommand(dvCheckValue, 60)
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dirty, check)

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchInfraFailed || len(batch.Results) != 2 {
		t.Fatalf("batch = %#v", batch)
	}
	if batch.Results[0].Status != models.DeferredValidationResultContaminated || batch.Results[1].Status != models.DeferredValidationResultPassed {
		t.Fatalf("the next command must run in its own clean lane: %#v", batch.Results)
	}
	state, _ := f.bb.Read()
	if waiting, blocked := deferredValidationGate(state, head); waiting == nil || blocked != nil {
		t.Fatalf("an infra-failed batch must keep the gate waiting, got waiting=%v blocked=%v", waiting, blocked)
	}
}

func TestDeferredValidationSetupTimeoutIsInfraFailureAndReleasesLock(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	setup := "sleep 30"
	f.modify(func(state *models.State) {
		state.Config.PostWorktreeCmd = &setup
		state.Config.DeferredValidation = &models.DeferredValidationConfig{SetupTimeoutSeconds: 1}
	})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchInfraFailed || !strings.Contains(batch.Reason, "setup") || batch.Results[0].Status != models.DeferredValidationResultNotRun {
		t.Fatalf("batch = %#v", batch)
	}
	held, acquired, err := filelock.New(filepath.Join(deferredValidationDir(f.root), "runner")).TryHold("test")
	if err != nil || !acquired {
		t.Fatalf("runner lock must be released after a setup timeout: acquired=%v err=%v", acquired, err)
	}
	_ = held.Release()
}

func TestDeferredValidationLiveRunnerMakesSecondInvocationNoOp(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	if err := os.MkdirAll(deferredValidationDir(f.root), 0o755); err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(filepath.Join(deferredValidationDir(f.root), "runner")).TryHold("live-runner")
	if err != nil || !acquired {
		t.Fatalf("take lock: %v %v", acquired, err)
	}
	defer func() { _ = held.Release() }()

	if result := f.run(false); result.Outcome != "skipped" || result.Reason != "runner_active" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(deferredValidationDir(f.root), "worktree")); !os.IsNotExist(err) {
		t.Fatal("a second runner must not touch the batch worktree")
	}
}

func TestDeferredValidationInheritedLockOutlivesDeadRunner(t *testing.T) {
	t.Parallel()
	requireShellRunner(t)
	dir := t.TempDir()
	lock := filelock.New(filepath.Join(dir, "runner"))
	held, acquired, err := lock.TryHold("old-runner")
	if err != nil || !acquired || held.File() == nil {
		t.Skip("lock descriptors do not pass to children on this platform")
	}
	release := filepath.Join(dir, "release")
	child, err := shellCommandContext(context.Background(), fmt.Sprintf("while [ ! -f %q ]; do sleep 0.05; done", release), dir)
	if err != nil {
		t.Fatal(err)
	}
	child.ExtraFiles = []*os.File{held.File()}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	// A dying runner's descriptor is closed without an explicit unlock; the
	// child's inherited descriptor must keep the successor out.
	if err := held.File().Close(); err != nil {
		t.Fatal(err)
	}
	if _, acquired, err := lock.TryHold("successor"); err != nil || acquired {
		t.Fatalf("successor acquired the lock while the old child lives: acquired=%v err=%v", acquired, err)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	successor, acquired, err := lock.TryHold("successor")
	if err != nil || !acquired {
		t.Fatalf("successor must acquire after the child exits: acquired=%v err=%v", acquired, err)
	}
	_ = successor.Release()
}

func TestDeferredValidationDeadPredecessorIsRecordedAndCleared(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.modify(func(state *models.State) {
		state.Goal.Integration.DeferredValidation.Runner = &models.DeferredValidationRunner{Token: "dead", PID: 1, Commit: head, StartedAt: time.Now().UTC()}
	})

	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch = %#v", batch)
	}
	ledger := f.ledger()
	if len(ledger.Batches) != 2 || ledger.Batches[0].Reason != "runner_died" || ledger.Batches[0].Status != models.DeferredValidationBatchAborted || ledger.Runner != nil {
		t.Fatalf("ledger = %#v", ledger)
	}
}

func TestDeferredValidationUnfencedPredecessorRefusesUntilSettled(t *testing.T) {
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.modify(func(state *models.State) {
		state.Goal.Integration.DeferredValidation.Runner = &models.DeferredValidationRunner{Token: "unsettled", PID: 4242, Commit: head, StartedAt: time.Now().UTC()}
	})
	previous := deferredValidationLockFollowsChildren
	deferredValidationLockFollowsChildren = func(*filelock.Held) bool { return false }
	t.Cleanup(func() { deferredValidationLockFollowsChildren = previous })

	_, err := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{})
	if err == nil || !strings.Contains(err.Error(), "settle") {
		t.Fatalf("want an unsettled-predecessor refusal, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(deferredValidationDir(f.root), "worktree")); !os.IsNotExist(statErr) {
		t.Fatal("refused recovery must not create, reset or remove the batch worktree")
	}
	if ledger := f.ledger(); len(ledger.Batches) != 0 || ledger.Runner == nil || ledger.Runner.Token != "unsettled" {
		t.Fatalf("refused recovery changed the ledger: %#v", ledger)
	}

	if _, err := SettleDeferredValidation(f.root, "operator confirmed pid 4242 is gone"); err != nil {
		t.Fatal(err)
	}
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch after settlement = %#v", batch)
	}
	if ledger := f.ledger(); ledger.Batches[0].Reason != "runner_settled_by_operator" {
		t.Fatalf("settlement not recorded: %#v", ledger.Batches)
	}
}

func TestDeferredValidationStaleTokenCannotPublish(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.modify(func(state *models.State) {
		state.Goal.Integration.DeferredValidation.Runner = &models.DeferredValidationRunner{Token: "successor", PID: 1, Commit: head, StartedAt: time.Now().UTC()}
	})
	r := &deferredValidationRunner{root: f.root, bb: f.bb, mask: func(s string) string { return s }}
	now := time.Now().UTC()
	if _, err := r.publish("stale", models.DeferredValidationBatch{Commit: head, StartedAt: now, FinishedAt: now, Status: models.DeferredValidationBatchPassed}); err == nil {
		t.Fatal("a stale token must not publish")
	}
	if ledger := f.ledger(); len(ledger.Batches) != 0 || ledger.Runner.Token != "successor" {
		t.Fatalf("stale publication changed the ledger: %#v", ledger)
	}
}

func TestDeferredValidationRefusesWhenHaltedOrStopped(t *testing.T) {
	t.Parallel()
	for _, mode := range []models.SystemMode{models.SystemModePaused, models.SystemModeStopped} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
			f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
			f.modify(func(state *models.State) { state.Config.Mode = mode })

			if result := f.run(false); result.Outcome != "skipped" || result.Reason != "halted" {
				t.Fatalf("result = %#v", result)
			}
			if _, err := os.Stat(filepath.Join(deferredValidationDir(f.root), "worktree")); !os.IsNotExist(err) {
				t.Fatal("a refused run must not touch the batch worktree")
			}
			if ledger := f.ledger(); len(ledger.Batches) != 0 || ledger.Runner != nil {
				t.Fatalf("a refused run changed the ledger: %#v", ledger)
			}
		})
	}
}

func TestDeferredValidationHaltDuringCommandStopsLaterStarts(t *testing.T) {
	t.Parallel()
	for _, mode := range []models.SystemMode{models.SystemModePaused, models.SystemModeStopped} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
			signal := t.TempDir()
			started, release, second := filepath.Join(signal, "started"), filepath.Join(signal, "release"), filepath.Join(signal, "second")
			first := dvCommand(fmt.Sprintf("touch %q; while [ ! -f %q ]; do sleep 0.05; done", started, release), 60)
			// The next command's lane waits behind the test's gate slot, so
			// it can start only after the halt commits.
			next := dvCommand(fmt.Sprintf("touch %q # gated", second), 60)
			f.merge("task-1", map[string]string{"a.txt": "a\n"}, first, next)
			gate := f.holdGate()

			done := make(chan *RunDeferredValidationResult, 1)
			go func() {
				result, _ := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{})
				done <- result
			}()
			waitForFile(t, started, time.Minute)
			f.modify(func(state *models.State) { state.Config.Mode = mode })
			// The halt's barrier returns while the admitted command still runs:
			// the runner holds the admission lock only across start.
			if err := AwaitWorkAdmissionBarrier(f.root, "pause"); err != nil {
				t.Fatalf("halt barrier blocked on a running batch command: %v", err)
			}
			gate.release()
			if err := os.WriteFile(release, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			result := <-done
			batch := onlyBatch(t, result)
			if batch.Status != models.DeferredValidationBatchAborted || batch.Reason != "halted" {
				t.Fatalf("batch = %#v", batch)
			}
			if _, err := os.Stat(second); !os.IsNotExist(err) {
				t.Fatal("no command may start after a committed halt or stop")
			}
		})
	}
}

// A halt or stop that commits while a step runs, or between its two post-run
// Git reads, keeps what the step observed: those reads finish admitted work
// and start none. Anything that begins work still does not start (D-158).
func TestDeferredValidationHaltKeepsFinishedStepResult(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		step   string // %[1]q is the step's marker, outside the batch worktree
		next   bool
		status models.DeferredValidationResultStatus
		exit   int
	}{
		{"passed_then_next", `touch %[1]q`, true, models.DeferredValidationResultPassed, 0},
		{"passed_final", `touch %[1]q`, false, models.DeferredValidationResultPassed, 0},
		{"failed_final", `touch %[1]q; echo HALT-FAILURE-MARKER; exit 1`, false, models.DeferredValidationResultFailed, 1},
		{"contaminated_then_next", `touch %[1]q dirty.txt`, true, models.DeferredValidationResultContaminated, 0},
	}
	modes := []models.SystemMode{models.SystemModePaused, models.SystemModeCircuitBreakerTripped, models.SystemModeStopped}
	for _, tc := range cases {
		for _, mode := range modes {
			// haltAt counts reaps from the step's own: 0 halts before the
			// first post-run read, 1 between rev-parse and status.
			for _, haltAt := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s/%s/reap%d", tc.name, mode, haltAt), func(t *testing.T) {
					t.Parallel()
					f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
					signal := t.TempDir()
					marker, second := filepath.Join(signal, "step"), filepath.Join(signal, "second")
					step := fmt.Sprintf(tc.step, marker)
					var gate *admissionSlots
					if tc.next {
						// The next lane queues behind the test's gate slot before
						// the step starts, so after the marker only the step's
						// own processes are reaped, and the next lane can start
						// only once the halt committed.
						gate = f.holdGate()
						step = f.dvAwaitGateQueue() + step
					}
					commands := []models.DeferredValidationCommand{dvCommand(step, 60)}
					if tc.next {
						commands = append(commands, dvCommand(fmt.Sprintf("touch %q # gated", second), 60))
					}
					head := f.merge("task-1", map[string]string{"a.txt": "a\n"}, commands...)
					var reaps atomic.Int32
					hooks := deferredValidationHooks{platformSupported: true, reap: func(pid int, wait time.Duration) bool {
						if _, err := os.Stat(marker); err == nil {
							if reaps.Add(1)-1 == int32(haltAt) {
								f.modify(func(state *models.State) { state.Config.Mode = mode })
								if gate != nil {
									gate.release()
								}
							}
						}
						return reapProcessGroup(pid, wait)
					}}

					result, err := runDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{}, hooks)
					if err != nil {
						t.Fatal(err)
					}
					batch := onlyBatch(t, result)
					ledger := f.ledger()
					if len(ledger.Batches) != 1 || ledger.Runner != nil {
						t.Fatalf("ledger = %#v, want the one published batch and no runner", ledger)
					}
					for _, got := range []models.DeferredValidationBatch{batch, ledger.Batches[0]} {
						step := got.Results[0]
						if step.Status != tc.status || step.ExitCode != tc.exit {
							t.Fatalf("step result = %#v, want %s exit %d", step, tc.status, tc.exit)
						}
						switch {
						case tc.next:
							if got.Status != models.DeferredValidationBatchAborted || got.Reason != "halted" || len(got.Results) != 2 || got.Results[1].Status != models.DeferredValidationResultNotRun {
								t.Fatalf("batch = %#v, want aborted/halted with the next command not_run", got)
							}
						case tc.status == models.DeferredValidationResultPassed:
							if got.Status != models.DeferredValidationBatchPassed || !got.Covers(ledger.EffectiveCommands()) || got.Commit != head {
								t.Fatalf("batch = %#v, want a passed batch covering HEAD", got)
							}
						default:
							if got.Status != models.DeferredValidationBatchAborted || got.Reason != "halted" || len(got.Culprits) != 0 {
								t.Fatalf("batch = %#v, want aborted/halted with no bisect", got)
							}
						}
					}
					if _, err := os.Stat(second); !os.IsNotExist(err) {
						t.Fatal("no command may start after a committed halt or stop")
					}
					if tc.status != models.DeferredValidationResultFailed {
						return
					}
					store, err := toolresult.New(dvToolResultsRoot(f.root), toolresult.Config{}, nil)
					if err != nil {
						t.Fatal(err)
					}
					full, err := store.Read(dvOutputRef(t, ledger.Batches[0].Results[0]), 0, toolresult.MaxReadBytes)
					if err != nil || !strings.Contains(full, "HALT-FAILURE-MARKER") {
						t.Fatalf("failed step output = %q (err %v), want the emitted failure text", full, err)
					}
				})
			}
		}
	}
}

func TestDeferredValidationDueAfterEveryNMergesWithoutNewDeferrals(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.modify(func(state *models.State) {
		state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 2}
		// Coding still in progress: the final integration gate does not
		// require a batch, so only periodic due-ness can start one.
		task := testhelpers.BuildTaskByStatus("task-pending", models.TaskStatusReady, time.Now().UTC())
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	})
	spawned := 0
	spawn := func(string) error { spawned++; return nil }
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	onlyBatch(t, f.run(false))

	for window := 1; window <= 3; window++ {
		f.merge(fmt.Sprintf("task-%d-a", window), map[string]string{fmt.Sprintf("w%d-a.txt", window): "x\n"})
		state, _ := f.bb.Read()
		head := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
		if wanted, reason, err := deferredValidationWanted(f.root, state, head, false, false); err != nil || wanted {
			t.Fatalf("window %d: one merge must not be due (wanted=%v reason=%s err=%v)", window, wanted, reason, err)
		}
		f.merge(fmt.Sprintf("task-%d-b", window), map[string]string{fmt.Sprintf("w%d-b.txt", window): "x\n"})
		if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
			t.Fatalf("window %d: two merges with no new deferral must be due (started=%v err=%v)", window, started, err)
		}
		onlyBatch(t, f.run(false))
	}
	if spawned != 3 {
		t.Fatalf("spawned = %d, want 3", spawned)
	}
}

// A batch spends the periodic interval whatever its outcome: after a failed,
// infra-failed or aborted batch, the next periodic batch waits for
// batch_every_merges further merges, with or without an earlier pass (D-160).
// A gate waiting on a verdict still starts one below the interval.
func TestDeferredValidationNonPassingBatchSpendsThePeriod(t *testing.T) {
	t.Parallel()
	check := dvCommand(dvCheckValue, 60)
	// setup lands a deferral, optionally a passed batch two merges back, and
	// a batch ending with status at HEAD, which by then is past the interval
	// counted from the pass or from the deferral.
	setup := func(t *testing.T, gateWaits, priorPass bool, status models.DeferredValidationBatchStatus) *dvFixture {
		f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
		f.modify(func(state *models.State) {
			state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 2}
			if !gateWaits {
				task := testhelpers.BuildTaskByStatus("task-pending", models.TaskStatusReady, time.Now().UTC())
				state.Tasks = append(state.Tasks, task)
				state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
			}
		})
		appendBatch := func(commit string, status models.DeferredValidationBatchStatus, result models.DeferredValidationResultStatus) {
			now := time.Now().UTC()
			batch := models.DeferredValidationBatch{
				Commit: commit, StartedAt: now, FinishedAt: now, Status: status,
				Results: []models.DeferredValidationResult{{Command: check.Command, CommandSHA256: check.CommandSHA256, Status: result, StartedAt: now, FinishedAt: now}},
			}
			if status == models.DeferredValidationBatchAborted {
				batch.Reason = "halted"
			}
			f.modify(func(state *models.State) { state.Goal.Integration.DeferredValidation.AppendBatch(batch) })
		}
		deferred := f.merge("task-1", map[string]string{"a.txt": "a\n"}, check)
		if priorPass {
			appendBatch(deferred, models.DeferredValidationBatchPassed, models.DeferredValidationResultPassed)
		}
		f.merge("task-2", map[string]string{"b.txt": "b\n"})
		head := f.merge("task-3", map[string]string{"c.txt": "c\n"})
		result := models.DeferredValidationResultNotRun
		if status == models.DeferredValidationBatchFailed {
			result = models.DeferredValidationResultFailed
		}
		appendBatch(head, status, result)
		return f
	}
	statuses := []models.DeferredValidationBatchStatus{
		models.DeferredValidationBatchFailed, models.DeferredValidationBatchInfraFailed, models.DeferredValidationBatchAborted,
	}
	spawn := func(string) error { return nil }

	for _, status := range statuses {
		for _, priorPass := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prior_pass=%v", status, priorPass), func(t *testing.T) {
				t.Parallel()
				f := setup(t, false, priorPass, status)
				f.merge("task-4", map[string]string{"d.txt": "d\n"})
				state, _ := f.bb.Read()
				head := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
				if deferredValidationRequired(f.root, state, head) {
					t.Fatal("fixture: the integration gate must not be waiting")
				}
				if wanted, reason, err := deferredValidationWanted(f.root, state, head, false, false); err != nil || wanted || reason != "not_due" {
					t.Fatalf("one merge after a %s batch must not be due: wanted=%v reason=%q err=%v", status, wanted, reason, err)
				}
				if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || started {
					t.Fatalf("one merge after a %s batch must not start one: started=%v err=%v", status, started, err)
				}
				f.merge("task-5", map[string]string{"e.txt": "e\n"})
				if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
					t.Fatalf("two merges after a %s batch must be due: started=%v err=%v", status, started, err)
				}
			})
		}
	}

	for _, status := range statuses {
		t.Run(fmt.Sprintf("%s/gate_waits", status), func(t *testing.T) {
			t.Parallel()
			f := setup(t, true, false, status)
			f.merge("task-4", map[string]string{"d.txt": "d\n"})
			state, _ := f.bb.Read()
			head := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			if !deferredValidationRequired(f.root, state, head) {
				t.Fatal("fixture: the integration gate must be waiting on a verdict at HEAD")
			}
			if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
				t.Fatalf("the gate must start a batch one merge after a %s batch: started=%v err=%v", status, started, err)
			}
		})
	}
}

func TestDeferredValidationRunnerCatchesUpBehindHEAD(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.modify(func(state *models.State) {
		state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 1}
	})
	signal := t.TempDir()
	started, release := filepath.Join(signal, "started"), filepath.Join(signal, "release")
	// The first run's command blocks until the test has merged behind it.
	gate := dvCommand(fmt.Sprintf("test -f %q || { touch %q; while [ ! -f %q ]; do sleep 0.05; done; }", release, started, release), 60)
	first := f.merge("task-1", map[string]string{"a.txt": "a\n"}, gate)

	done := make(chan *RunDeferredValidationResult, 1)
	go func() {
		result, _ := RunDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{})
		done <- result
	}()
	waitForFile(t, started, time.Minute)
	second := f.merge("task-2", map[string]string{"b.txt": "b\n"})
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	result := <-done
	if len(result.Batches) != 2 || result.Batches[0].Commit != first || result.Batches[1].Commit != second {
		t.Fatalf("runner must catch up to the HEAD that moved during its batch: %#v", result)
	}
}

func TestDeferredValidationTriggerRespectsFailedHEADAndLiveRunner(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "broken\n"})
	f.modify(func(state *models.State) {
		state.Config.DeferredValidation = &models.DeferredValidationConfig{BatchEveryMerges: 1}
	})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	spawn := func(string) error { return nil }
	if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
		t.Fatalf("first trigger: started=%v err=%v", started, err)
	}
	onlyBatch(t, f.run(false))
	if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || started {
		t.Fatalf("a failed batch at HEAD must not respawn: started=%v err=%v", started, err)
	}
	f.merge("task-2", map[string]string{"value.txt": "ok\n"})
	if err := os.MkdirAll(deferredValidationDir(f.root), 0o755); err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(filepath.Join(deferredValidationDir(f.root), "runner")).TryHold("live")
	if err != nil || !acquired {
		t.Fatal(err)
	}
	if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || started {
		t.Fatalf("a live runner must suppress spawning: started=%v err=%v", started, err)
	}
	_ = held.Release()
	if started, err := startDeferredValidationIfDueWith(f.root, spawn); err != nil || !started {
		t.Fatalf("a correction merge re-arms the trigger: started=%v err=%v", started, err)
	}
}

func TestDeferredValidationRetirementLifecycle(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"old.txt": "x\n"})
	oldCheck := dvCommand("test -f old.txt", 60)
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, oldCheck)
	if err := os.Remove(filepath.Join(f.root, "old.txt")); err != nil {
		t.Fatal(err)
	}
	head := f.merge("task-2", map[string]string{"new.txt": "x\n"})

	batch := onlyBatch(t, f.run(false))
	if len(batch.Culprits) != 1 || batch.Culprits[0].TaskID != "task-2" {
		t.Fatalf("rename culprit = %#v", batch.Culprits)
	}
	if _, err := RetireDeferredValidation(f.root, oldCheck.Command, "D-97 ruling: test renamed by task-2"); err != nil {
		t.Fatal(err)
	}
	state, _ := f.bb.Read()
	if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
		t.Fatalf("a retired command must leave the gate: waiting=%v blocked=%v", waiting, blocked)
	}
	newCheck := dvCommand("test -f new.txt", 60)
	f.merge("task-3", map[string]string{"b.txt": "b\n"}, newCheck)
	effective := f.ledger().EffectiveCommands()
	if len(effective) != 1 || effective[0].CommandSHA256 != newCheck.CommandSHA256 {
		t.Fatalf("effective = %#v", effective)
	}
	f.merge("task-4", map[string]string{"c.txt": "c\n"}, oldCheck)
	if effective := f.ledger().EffectiveCommands(); len(effective) != 2 {
		t.Fatalf("a later deferral re-enters a retired command: %#v", effective)
	}
	if _, err := RetireDeferredValidation(f.root, "not-deferred", "ruling"); err == nil {
		t.Fatal("retiring a command outside the effective set must be refused")
	}
}

func TestDeferredValidationGateWaitsAndBlocksOnAttempts(t *testing.T) {
	t.Parallel()
	check := dvCommand("true", 60)
	head := strings.Repeat("a", 40)
	state := testhelpers.CreateValidState()
	if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
		t.Fatal("no deferrals must not gate")
	}
	ledger := &models.DeferredValidationLedger{Deferrals: []models.DeferredValidationDeferral{{TaskID: "t", MergeCommit: head, Commands: []models.DeferredValidationCommand{check}}}}
	state.Goal.Integration = &models.IntegrationLifecycle{DeferredValidation: ledger}
	if waiting, _ := deferredValidationGate(state, head); waiting == nil || waiting.Code != IntegrationProgressWaitingDeferredValidation {
		t.Fatalf("missing batch must wait, got %v", waiting)
	}
	for i := 1; i <= models.DeferredValidationMaxAttemptsPerCommit; i++ {
		ledger.Batches = append(ledger.Batches, models.DeferredValidationBatch{ID: i, Commit: head, Status: models.DeferredValidationBatchAborted})
	}
	if _, blocked := deferredValidationGate(state, head); blocked == nil || blocked.Code != string(integrationProgressBlockedDeferredInfra) {
		t.Fatalf("exhausted attempts must block, got %v", blocked)
	}
	ledger.Batches = append(ledger.Batches, models.DeferredValidationBatch{ID: 9, Commit: head, Status: models.DeferredValidationBatchPassed, Results: []models.DeferredValidationResult{{CommandSHA256: check.CommandSHA256, Status: models.DeferredValidationResultPassed}}})
	if waiting, blocked := deferredValidationGate(state, head); waiting != nil || blocked != nil {
		t.Fatal("a passed covering batch must open the gate")
	}
}

func TestRecordMergedDeferralsUsesExactCommandAndMergeBase(t *testing.T) {
	t.Parallel()
	before, after := strings.Repeat("1", 40), strings.Repeat("2", 40)
	state := testhelpers.CreateValidState()
	state.Goal.Integration = &models.IntegrationLifecycle{MutationReceipts: []models.IntegrationMutationReceipt{{TaskID: "task-1", BeforeCommit: before, AfterCommit: after}}}
	secretCommand := "SECRET_TOKEN=hunter2 run-suite"
	task := &models.Task{ID: "task-1", Validation: []string{"unit", secretCommand}, AcceptanceReceipt: &models.AcceptanceReceipt{
		Deferred: []models.AcceptanceDeferredCommand{{Index: 1, Command: "***", CommandSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(secretCommand))), TimeoutSeconds: 90, Authority: models.AcceptanceDeferralOperator}},
	}}
	recordMergedDeferrals(state, task, after, time.Now().UTC())
	deferrals := state.Goal.Integration.DeferredValidation.Deferrals
	if len(deferrals) != 1 || deferrals[0].BeforeCommit != before || deferrals[0].Commands[0].Command != secretCommand || deferrals[0].Commands[0].TimeoutSeconds != 90 {
		t.Fatalf("deferrals = %#v", deferrals)
	}
	recordMergedDeferrals(state, &models.Task{ID: "task-2"}, after, time.Now().UTC())
	if len(state.Goal.Integration.DeferredValidation.Deferrals) != 1 {
		t.Fatal("a task without deferrals must not be recorded")
	}
}

// dvFailedResult runs one failing command at a fresh merge and returns the
// published batch, whose only result is that command's.
func dvFailedResult(t *testing.T, f *dvFixture, command string) (models.DeferredValidationBatch, models.DeferredValidationResult) {
	t.Helper()
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(command, 60))
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchFailed || len(batch.Results) != 1 || batch.Results[0].Status != models.DeferredValidationResultFailed {
		t.Fatalf("batch = %#v", batch)
	}
	return batch, batch.Results[0]
}

// dvOutputRef reads the result's full-output reference through its wire
// form, so the assertion is on the persisted field name.
func dvOutputRef(t *testing.T, result models.DeferredValidationResult) string {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	ref, _ := fields["output_sha256"].(string)
	return ref
}

func dvToolResultsRoot(root string) string {
	return filepath.Join(root, paths.ProjectDirName(), "tool-results")
}

func requireStateText(t *testing.T, field, text string) {
	t.Helper()
	if len(text) > statehygiene.MaxStateTextBytes || !utf8.ValidString(text) || statehygiene.IsTranscriptPayload(text) {
		t.Fatalf("%s is not state-safe (%d bytes, valid UTF-8 %v): %q", field, len(text), utf8.ValidString(text), text)
	}
}

const dvLongFailure = `head -c 9000 /dev/zero | tr '\0' x; printf '\nTAIL-MARKER\n'; exit 1`

func TestDeferredValidationLongFailureOutputPublishesBoundedExcerpt(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	_, result := dvFailedResult(t, f, dvLongFailure)
	requireStateText(t, "excerpt", result.Excerpt)
	if !strings.HasPrefix(result.Excerpt, "exit status 1\n") || !strings.Contains(result.Excerpt, "TAIL-MARKER") {
		t.Fatalf("excerpt must keep the head (error) and the tail (last line): %q", result.Excerpt)
	}
}

func TestDeferredValidationFailureKeepsFullOutputInToolResults(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	_, result := dvFailedResult(t, f, dvLongFailure)
	ref := dvOutputRef(t, result)
	if ref == "" {
		t.Fatalf("a failed result must reference its full output: %#v", result)
	}
	store, err := toolresult.New(dvToolResultsRoot(f.root), toolresult.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	full, err := store.Read(ref, 0, toolresult.MaxReadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(full, "exit status 1\n") || !strings.Contains(full, strings.Repeat("x", 9000)) || !strings.HasSuffix(full, "TAIL-MARKER\n") {
		t.Fatalf("stored output is not the complete masked capture (%d bytes)", len(full))
	}
	events, err := os.ReadDir(filepath.Join(dvToolResultsRoot(f.root), "events"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("evidence storage must not write tool-result telemetry, got %d events", len(events))
	}
}

func TestDeferredValidationTranscriptShapedOutputPublishes(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	// Only the output is transcript-shaped: the command text, which state
	// also checks, assembles the markers at run time.
	_, result := dvFailedResult(t, f, `printf '{"type":"%s.completed","item":{"type":"command_%s","aggregated_%s":"x"}}\n' item execution output; exit 1`)
	requireStateText(t, "excerpt", result.Excerpt)
	if result.Excerpt == "" {
		t.Fatal("a failed result must say why its output is not shown")
	}
}

// dvManyUntracked leaves enough untracked files that `git status --short`
// exceeds the state text limit several times over.
const dvManyUntracked = `for i in $(seq 1 400); do : > "untracked-file-with-a-deliberately-long-name-$i.txt"; done`

func TestDeferredValidationLongContaminationPublishes(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvManyUntracked, 60))
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchInfraFailed || batch.Results[0].Status != models.DeferredValidationResultContaminated {
		t.Fatalf("batch = %#v", batch)
	}
	requireStateText(t, "excerpt", batch.Results[0].Excerpt)
	if !strings.Contains(batch.Results[0].Excerpt, "batch worktree has changes") {
		t.Fatalf("excerpt = %q", batch.Results[0].Excerpt)
	}
}

func TestDeferredValidationLongSetupFailureReasonPublishes(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	setup := dvManyUntracked
	f.modify(func(state *models.State) { state.Config.PostWorktreeCmd = &setup })
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	batch := onlyBatch(t, f.run(false))
	if batch.Status != models.DeferredValidationBatchInfraFailed || !strings.HasPrefix(batch.Reason, "setup: batch worktree has changes") {
		t.Fatalf("batch = %#v", batch)
	}
	requireStateText(t, "reason", batch.Reason)
}

func TestDeferredValidationOutputStoreFailureStillPublishes(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	if err := os.WriteFile(dvToolResultsRoot(f.root), []byte("not a directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, result := dvFailedResult(t, f, dvLongFailure)
	requireStateText(t, "excerpt", result.Excerpt)
	if ref := dvOutputRef(t, result); ref != "" {
		t.Fatalf("no output can be referenced when the store cannot open, got %q", ref)
	}
}

// The synthetic secret's first line is captured before the 1 MiB capture
// limit and its remainder after it, so exact-value masking cannot recognize
// the captured part.
func TestDeferredValidationOutputOverflowOmitsCapture(t *testing.T) {
	prefix := "SYNTHETIC-D132-SECRET-PREFIX-" + strings.Repeat("p", 64)
	t.Setenv("DV_D132_SECRET_TOKEN", prefix+"\n"+strings.Repeat("s", 4096))
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	filler := acceptanceOutputLimit - len(prefix) - 64
	_, result := dvFailedResult(t, f, fmt.Sprintf(`head -c %d /dev/zero | tr '\0' x; printf '\n%%s\n' "$DV_D132_SECRET_TOKEN"; exit 1`, filler))
	requireStateText(t, "excerpt", result.Excerpt)
	if strings.Contains(result.Excerpt, "SYNTHETIC-D132") || !strings.Contains(result.Excerpt, "output omitted") {
		t.Fatalf("an overflowed capture must be omitted, not excerpted: %q", result.Excerpt)
	}
	if ref := dvOutputRef(t, result); ref != "" {
		t.Fatalf("an overflowed capture must not be stored, got %q", ref)
	}
	_ = filepath.WalkDir(dvToolResultsRoot(f.root), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), "SYNTHETIC-D132") {
			t.Fatalf("%s holds the partial secret", path)
		}
		return nil
	})
}

func TestDeferredStateText(t *testing.T) {
	t.Parallel()
	limit := statehygiene.MaxStateTextBytes
	long := "HEAD" + strings.Repeat("é", limit) + "TAIL"
	transcript := `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"x"}}`
	for _, tc := range []struct {
		name, in   string
		want       string
		head, tail string
	}{
		{name: "short text passes through", in: "exit status 1\nok", want: "exit status 1\nok"},
		{name: "text at the limit passes through", in: strings.Repeat("x", limit), want: strings.Repeat("x", limit)},
		{name: "long multibyte text keeps head and tail", in: long, head: "HEAD", tail: "TAIL"},
		{name: "invalid UTF-8 is replaced", in: "bad\xffbyte", want: "bad�byte"},
		{name: "transcript-shaped text is replaced", in: transcript, want: deferredStateTranscriptOmitted},
		{name: "transcript-shaped tail of long text is replaced", in: strings.Repeat("x", 2*limit) + transcript, want: deferredStateTranscriptOmitted},
	} {
		got := deferredStateText(tc.in)
		requireStateText(t, tc.name, got)
		if tc.want != "" && got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
		if tc.head != "" && (!strings.HasPrefix(got, tc.head) || !strings.HasSuffix(got, tc.tail) || !strings.Contains(got, "bytes omitted")) {
			t.Errorf("%s: got %d bytes without head, tail and omission marker", tc.name, len(got))
		}
	}
}
