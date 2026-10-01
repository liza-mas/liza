package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/runtimeinputs"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// These tests exercise ADR-0169 through the real strict gate: a committed
// canonical command records, per run, the fixture it received and whether the
// ledger already showed its instance consumed when it launched. They redirect
// the package-level key path, so none of them runs in parallel.

const (
	runtimeInputFixtureValue = "fixture-0001"
	runtimeInputSecret       = "rehearsal-member-credential-9f3a"
	runtimeInputCommand      = "sh boundary_test.sh"
)

type runtimeInputFixture struct {
	root, taskID, agentID, commit string
	bb                            *db.Blackboard
	probe, operator               string
}

func useRuntimeInputKey(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), runtimeinputs.KeyFileName)
	previous := runtimeInputKeyPath
	runtimeInputKeyPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { runtimeInputKeyPath = previous })
	return path
}

func runtimeInputDeclarations() []models.RuntimeInput {
	return []models.RuntimeInput{
		{ID: "w03", Commands: []string{runtimeInputCommand}, Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{"W03_FIXTURE"}},
		{ID: "principals", Commands: []string{runtimeInputCommand}, Recipe: "project.principals", Consumption: models.RuntimeInputReusable, Secret: true, Env: []string{"MEMBER_CREDENTIAL"}},
	}
}

