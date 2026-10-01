package ops

import (
	"fmt"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// D65: a runtime-input refusal is a block only an operator can clear, so both
// refusal paths record the operator question as the episode's human ask,
// without any orchestrator involvement.

// requireBlockedEntryAsksHuman asserts the task's latest blocked entry carries
// the persisted awaiting_human ask, equal to the blocked question.
func requireBlockedEntryAsksHuman(t *testing.T, task *models.Task) {
	t.Helper()
	if task.Status != models.TaskStatusBlocked || len(task.BlockedQuestions) != 1 {
		t.Fatalf("task = %s %q, want BLOCKED with one operator question", task.Status, task.BlockedQuestions)
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventBlocked {
			continue
		}
		if ask, _ := entry.Extra["awaiting_human"].(string); ask != task.BlockedQuestions[0] {
			t.Fatalf("blocked entry awaiting_human = %q, want the operator question %q", ask, task.BlockedQuestions[0])
		}
		return
	}
	t.Fatal("no blocked history entry")
}

func TestRuntimeInputClaimRefusalBlockAsksTheHuman(t *testing.T) {
	// GIVEN a claim refused because declared inputs have no instance
	f, refusal := claimRefusedForMissingInputs(t)

	// WHEN the supervisor escalates the refusal
	blocked, err := BlockAcceptanceRefusedTask(f.root, acceptanceCoderAuthority(f.agentID), refusal)
	if err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
	}

	// THEN the block names the operator action as the human ask
	requireBlockedEntryAsksHuman(t, f.state(t).FindTask(f.taskID))
}

func TestRuntimeInputSubmissionRefusalBlockAsksTheHuman(t *testing.T) {
	// GIVEN a spent single_use input after a failed gate run
	f := setupRuntimeInputScenario(t, 3)
	if _, err := SubmitForReview(f.root, f.taskID, f.commit, f.agentID); err == nil {
		t.Fatal("failing canonical command was admitted")
	}

	// WHEN the doer resubmits and the gate refuses the input
	_, err := SubmitForReview(f.root, f.taskID, f.commit, f.agentID)
	if refusal := requireRuntimeInputRefusal(t, err, "runtime_input_consumed:w03"); !refusal.Blocked {
		t.Fatalf("refusal = %+v, want the task blocked", refusal)
	}

	// THEN the block names the operator action as the human ask
	requireBlockedEntryAsksHuman(t, f.state(t).FindTask(f.taskID))
}

// maxRuntimeInputRefusal returns the refusal a task declaring the most inputs
// with the longest IDs the declaration rules accept would get with all of them
// missing.
func maxRuntimeInputRefusal(t *testing.T, taskID string) *AcceptanceEvidenceError {
	t.Helper()
	return runtimeInputRefusalWithIDs(t, taskID, 128)
}

// runtimeInputRefusalWithIDs returns the refusal for 64 missing inputs whose
// IDs are idLen bytes, after checking the declarations are valid.
func runtimeInputRefusalWithIDs(t *testing.T, taskID string, idLen int) *AcceptanceEvidenceError {
	t.Helper()
	var declarations []models.RuntimeInput
	var refusals []runtimeInputResolution
	for i := 0; i < 64; i++ {
		prefix := fmt.Sprintf("input-%02d-", i)
		input := models.RuntimeInput{
			ID: prefix + strings.Repeat("x", idLen-len(prefix)), Commands: []string{runtimeInputCommand},
			Recipe: "project.w03", Consumption: models.RuntimeInputSingleUse, Env: []string{fmt.Sprintf("FIXTURE_%02d", i)},
		}
		declarations = append(declarations, input)
		refusals = append(refusals, runtimeInputResolution{declaration: input, code: "runtime_input_missing"})
	}
	if err := models.ValidateRuntimeInputs([]string{runtimeInputCommand}, declarations); err != nil {
		t.Fatalf("declarations at the limit rejected: %v", err)
	}
	return runtimeInputRefusalError(taskID, refusals, nil)
}

