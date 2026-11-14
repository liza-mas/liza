package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// checkFixture is one submission scenario the local check and the gate both
// run on: the local check first, which must change no state, then the gate.
type checkFixture struct {
	root, taskID, agentID, commit string
	bb                            *db.Blackboard
	// authority is the caller's authority; nil runs the gate through
	// SubmitForReview and the local check as an operator naming the agent.
	authority *models.AgentAuthority
	// probe is a directory canonical commands write run markers in.
	probe string
	// writesWorktree lets the row's own canonical command change the worktree.
	writesWorktree bool
	// maxWait is the local slot wait; zero refuses a full class at once.
	maxWait time.Duration
	// preTransition is the task's transition identity before the local run.
	preTransition string
}

// worktreeWitness is everything the local check promises to leave alone.
type worktreeWitness struct {
	state                []byte
	branch, head, status string
}

func (f *checkFixture) witness(t *testing.T) worktreeWitness {
	t.Helper()
	state, err := os.ReadFile(paths.New(f.root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	wt := git.New(f.root).GetWorktreePath(f.taskID)
	return worktreeWitness{
		state:  state,
		branch: testhelpers.MustGit(t, f.root, "rev-parse", "refs/heads/task/"+f.taskID),
		head:   testhelpers.MustGit(t, wt, "rev-parse", "HEAD"),
		status: testhelpers.MustGit(t, wt, "status", "--porcelain"),
	}
}

// local runs the check and fails unless it left state, the task branch and the
// worktree's HEAD byte for byte as they were; the status too, unless the row's
// own canonical command writes in the worktree.
func (f *checkFixture) local(t *testing.T) *CheckSubmissionResult {
	t.Helper()
	before := f.witness(t)
	f.preTransition = models.TaskTransitionID(f.task(t))
	result, err := CheckSubmission(f.root, CheckSubmissionInput{TaskID: f.taskID, AgentID: f.agentID, Authority: f.authority, MaxWait: f.maxWait})
	if err != nil {
		t.Fatalf("CheckSubmission() error = %v", err)
	}
	after := f.witness(t)
	if !bytes.Equal(before.state, after.state) || before.branch != after.branch || before.head != after.head || (!f.writesWorktree && before.status != after.status) {
		t.Fatalf("the local check changed state: state equal=%t branch %s→%s head %s→%s status %q→%q",
			bytes.Equal(before.state, after.state), before.branch, after.branch, before.head, after.head, before.status, after.status)
	}
	return result
}

func (f *checkFixture) gate() (*SubmitForReviewResult, error) {
	if f.authority != nil {
		return SubmitForReviewWithAuthority(f.root, f.taskID, f.commit, *f.authority)
	}
	return SubmitForReview(f.root, f.taskID, f.commit, f.agentID)
}

func (f *checkFixture) worktree() string { return git.New(f.root).GetWorktreePath(f.taskID) }

func (f *checkFixture) task(t *testing.T) *models.Task {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	return state.FindTask(f.taskID)
}

// requireVerdict asserts the verdict and, for a refusal, where it was made:
// every check before it passed or was not applicable, every one after it was
// not run for that reason.
func requireVerdict(t *testing.T, result *CheckSubmissionResult, verdict, at string) {
	t.Helper()
	if result.Verdict != verdict {
		t.Fatalf("verdict = %s, want %s; checks %+v", result.Verdict, verdict, result.Checks)
	}
	if verdict != SubmissionVerdictRefused {
		return
	}
	reached := false
	for _, check := range result.Checks {
		switch {
		case check.Name == at:
			reached = true
			if check.Status != SubmissionCheckRefused {
				t.Fatalf("check %s = %+v, want refused", at, check)
			}
		case !reached && check.Status != SubmissionCheckPassed && check.Status != SubmissionCheckNotApplicable:
			t.Fatalf("check %s before the refusal at %s = %+v (refusal: %v)", check.Name, at, check, result.Refusal)
		case reached && check.Status != SubmissionCheckNotApplicable && (check.Status != SubmissionCheckNotRun || check.Reason != NotRunAfterRefusal):
			t.Fatalf("check %s after the refusal at %s = %+v, want not_run after_refusal", check.Name, at, check)
		}
	}
	if !reached || result.Refusal == nil {
		t.Fatalf("no refusal at %s: reached=%t refusal=%v", at, reached, result.Refusal)
	}
}

// requireNotRun asserts the verdict is incomplete because check name was not
// run for reason, and that no check after it ran or was refused.
func requireNotRun(t *testing.T, result *CheckSubmissionResult, name, reason string) {
	t.Helper()
	requireVerdict(t, result, SubmissionVerdictIncomplete, "")
	reached := false
	for _, check := range result.Checks {
		if check.Name == name {
			reached = true
			if check.Status != SubmissionCheckNotRun || check.Reason != reason {
				t.Fatalf("check %s = %+v, want not_run %s", name, check, reason)
			}
			continue
		}
		if reached && check.Status != SubmissionCheckNotApplicable && (check.Status != SubmissionCheckNotRun || check.Reason != reason) {
			t.Fatalf("check %s after %s = %+v, want not_run %s", check.Name, name, check, reason)
		}
	}
	if !reached {
		t.Fatalf("no check %s in %+v", name, result.Checks)
	}
}

// refusalCause strips the lifecycle wrappers that carry policy around an error.
func refusalCause(err error) error {
	for {
		lifecycle, ok := err.(*LifecycleError)
		if !ok || lifecycle.Err == nil {
			return err
		}
		err = lifecycle.Err
	}
}

// postMarkerRows are the refusal rows of entries 13-19, which the gate makes
// after its preparation marker: it reports STATE_CHANGED, requery and unknown
// effects there, the local run, which has no marker, INVALID_INPUT,
// correct_input and none (ADR-0252, D3). Every other refusal row is compared
// whole.
var postMarkerRows = map[string]string{
	rowE16PendingPlanAmendment:             checkPlanAmendmentName,
	rowE17BoundedCorrectionOutsideContract: checkBoundedContractCorrectionName,
	rowE19aFailingCommand:                  checkCanonicalValidation,
	rowE19bDirtyAfterCommand:               checkCanonicalValidation,
	rowPREBExecutableMissing:               checkCanonicalValidation,
	rowD7FailingOutput:                     checkCanonicalValidation,
	rowDISSingleUseFirstFails:              checkCanonicalValidation,
	rowPREARuntimeInputUnavailable:         checkCanonicalValidation,
}

// refusalEnvelope is the refusal as submit-for-review's error envelope and the
// command render it: code, message and details, from the shared functions.
type refusalEnvelope struct {
	code, message string
	details       map[string]any
}

func renderRefusal(err error) refusalEnvelope {
	code, message := jsonout.ClassifyError(err)
	return refusalEnvelope{code: code, message: message, details: jsonout.ErrorDetails(err)}
}

// withoutFollowUp drops the BLOCKED follow-up the gate performs and words after
// a runtime-input or timeout refusal, by clearing its fields, never by trimming
// text (ASM-002). Every other error is returned as it is.
func withoutFollowUp(err error) error {
	var lifecycle *LifecycleError
	var evidence *AcceptanceEvidenceError
	if !errors.As(err, &lifecycle) || !errors.As(lifecycle.Err, &evidence) {
		return err
	}
	clone := *evidence
	clone.Blocked, clone.BlockFailed, clone.BlockSkipped = false, false, ""
	return &LifecycleError{Outcome: lifecycle.Outcome, Err: &clone}
}

// lifecycleFields are the details keys the lifecycle owns; only they, with the
// code and the message prefix they derive, may differ after the marker.
var lifecycleFields = []string{"outcome", "safe_action", "effects", "transition_id", "request_id"}

// outcomeOwnedFields are absent from every refusal, gate or local.
var outcomeOwnedFields = []string{"changed", "diagnostics", "completed_transition_id"}

// requireSameRefusal compares the gate's error with the local refusal as the
// rendered envelopes: the same error type, the same reason byte for byte, every
// field equal. Before the gate's preparation marker the whole envelope and the
// lifecycle outcome are equal. After it, each lifecycle field is asserted per
// side to its value and only those fields are masked; the remainders must then
// be equal. Every comparison runs before the function returns.
func requireSameRefusal(t *testing.T, f *checkFixture, gateErr, localErr error, postMarker bool) {
	t.Helper()
	if gateErr == nil || localErr == nil {
		t.Fatalf("gate error = %v, local refusal = %v, want both", gateErr, localErr)
	}
	gateCause, localCause := refusalCause(gateErr), refusalCause(localErr)
	if reflect.TypeOf(gateCause) != reflect.TypeOf(localCause) {
		t.Fatalf("error type: gate %T, local %T", gateCause, localCause)
	}
	if !reflect.DeepEqual(normalizedCause(gateCause), normalizedCause(localCause)) {
		t.Fatalf("causes differ:\ngate:  %+v\nlocal: %+v", gateCause, localCause)
	}
	var gate, local *LifecycleError
	if !errors.As(gateErr, &gate) || !errors.As(localErr, &local) {
		t.Fatalf("lifecycle outcome missing: gate %T, local %T", gateErr, localErr)
	}
	gateEnv, localEnv := renderRefusal(withoutFollowUp(gateErr)), renderRefusal(localErr)
	if !postMarker {
		if !reflect.DeepEqual(gate.Outcome, local.Outcome) || local.Outcome.Effects != string(LifecycleEffectsNone) {
			t.Fatalf("lifecycle outcome differs before the marker:\ngate:  %+v\nlocal: %+v", gate.Outcome, local.Outcome)
		}
		if !reflect.DeepEqual(gateEnv, localEnv) {
			t.Fatalf("envelope differs before the marker:\ngate:  %+v\nlocal: %+v", gateEnv, localEnv)
		}
		return
	}
	preTransition := f.preTransition
	requireLifecycleSide(t, "gate", gateEnv, "state_changed", models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown, "")
	requireLifecycleSide(t, "local", localEnv, "validation", models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone, preTransition)
	// The gate wrote its marker's retirement: a new transition and the preparation's request.
	if gateID, _ := gateEnv.details["transition_id"].(string); gateID == "" || gateID == preTransition {
		t.Fatalf("gate transition_id = %q, want a non-empty value differing from the local %q", gateID, preTransition)
	}
	if requestID, _ := gateEnv.details["request_id"].(string); requestID == "" {
		t.Fatal("gate request_id is empty, want the preparation's request identity")
	}
	if _, present := localEnv.details["request_id"]; present {
		t.Fatal("local request_id present, want absent: the local run has no request")
	}
	gateReason := strings.TrimPrefix(gateEnv.message, refusalPrefix(models.LifecycleStateChanged, LifecycleSafeActionRequery))
	localReason := strings.TrimPrefix(localEnv.message, refusalPrefix(models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput))
	if gateReason != localReason {
		t.Fatalf("reason differs:\ngate:  %s\nlocal: %s", gateReason, localReason)
	}
	gateRest, localRest := maskLifecycle(gateEnv), maskLifecycle(localEnv)
	if !reflect.DeepEqual(gateRest, localRest) {
		t.Fatalf("envelopes differ after masking only the lifecycle fields:\ngate:  %+v\nlocal: %+v", gateRest, localRest)
	}
}

func refusalPrefix(outcome string, action LifecycleSafeAction) string {
	return fmt.Sprintf("%s: %s (safe_action=%s): ", integrationOperationSubmitForReview, outcome, action)
}

// requireLifecycleSide asserts one side's lifecycle fields to their exact values.
func requireLifecycleSide(t *testing.T, side string, env refusalEnvelope, code, outcome string, action LifecycleSafeAction, effects LifecycleEffects, transition string) {
	t.Helper()
	want := map[string]any{"outcome": outcome, "safe_action": string(action), "effects": string(effects)}
	if transition != "" {
		want["transition_id"] = transition
	}
	for key, value := range want {
		if env.details[key] != value {
			t.Fatalf("%s details.%s = %v, want %v", side, key, env.details[key], value)
		}
	}
	if env.code != code {
		t.Fatalf("%s code = %q, want %q", side, env.code, code)
	}
	if !strings.HasPrefix(env.message, refusalPrefix(outcome, action)) {
		t.Fatalf("%s message prefix: %q, want %q", side, env.message, refusalPrefix(outcome, action))
	}
	for _, key := range outcomeOwnedFields {
		if _, present := env.details[key]; present {
			t.Fatalf("%s details.%s present, want absent", side, key)
		}
	}
}

// maskLifecycle returns the envelope without the lifecycle fields and what is
// derived from them: nothing else is removed.
func maskLifecycle(env refusalEnvelope) refusalEnvelope {
	masked := refusalEnvelope{details: map[string]any{}}
	for key, value := range env.details {
		if !slices.Contains(lifecycleFields, key) {
			masked.details[key] = value
		}
	}
	_, masked.message, _ = strings.Cut(env.message, "): ")
	return masked
}

// normalizedCause drops what only the gate does after a refusal: the BLOCKED
// follow-up it reports on the refusal.
func normalizedCause(err error) any {
	var evidence *AcceptanceEvidenceError
	if errors.As(err, &evidence) {
		clone := *evidence
		clone.Blocked, clone.BlockFailed, clone.BlockSkipped = false, false, ""
		return clone
	}
	return err
}

// strictSpec describes a strict coding task whose canonical commands the
// fixture commits.
type strictSpec struct {
	commands     []string
	deferred     []int
	inputs       []models.RuntimeInput
	recordInputs map[string]string // input ID → envelope content; an input left out is never recorded
	limits       []models.AdmissionLimit
	files        map[string]string
	commitMsg    string
}

// newStrictFixture commits an independently reviewed allocation on integration
// and a candidate worktree with its manifest, then writes a state that holds
// the executing task, its approved planning parent and the registered agent.
func newStrictFixture(t *testing.T, spec strictSpec) *checkFixture {
	t.Helper()
	requirePosixShell(t)
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.MustGit(t, root, "checkout", "integration")
	write := func(dir, name, contents string) {
		t.Helper()
		filename := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(root, "specs/acceptance-goal.md", "# Boundary\n\n## Identity\nReject malformed identity.\n")
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: record boundary requirement")
	sourceCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	contract := map[string]any{"version": 1, "manifest": "acceptance/task-1.json", "obligations": []string{"AC-identity"}, "validation": spec.commands, "timeout_seconds": 60, "approved_proofs": []any{}}
	if len(spec.deferred) > 0 {
		contract["deferred"] = spec.deferred
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	plan := fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n%s\n```\n", sourceCommit, encoded)
	write(root, "specs/acceptance-plan.md", plan)
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: independently reviewed acceptance allocation")
	parentCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	taskID := "task-check-submission"
	worktree := filepath.Join(".worktrees", taskID)
	wt := filepath.Join(root, worktree)
	testhelpers.MustGit(t, root, "worktree", "add", "-b", "task/"+taskID, wt, "integration")
	write(wt, "feature.go", "package main\n")
	write(wt, "feature_test.go", "package main\n")
	write(wt, "boundary_test.sh", "exit 0\n")
	write(wt, "acceptance/task-1.json", `{"version":1,"mappings":[{"obligation_id":"AC-identity","file":"boundary_test.sh","assertion":"identity assertion","command_index":0}]}`+"\n")
	for name, contents := range spec.files {
		write(wt, name, contents)
	}
	testhelpers.MustGit(t, wt, "add", "-A")
	message := spec.commitMsg
	if message == "" {
		message = "test: candidate with complete acceptance mapping"
	}
	testhelpers.MustGit(t, wt, "commit", "-m", message)
	commit := testhelpers.MustGit(t, wt, "rev-parse", "HEAD")

	now := time.Now().UTC()
	agentID := "coder-1"
	lease := now.Add(30 * time.Minute)
	parentID, planner, approver := "acceptance-parent", "code-planner-1", "code-plan-reviewer-1"
	planRef, specRef := "specs/acceptance-plan.md#Task 1", "specs/acceptance-goal.md"
	agent := testhelpers.RegisteredTestAgent(models.RoleCoder)
	agent.Status, agent.CurrentTask = models.AgentStatusWorking, &taskID
	state := &models.State{
		Sprint: models.Sprint{ID: "sprint-1", Number: 1, Timeline: models.SprintTimeline{Started: now}},
		Config: models.Config{IntegrationBranch: "integration", LeaseDuration: 1800, AdmissionLimits: spec.limits},
		Tasks: []models.Task{
			{
				ID: taskID, Description: "Task with a strict acceptance contract", Status: models.TaskStatusImplementing, RolePair: "coding-pair",
				AssignedTo: &agentID, LeaseExpires: &lease, Worktree: &worktree, BaseCommit: &parentCommit,
				PlanRef: planRef, SpecRef: specRef, Validation: spec.commands, ParentTask: &parentID, RuntimeInputs: spec.inputs,
				Iteration: 1, Created: now,
				History: []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventPreExecutionCheckpoint, Agent: &agentID,
					Extra: map[string]any{"intent": "exercise the local submission check", "validation_plan": "check-submission", "files_to_modify": []string{"feature.go"}}}},
			},
			{
				ID: parentID, Type: models.TaskTypePlanning, RolePair: "code-planning-pair", Status: models.TaskStatusMerged,
				AssignedTo: &planner, ApprovedBy: &approver, Approvals: []models.Approval{{Agent: approver, Timestamp: now}},
				BaseCommit: &sourceCommit, ReviewCommit: &parentCommit, MergeCommit: &parentCommit,
				Output:  []models.OutputEntry{{PlanRef: planRef, SpecRef: specRef, Validation: spec.commands, RuntimeInputs: spec.inputs}},
				Created: now,
			},
		},
		Agents: map[string]models.Agent{agentID: agent},
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	f := &checkFixture{
		root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir(), maxWait: 0,
		authority: &models.AgentAuthority{ID: agentID, Generation: testhelpers.TestAgentGeneration},
	}
	if len(spec.recordInputs) > 0 {
		useRuntimeInputKey(t)
		operator := t.TempDir()
		for input, content := range spec.recordInputs {
			envelope := filepath.Join(operator, input+".env")
			if err := os.WriteFile(envelope, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := RecordRuntimeInput(root, RecordRuntimeInputInput{Tasks: []string{taskID}, InputID: input, Envelope: envelope}); err != nil {
				t.Fatalf("RecordRuntimeInput(%s): %v", input, err)
			}
		}
	}
	return f
}

// baseFixture wraps the coding task of setupSuccessfulSubmitScenario: a test
// file, no acceptance contract and so no canonical batch.
func baseFixture(t *testing.T) *checkFixture {
	t.Helper()
	root, taskID, commit, agentID, bb := setupSuccessfulSubmitScenario(t)
	return &checkFixture{root: root, taskID: taskID, agentID: agentID, commit: commit, bb: bb, probe: t.TempDir()}
}

// recommit commits the worktree's staged changes and moves the fixture's
// submitted commit to the new HEAD.
func (f *checkFixture) recommit(t *testing.T, message string) {
	t.Helper()
	testhelpers.MustGit(t, f.worktree(), "commit", "-m", message)
	f.commit = testhelpers.MustGit(t, f.worktree(), "rev-parse", "HEAD")
}

// writeFile writes a file in the worktree without staging it.
func (f *checkFixture) writeFile(t *testing.T, name, contents string) {
	t.Helper()
	path := filepath.Join(f.worktree(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