// setupRuntimeInputScenario builds a complete strict task whose canonical
// command reads both declared inputs, records one instance of each, and
// returns before any gate ran. exitCode makes the command fail after output.
func setupRuntimeInputScenario(t *testing.T, exitCode int) *runtimeInputFixture {
	t.Helper()
	useRuntimeInputKey(t)
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	f := &runtimeInputFixture{root: root, taskID: taskID, agentID: agentID, bb: bb, probe: t.TempDir(), operator: t.TempDir()}
	wt := git.New(root).GetWorktreePath(taskID)
	statePath := paths.New(root).StatePath()
	script := fmt.Sprintf(`set -eu
test "$(cat identity.txt)" = human
consumed=$(grep -c 'state: consumed' %[2]q || true)
printf '%%s %%s\n' "${W03_FIXTURE:-absent}" "$consumed" >> %[1]q/runs.log
printf 'credential=%%s\n' "${MEMBER_CREDENTIAL:-absent}"
printf 'PASS identity assertion\n'
printf 'PASS replay assertion\n'
exit %[3]d
`, f.probe, statePath, exitCode)
	if err := os.WriteFile(filepath.Join(wt, "boundary_test.sh"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", "boundary_test.sh")
	testhelpers.MustGit(t, wt, "commit", "-m", "test: live canary reads runtime inputs")
	f.commit = testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		task.RuntimeInputs = runtimeInputDeclarations()
		parent := state.FindTask("acceptance-parent")
		parent.Output[0].RuntimeInputs = runtimeInputDeclarations()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.record(t, "w03", "W03_FIXTURE="+runtimeInputFixtureValue+"\n", taskID)
	f.record(t, "principals", "MEMBER_CREDENTIAL="+runtimeInputSecret+"\n", taskID)
	return f
}

func (f *runtimeInputFixture) envelope(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(f.operator, name+".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *runtimeInputFixture) record(t *testing.T, input, content string, tasks ...string) *RecordRuntimeInputResult {
	t.Helper()
	result, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: tasks, InputID: input, Envelope: f.envelope(t, input, content)})
	if err != nil {
		t.Fatalf("RecordRuntimeInput(%s): %v", input, err)
	}
	return result
}

func (f *runtimeInputFixture) runs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.probe, "runs.log"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (f *runtimeInputFixture) state(t *testing.T) *models.State {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (f *runtimeInputFixture) instance(t *testing.T, input string) models.RuntimeInputInstance {
	t.Helper()
	var found []models.RuntimeInputInstance
	for _, instance := range f.state(t).RuntimeInputs {
		if instance.InputID == input {
			found = append(found, instance)
		}
	}
	if len(found) != 1 {
		t.Fatalf("instances of %s = %d, want 1", input, len(found))
	}
	return found[0]
}

func (f *runtimeInputFixture) gate() runtimeInputGate {
	return runtimeInputGate{projectRoot: f.root, bb: f.bb, actor: "tester", operation: "test"}
}

func (f *runtimeInputFixture) newCommit(t *testing.T, name string) string {
	t.Helper()
	wt := git.New(f.root).GetWorktreePath(f.taskID)
	if err := os.WriteFile(filepath.Join(wt, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wt, "add", name)
	testhelpers.MustGit(t, wt, "commit", "-m", "test: "+name)
	return testhelpers.MustGit(t, wt, "rev-parse", "HEAD")
}

func requireRuntimeInputRefusal(t *testing.T, err error, code string) *AcceptanceEvidenceError {
	t.Helper()
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != AcceptanceFaultRuntimeInput || !strings.Contains(refusal.Reason, code) {
		t.Fatalf("err = %v, want a runtime-input refusal %q", err, code)
	}
	if !strings.Contains(err.Error(), "provision --record") || strings.Contains(err.Error(), "correct the evidence") {
		t.Fatalf("refusal advice = %q, want the operator action and no evidence repair", err.Error())
	}
	return refusal
}

func runtimeInputAnomalies(state *models.State) []models.Anomaly {
	var found []models.Anomaly
	for _, anomaly := range state.Anomalies {
		if anomaly.Type == models.AnomalyTypeRuntimeInputUnavailable {
			found = append(found, anomaly)
		}
	}
	return found
}

// AC3, AC6 (gate delivery and scrub), AC7: the gate spends the single_use
// input durably before launch, masks the secret on every surface, and a
// second gate run on the spent instance is refused before any command runs,
// without spending a review cycle.
func TestRuntimeInputGateSpendsBeforeLaunchAndRefusesReuse(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	t.Setenv("W03_FIXTURE", "ambient-leak")
	t.Setenv("MEMBER_CREDENTIAL", "ambient-credential-value")

	if _, err := SubmitForReview(f.root, f.taskID, f.commit, f.agentID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	if runs := f.runs(t); len(runs) != 1 || runs[0] != runtimeInputFixtureValue+" 1" {
		t.Fatalf("runs = %q, want the recorded fixture with its instance already consumed at launch", runs)
	}
	if consumed := f.instance(t, "w03"); consumed.State != models.RuntimeInputConsumed || consumed.Consumed.Task != f.taskID || consumed.Consumed.RunID == "" {
		t.Fatalf("w03 instance = %+v, want consumed by the gate run", consumed)
	}
	if reusable := f.instance(t, "principals"); reusable.State != models.RuntimeInputAvailable {
		t.Fatalf("reusable instance = %s, want available", reusable.State)
	}
	task := f.state(t).FindTask(f.taskID)
	output := task.AcceptanceReceipt.Commands[0].Output
	if strings.Contains(output, runtimeInputSecret) || !strings.Contains(output, "credential=***") {
		t.Fatalf("stored receipt output = %q, want the brokered secret masked", output)
	}
	cycles := task.ReviewCyclesCurrent

	f.newCommit(t, "follow-up.txt")
	_, err := UpdateReviewCommit(f.root, f.taskID, "human")
	requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	_, err = UpdateReviewCommit(f.root, f.taskID, "human")
	requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	if runs := f.runs(t); len(runs) != 1 {
		t.Fatalf("runs = %q, want no command executed by the refused gates", runs)
	}
	after := f.state(t)
	if got := after.FindTask(f.taskID); got.ReviewCyclesCurrent != cycles || got.Status != task.Status || got.AcceptanceReceipt == nil {
		t.Fatalf("refused repair changed the task: cycles %d→%d, status %s→%s", cycles, got.ReviewCyclesCurrent, task.Status, got.Status)
	}
	anomalies := runtimeInputAnomalies(after)
	if len(anomalies) != 1 || anomalies[0].Details["input_id"] != "w03" || anomalies[0].Details["code"] != runtimeInputCodeConsumed {
		t.Fatalf("anomalies = %+v, want one deduplicated operator signal for w03", anomalies)
	}

	// The archived receipt is the stored one, byte for byte: still masked.
	if err := f.bb.Modify(func(state *models.State) error {
		state.FindTask(f.taskID).Status = models.TaskStatusMerged
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveTerminalAcceptanceReceipts(f.root, nil, DefaultArchiveLimit); err != nil {
		t.Fatalf("archive: %v", err)
	}
	files := archiveObjectFiles(t, f.root)
	if len(files) == 0 {
		t.Fatal("no archived receipt object written")
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), runtimeInputSecret) || !strings.Contains(string(data), "credential=***") {
			t.Fatalf("archived receipt %s leaks or lost the masked secret", file)
		}
	}
}

// AC4 and D13: a gate run that fails still spends its input; the doer's next
// submission is refused before launch and the task is blocked with the
// operator's question; the failure text masks the brokered secret.
func TestRuntimeInputFailedGateStaysSpentAndResubmissionBlocks(t *testing.T) {
	f := setupRuntimeInputScenario(t, 3)
	_, err := SubmitForReview(f.root, f.taskID, f.commit, f.agentID)
	if err == nil {
		t.Fatal("failing canonical command was admitted")
	}
	if strings.Contains(err.Error(), runtimeInputSecret) || !strings.Contains(err.Error(), "credential=***") {
		t.Fatalf("failure diagnostic = %q, want the secret masked", err)
	}
	if runs := f.runs(t); len(runs) != 1 || runs[0] != runtimeInputFixtureValue+" 1" {
		t.Fatalf("runs = %q, want one launch that saw its input consumed", runs)
	}
	if f.instance(t, "w03").State != models.RuntimeInputConsumed {
		t.Fatal("a failed gate run returned its single_use input")
	}

	_, err = SubmitForReview(f.root, f.taskID, f.commit, f.agentID)
	refusal := requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	if !refusal.Blocked || !strings.Contains(err.Error(), "BLOCKED") {
		t.Fatalf("refusal = %+v (%v), want the task blocked", refusal, err)
	}
	if runs := f.runs(t); len(runs) != 1 {
		t.Fatalf("runs = %q, want the resubmission refused before launch", runs)
	}
	task := f.state(t).FindTask(f.taskID)
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "runtime_input_consumed:w03") ||
		len(task.BlockedQuestions) != 1 || !strings.Contains(task.BlockedQuestions[0], "provision --record") {
		t.Fatalf("task = %s %v %q, want BLOCKED with the operator question", task.Status, task.BlockedReason, task.BlockedQuestions)
	}
	if strings.Contains(task.BlockedQuestions[0], f.operator) {
		t.Fatal("blocked question names an artifact path")
	}
}

// AC5: of two concurrent acquisitions of one single_use instance, exactly one
// succeeds.
func TestRuntimeInputConcurrentAcquisitionHasOneWinner(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	task := f.state(t).FindTask(f.taskID)
	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = f.gate().acquire(task)
		}(i)
	}
	close(start)
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	}
	if winners != 1 {
		t.Fatalf("winners = %d (%v), want exactly one", winners, errs)
	}
}