func TestRuntimeInputHumanAskIsBoundedAtTheDeclarationLimit(t *testing.T) {
	refusal := maxRuntimeInputRefusal(t, "task-1")
	_, question := runtimeInputBlockText(refusal)

	ask := runtimeInputHumanAsk(refusal)

	if len(question) <= 4096 {
		t.Fatalf("question is %d bytes; the fixture no longer exceeds the human_action bound", len(question))
	}
	if len(ask) > runtimeInputHumanAskLimit {
		t.Fatalf("ask is %d bytes, want at most %d", len(ask), runtimeInputHumanAskLimit)
	}
	if diagnostics := payloadschema.ValidateHumanAction("/human_action", ask); len(diagnostics) != 0 {
		t.Fatalf("ask rejected: %+v", diagnostics)
	}
	firstCode := strings.Split(refusal.Reason, ", ")[0]
	for _, want := range []string{firstCode, "more listed in blocked_questions", brand.Command("unblock-task", "task-1")} {
		if !strings.Contains(ask, want) {
			t.Errorf("ask = %q, want it to contain %q", ask, want)
		}
	}
}

// The submission path stores the full reason, which state hygiene bounds at
// 4096 bytes, but not the question, which is longer by the operator action. A
// refusal in between must still block: its ask is bounded, not the question.
func TestRuntimeInputSubmissionRefusalAtTheLimitStillBlocks(t *testing.T) {
	// GIVEN a submission refusal whose reason fits the state text limit and
	// whose question exceeds the human_action bound
	f := setupRuntimeInputScenario(t, 0)
	refusal := runtimeInputRefusalWithIDs(t, f.taskID, 38)
	if reason, question := runtimeInputBlockText(refusal); len(reason) > 4096 || len(question) <= 4096 {
		t.Fatalf("reason %d / question %d bytes; want the reason within 4096 and the question beyond it", len(reason), len(question))
	}

	// WHEN the submission path blocks the task
	blockRuntimeInputRefusal(f.root, f.taskID, f.agentID, nil, refusal)

	// THEN it is BLOCKED with the bounded ask, and the question keeps every code
	task := f.state(t).FindTask(f.taskID)
	if !refusal.Blocked || refusal.BlockFailed || task.Status != models.TaskStatusBlocked {
		t.Fatalf("Blocked=%v BlockFailed=%v status=%s, want BLOCKED", refusal.Blocked, refusal.BlockFailed, task.Status)
	}
	requireAskAtTheLimit(t, task, refusal)
}

func TestRuntimeInputClaimRefusalAtTheLimitStillBlocks(t *testing.T) {
	// GIVEN a current claim refusal whose code list is at the declaration limit
	f, refusal := claimRefusedForMissingInputs(t)
	refusal.Reason = maxRuntimeInputRefusal(t, f.taskID).Reason

	// WHEN the supervisor escalates it
	blocked, err := BlockAcceptanceRefusedTask(f.root, acceptanceCoderAuthority(f.agentID), refusal)
	if err != nil || !blocked {
		t.Fatalf("BlockAcceptanceRefusedTask() = %v, %v; want blocked", blocked, err)
	}

	// THEN the same bounded ask is recorded
	requireAskAtTheLimit(t, f.state(t).FindTask(f.taskID), refusal)
}

func requireAskAtTheLimit(t *testing.T, task *models.Task, refusal *AcceptanceEvidenceError) {
	t.Helper()
	human, ok := models.CurrentAwaitingHuman(task)
	if !ok || human.Ask != runtimeInputHumanAsk(refusal) {
		t.Fatalf("ask = %q (%v), want the bounded runtime-input ask", human.Ask, ok)
	}
	if len(task.BlockedQuestions) != 1 || !strings.Contains(task.BlockedQuestions[0], refusal.Reason) {
		t.Fatal("blocked question lost refused inputs")
	}
}
