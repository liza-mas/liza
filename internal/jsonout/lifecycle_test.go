package jsonout

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleErrorEnvelopeHasSafeAction(t *testing.T) {
	const losing = "test-losing-generation-json-155"
	const current = "test-current-generation-json-155"
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1": {Generation: current},
	}}
	authorityErr := ops.RequireAgentAuthority(state, models.AgentAuthority{ID: "coder-1", Generation: losing})
	var output bytes.Buffer
	err := WriteResult(&output, nil, nil, authorityErr)
	if !errors.Is(err, ErrAlreadyWritten) {
		t.Fatalf("authority rejection must retain hard-failure exit signaling: %v", err)
	}
	var envelope struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
		Error  *ErrorDetail   `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error == nil {
		t.Error("authority rejection must remain an error envelope")
	}
	for key, want := range map[string]string{"outcome": "STALE_CALLER", "safe_action": "stop"} {
		if envelope.Result[key] != want {
			t.Errorf("result.%s = %v, want %s", key, envelope.Result[key], want)
		}
	}
	for _, generation := range []string{losing, current} {
		if strings.Contains(output.String(), generation) {
			t.Error("JSON envelope exposes a registration generation")
		}
	}
}

// An add-task acceptance refusal reaches agents as the lifecycle outcome with
// its field diagnostic, not only the generic validation code.
func TestAddTaskAcceptanceRefusalEnvelopeCarriesFieldDiagnostic(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, logPath := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	testhelpers.WriteInitialState(t, statePath, state)
	// README.md is committed on integration and has no such heading, so claim
	// would refuse the task at every commit that keeps this file unchanged.
	input := &ops.AddTaskInput{ID: "adhoc", RolePair: "coding-pair", Description: "repair", SpecRef: "README.md",
		PlanRef: "README.md#no-such-heading", DoneWhen: "done", Scope: "repair", Priority: 1}
	_, addErr := ops.AddTaskWithAuthority(statePath, logPath, input, models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration})

	var output bytes.Buffer
	if err := WriteResult(&output, nil, nil, addErr); !errors.Is(err, ErrAlreadyWritten) {
		t.Fatalf("refusal must be an error envelope: addErr=%v writeErr=%v", addErr, err)
	}
	var envelope struct {
		Result struct {
			Operation   string                   `json:"operation"`
			Outcome     string                   `json:"outcome"`
			SafeAction  string                   `json:"safe_action"`
			Diagnostics []models.FieldDiagnostic `json:"diagnostics"`
		} `json:"result"`
		Error *ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	r := envelope.Result
	if envelope.Error == nil || envelope.Error.Code != "validation" || r.Operation != "add-task" || r.Outcome != models.LifecycleInvalidInput || r.SafeAction != "correct_input" {
		t.Fatalf("envelope = %s", output.String())
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Field != "plan_ref" || !strings.Contains(r.Diagnostics[0].Constraint, "acceptance.source") {
		t.Fatalf("diagnostics = %+v", r.Diagnostics)
	}
}