// AC8: registering a consumed materialization again, even reformatted,
// returns it consumed, and the gate still refuses it.
func TestRuntimeInputReRecordingASpentMaterializationKeepsItSpent(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	task := f.state(t).FindTask(f.taskID)
	if _, err := f.gate().acquire(task); err != nil {
		t.Fatalf("first acquisition: %v", err)
	}
	result := f.record(t, "w03", "# re-exported fixture\n\nW03_FIXTURE="+runtimeInputFixtureValue+"\n", f.taskID)
	if result.Outcome != "unchanged" || result.State != models.RuntimeInputConsumed {
		t.Fatalf("re-record = %+v, want the consumed instance unchanged", result)
	}
	_, err := f.gate().acquire(task)
	requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
}

// AC9 and AC6 (run-live): a consumptive canonical command is refused before
// launch; a local run receives the reusable secret masked, never the
// single_use value or an ambient copy of a reserved name.
func TestRunLiveDeliversOnlyReusableInputs(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	t.Setenv("W03_FIXTURE", "ambient-leak")
	wt := git.New(f.root).GetWorktreePath(f.taskID)
	for _, argv := range [][]string{{"sh", "boundary_test.sh"}, {"sh", "-c", runtimeInputCommand}} {
		_, err := RunLive(f.root, RunLiveInput{TaskID: f.taskID, Argv: argv, Dir: wt})
		if err == nil || !strings.Contains(err.Error(), "single_use input w03") {
			t.Fatalf("RunLive(%q) err = %v, want a pre-launch refusal", argv, err)
		}
	}
	if runs := f.runs(t); len(runs) != 0 {
		t.Fatalf("runs = %q, want nothing executed", runs)
	}
	result, err := RunLive(f.root, RunLiveInput{TaskID: f.taskID, Dir: wt,
		Argv: []string{"sh", "-c", `printf '%s|%s' "${W03_FIXTURE:-absent}" "$MEMBER_CREDENTIAL"`}})
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if result.Output != "absent|***" || result.ExitCode != 0 {
		t.Fatalf("run-live = %q (exit %d), want no single_use value and the secret masked", result.Output, result.ExitCode)
	}
	if f.instance(t, "w03").State != models.RuntimeInputAvailable {
		t.Fatal("run-live spent a single_use input")
	}
}

// AC6 (another task): a gate for a task without inputs strips every reserved
// name and receives none of them.
func TestRuntimeInputGateOfAnotherTaskReceivesNothing(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	grant, err := f.gate().acquire(&models.Task{ID: "other-task"})
	if err != nil {
		t.Fatal(err)
	}
	env := grant.commandEnvironment([]string{"PATH=/bin", "W03_FIXTURE=ambient", "MEMBER_CREDENTIAL=ambient"}, runtimeInputCommand)
	if strings.Join(env, ",") != "PATH=/bin" {
		t.Fatalf("other task env = %v, want reserved names scrubbed and nothing overlaid", env)
	}
}

// AC6 (sessions): readiness resolves inputs from the ledger, role-aware: the
// doer needs the spent single_use input, a reviewer only the reusable one.
func TestRuntimeInputReadinessIsRoleAware(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	state := f.state(t)
	task := state.FindTask(f.taskID)
	if refusal := checkRuntimeInputReadiness(f.root, state, task, false); refusal != nil {
		t.Fatalf("doer readiness before consumption: %v", refusal)
	}
	if _, err := f.gate().acquire(task); err != nil {
		t.Fatal(err)
	}
	state = f.state(t)
	if refusal := checkRuntimeInputReadiness(f.root, state, task, true); refusal != nil {
		t.Fatalf("reviewer readiness after consumption: %v", refusal)
	}
	err := checkAgentRuntimeInputReadiness(f.root, f.bb, state, task, f.agentID)
	var preflight *runtimeInputPreflightError
	if !errors.As(err, &preflight) || !strings.Contains(err.Error(), "runtime_input_consumed:w03") {
		t.Fatalf("doer preflight err = %v, want a runtime-input preflight failure", err)
	}
	if len(runtimeInputAnomalies(f.state(t))) != 1 {
		t.Fatal("preflight failure did not record the operator signal")
	}
	if err := checkAgentRuntimeInputReadiness(f.root, f.bb, state, task, "code-reviewer-1"); err != nil {
		t.Fatalf("reviewer preflight: %v", err)
	}
}

// D13: a reviewer whose update-review-commit is refused loses its claim and
// the task returns to submitted; an operator caller leaves the claim alone.
func TestRuntimeInputUpdateReviewCommitRefusalReleasesOnlyTheReviewersClaim(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	if _, err := SubmitForReview(f.root, f.taskID, f.commit, f.agentID); err != nil {
		t.Fatalf("SubmitForReview: %v", err)
	}
	submitted := f.state(t).FindTask(f.taskID).Status
	reviewer := "code-reviewer-1"
	claim := func() {
		t.Helper()
		if err := f.bb.Modify(func(state *models.State) error {
			task := state.FindTask(f.taskID)
			task.Status = models.TaskStatusReviewing
			task.ReviewingBy = &reviewer
			lease := time.Now().UTC().Add(time.Hour)
			task.ReviewLeaseExpires = &lease
			agent := testhelpers.RegisteredTestAgent(models.RoleCodeReviewer)
			agent.CurrentTask = &f.taskID
			state.Agents[reviewer] = agent
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.newCommit(t, "follow-up.txt")

	claim()
	_, err := UpdateReviewCommit(f.root, f.taskID, "human")
	requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	if task := f.state(t).FindTask(f.taskID); task.ReviewingBy == nil || task.Status != models.TaskStatusReviewing {
		t.Fatalf("operator refusal changed the claim: %s %v", task.Status, task.ReviewingBy)
	}

	_, err = UpdateReviewCommit(f.root, f.taskID, reviewer)
	requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03")
	state := f.state(t)
	task := state.FindTask(f.taskID)
	if task.ReviewingBy != nil || task.ReviewLeaseExpires != nil || task.Status != submitted || task.AcceptanceReceipt == nil {
		t.Fatalf("reviewer refusal left %s reviewing_by=%v receipt=%v, want released to %s with its receipt", task.Status, task.ReviewingBy, task.AcceptanceReceipt != nil, submitted)
	}
	if agent := state.Agents[reviewer]; agent.CurrentTask != nil {
		t.Fatal("reviewer agent still holds the task")
	}
	if strings.Contains(err.Error(), "mark-blocked") {
		t.Fatalf("update-review-commit refusal advises mark-blocked: %v", err)
	}
}

func TestRuntimeInputAnomalyDeduplicationReArmsOnANewInstance(t *testing.T) {
	state := &models.State{}
	now := time.Now().UTC()
	appendRuntimeInputAnomaly(state, "task", "w03", runtimeInputCodeConsumed, "submit", "a", "coder", now)
	appendRuntimeInputAnomaly(state, "task", "w03", runtimeInputCodeConsumed, "update", "a", "human", now)
	if len(state.Anomalies) != 1 {
		t.Fatalf("anomalies = %d, want the identical refusal deduplicated", len(state.Anomalies))
	}
	appendRuntimeInputAnomaly(state, "task", "w03", runtimeInputCodeConsumed, "submit", "b", "coder", now)
	appendRuntimeInputAnomaly(state, "task", "other", runtimeInputCodeMissing, "submit", "none", "coder", now)
	if len(state.Anomalies) != 3 {
		t.Fatalf("anomalies = %d, want a new bound instance and another input announced", len(state.Anomalies))
	}
	if violations := models.AnomalyViolations(state.Anomalies); len(violations) != 0 {
		t.Fatalf("anomaly violations: %v", violations)
	}
}

func TestRuntimeInputArtifactChangeInvalidatesAndUnreadableDoesNot(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	task := f.state(t).FindTask(f.taskID)
	envelope := filepath.Join(f.operator, "w03.env")
	if err := os.Rename(envelope, envelope+".moved"); err != nil {
		t.Fatal(err)
	}
	_, err := f.gate().acquire(task)
	requireRuntimeInputRefusal(t, err, "runtime_input_unavailable:w03")
	if f.instance(t, "w03").State != models.RuntimeInputAvailable {
		t.Fatal("an unreadable artifact invalidated its instance")
	}
	if err := os.WriteFile(envelope, []byte("W03_FIXTURE=fixture-0002\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = f.gate().acquire(task)
	requireRuntimeInputRefusal(t, err, "runtime_input_invalidated:w03")
	if instance := f.instance(t, "w03"); instance.State != models.RuntimeInputInvalidated || instance.Invalidated.Reason != models.RuntimeInputInvalidatedArtifactChanged {
		t.Fatalf("changed artifact instance = %+v, want invalidated", instance)
	}
}

func TestRecordRuntimeInputRules(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	moved := filepath.Join(t.TempDir(), "w03.env")
	if err := os.WriteFile(moved, []byte("W03_FIXTURE="+runtimeInputFixtureValue+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "w03", Envelope: moved})
	if err == nil || !strings.Contains(err.Error(), filepath.Join(f.operator, "w03.env")) {
		t.Fatalf("available identity at a new locator err = %v, want a conflict naming the recorded envelope", err)
	}
	inside := filepath.Join(f.root, "w03.env")
	if err := os.WriteFile(inside, []byte("W03_FIXTURE=fixture-9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "w03", Envelope: inside}); err == nil {
		t.Fatal("an envelope inside the repository was recorded")
	}
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "unknown", Envelope: moved}); err == nil {
		t.Fatal("an undeclared input was recorded")
	}
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID, "acceptance-parent"}, InputID: "w03", Envelope: moved}); err == nil {
		t.Fatal("a single_use input was recorded for two tasks")
	}
}

func TestRuntimeInputKeyFailsClosed(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	path, err := runtimeInputKeyPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	fresh := f.envelope(t, "fresh", "W03_FIXTURE=fixture-0003\n")
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "w03", Envelope: fresh}); !errors.Is(err, runtimeinputs.ErrKeyUnavailable) {
		t.Fatalf("record without the key err = %v, want fail closed", err)
	}
	if _, err := runtimeinputs.CreateKey(path); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "w03", Envelope: fresh}); !errors.Is(err, runtimeinputs.ErrKeyUnavailable) {
		t.Fatalf("record with a different key err = %v, want fail closed", err)
	}
	_, err = f.gate().acquire(f.state(t).FindTask(f.taskID))
	requireRuntimeInputRefusal(t, err, "runtime_input_unavailable")
}

func TestRetireRuntimeInputBindings(t *testing.T) {
	declarations := runtimeInputDeclarations()
	now := time.Now().UTC()
	instance := func(input, consumption string, tasks ...string) models.RuntimeInputInstance {
		return models.RuntimeInputInstance{InputID: input, Consumption: consumption, Tasks: tasks, State: models.RuntimeInputAvailable}
	}
	state := &models.State{
		Tasks: []models.Task{
			{ID: "old", RuntimeInputs: declarations},
			{ID: "same", Status: models.TaskStatusReady, RuntimeInputs: declarations},
		},
		RuntimeInputs: map[string]models.RuntimeInputInstance{
			"single":   instance("w03", models.RuntimeInputSingleUse, "old"),
			"reusable": instance("principals", models.RuntimeInputReusable, "old"),
			"shared":   instance("principals", models.RuntimeInputReusable, "old", "keeper"),
		},
	}
	retireRuntimeInputBindings(state, state.FindTask("old"), nil, now)
	if got := state.RuntimeInputs["single"]; got.State != models.RuntimeInputInvalidated || got.Invalidated.Reason != models.RuntimeInputInvalidatedTaskRetired {
		t.Fatalf("orphaned single_use = %+v, want invalidated", got)
	}
	if got := state.RuntimeInputs["reusable"]; got.State != models.RuntimeInputAvailable || len(got.Tasks) != 0 {
		t.Fatalf("orphaned reusable = %+v, want available and unbound", got)
	}
	if got := state.RuntimeInputs["shared"]; strings.Join(got.Tasks, ",") != "keeper" {
		t.Fatalf("shared reusable tasks = %v", got.Tasks)
	}

	state.RuntimeInputs["single2"] = instance("w03", models.RuntimeInputSingleUse, "old")
	changed := models.CloneRuntimeInputs(declarations)
	changed[0].Consumption = models.RuntimeInputReusable
	state.Tasks = append(state.Tasks, models.Task{ID: "changed", Status: models.TaskStatusReady, RuntimeInputs: changed})
	retireRuntimeInputBindings(state, state.FindTask("old"), []string{"changed", "same"}, now)
	if got := state.RuntimeInputs["single2"]; got.State != models.RuntimeInputAvailable || strings.Join(got.Tasks, ",") != "same" {
		t.Fatalf("transferred single_use = %+v, want bound to the equally declared replacement only", got)
	}
}

func TestCopyEnvFileUnlessReservedRefusesAnyMention(t *testing.T) {
	deny := map[string]bool{"W03_FIXTURE": true, "MEMBER_CREDENTIAL": true}
	for name, content := range map[string]string{
		"plain assignment":     "API_TOKEN=keep\nW03_FIXTURE=fixture-1\n",
		"multiple assignments": "export OTHER=kept MEMBER_CREDENTIAL=dummy-value\n",
		"tab after export":     "export\tMEMBER_CREDENTIAL=dummy-value\n",
		"multi-line value":     "MEMBER_CREDENTIAL=\"first\nsecond\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			src, dst := filepath.Join(dir, "agent.env"), filepath.Join(dir, "copy.env")
			if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			found, err := copyEnvFileUnlessReserved(src, dst, 0o600, deny)
			if err != nil || len(found) == 0 {
				t.Fatalf("found = %v, err = %v; want the reserved name reported", found, err)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("a file mentioning a runtime-input name was copied: %v", err)
			}
		})
	}

	dir := t.TempDir()
	src, dst := filepath.Join(dir, "agent.env"), filepath.Join(dir, "copy.env")
	content := "# credentials\nAPI_TOKEN=keep\nW03_FIXTURE_EXTRA=keep\n"
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if found, err := copyEnvFileUnlessReserved(src, dst, 0o600, deny); err != nil || found != nil {
		t.Fatalf("clean file: found = %v, err = %v", found, err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != content {
		t.Fatalf("copy = %q, %v", got, err)
	}
	if info, err := os.Stat(dst); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("copy mode = %v, %v", info.Mode(), err)
	}
	if _, err := copyEnvFileUnlessReserved(src, dst, 0o600, nil); err == nil {
		t.Fatal("copy overwrote an existing destination")
	}
}

// A legacy prerequisite naming a variable reserved for runtime inputs cannot
// be satisfied, since launches strip it: preflight names that cause.
func TestValidationPreflightNamesAScrubbedRuntimeInputVariable(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusRejected)
	// Admission and the Modify fence refuse this collision now; only a state
	// written before them holds it, so write it unchecked.
	s, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	s.FindTask("task-1").ValidationPrerequisites[0].Env = []string{"W03_FIXTURE"}
	other := testhelpers.BuildTaskByStatus("other", models.TaskStatusReady, time.Now().UTC())
	other.Validation = []string{runtimeInputCommand}
	other.RuntimeInputs = runtimeInputDeclarations()[:1]
	s.Tasks = append(s.Tasks, other)
	if err := f.bb.Write(s); err != nil {
		t.Fatal(err)
	}
	session := &ValidationSession{Environment: []string{"PATH=/usr/bin:/bin"}, Execution: "local", ForceCheck: true}
	_, err = PrepareValidationPreflight(f.root, "task-1", "coder-1", "", session)
	if err == nil || !strings.Contains(err.Error(), "runtime_input_scrubbed") {
		t.Fatalf("preflight err = %v, want runtime_input_scrubbed", err)
	}
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if record := state.ValidationReadiness["coder-1"]["task-1"]; record.Code != "runtime_input_scrubbed" || record.Variable != "W03_FIXTURE" {
		t.Fatalf("readiness record = %+v", record)
	}
}

// claimRefusedForMissingInputs returns a claimable strict task whose declared
// inputs have no instance, and the claim refusal a supervisor escalates.
func claimRefusedForMissingInputs(t *testing.T) (*runtimeInputFixture, *AcceptanceEvidenceError) {
	t.Helper()
	useRuntimeInputKey(t)
	root, taskID, _, agentID, bb := completeAcceptanceScenario(t)
	resetAcceptanceTaskForClaim(t, bb, taskID, agentID, nil)
	f := &runtimeInputFixture{root: root, taskID: taskID, agentID: agentID, bb: bb, operator: t.TempDir()}
	if err := bb.Modify(func(state *models.State) error {
		state.FindTask(taskID).RuntimeInputs = runtimeInputDeclarations()
		state.FindTask("acceptance-parent").Output[0].RuntimeInputs = runtimeInputDeclarations()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ClaimTask(root, taskID, agentID)
	return f, requireRuntimeInputRefusal(t, err, "runtime_input_missing:w03")
}

// C4: provisioning between the claim refusal and its escalation repairs the
// task, so the stale refusal must not block it; a current one still does.
func TestRuntimeInputClaimRefusalIsNotEscalatedAfterProvisioning(t *testing.T) {
	t.Run("provisioned before the block", func(t *testing.T) {
		f, refusal := claimRefusedForMissingInputs(t)
		t.Cleanup(func() { testAcceptanceClaimBlockHooks = nil })
		testAcceptanceClaimBlockHooks = &acceptanceClaimBlockTestHooks{afterIntegrationEqualityCheck: func() {
			f.record(t, "w03", "W03_FIXTURE="+runtimeInputFixtureValue+"\n", f.taskID)
			f.record(t, "principals", "MEMBER_CREDENTIAL="+runtimeInputSecret+"\n", f.taskID)
		}}
		blocked, err := BlockAcceptanceRefusedTask(f.root, acceptanceCoderAuthority(f.agentID), refusal)
		if err != nil || blocked {
			t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want the repaired task left claimable", blocked, err)
		}
		if task := f.state(t).FindTask(f.taskID); task.Status == models.TaskStatusBlocked {
			t.Fatal("repaired task was blocked")
		}
	})
	t.Run("still missing", func(t *testing.T) {
		f, refusal := claimRefusedForMissingInputs(t)
		blocked, err := BlockAcceptanceRefusedTask(f.root, acceptanceCoderAuthority(f.agentID), refusal)
		if err != nil || !blocked {
			t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
		}
		task := f.state(t).FindTask(f.taskID)
		if task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "runtime_input_missing:w03") {
			t.Fatalf("blocked reason = %v", task.BlockedReason)
		}
	})
}

// A changed older artifact is invalidated, and the gate uses the next
// available instance instead of refusing.
func TestRuntimeInputGateFallsThroughToTheNextAvailableInstance(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	newer := filepath.Join(t.TempDir(), "w03-newer.env")
	if err := os.WriteFile(newer, []byte("W03_FIXTURE=fixture-0002\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordRuntimeInput(f.root, RecordRuntimeInputInput{Tasks: []string{f.taskID}, InputID: "w03", Envelope: newer}); err != nil {
		t.Fatal(err)
	}
	older := f.envelope(t, "w03", "W03_FIXTURE=fixture-changed\n") // overwrite the first instance's artifact
	grant, err := f.gate().acquire(f.state(t).FindTask(f.taskID))
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if env := strings.Join(grant.overlays[runtimeInputCommand], ","); !strings.Contains(env, "W03_FIXTURE=fixture-0002") {
		t.Fatalf("overlay = %s, want the newer instance", env)
	}
	states := map[string]string{}
	for _, instance := range f.state(t).RuntimeInputs {
		if instance.InputID == "w03" {
			states[instance.Envelope] = instance.State
		}
	}
	if states[older] != models.RuntimeInputInvalidated || states[newer] != models.RuntimeInputConsumed {
		t.Fatalf("w03 instances = %v, want the changed one invalidated and the newer one consumed", states)
	}
}
