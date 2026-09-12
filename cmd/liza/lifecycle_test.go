package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleCLI_AllMutationCommandsRequireOriginalPair(t *testing.T) {
	commands := [][]string{
		{"submit-for-review", "target"}, {"submit-verdict", "target", "APPROVED"},
		{"mark-blocked", "target"}, {"assess-blocked", "target"},
		{"assess-hypothesis-exhausted", "target"}, {"claim-task", "target", "coder-1"},
		{"release-claim", "target"}, {"wt-merge", "target"}, {"recover-task", "target"},
		{"retarget-dependency", "target", "old", "new"}, {"apply-dependency-repair", "target"},
		{"repair-superseded-dependencies", "target"}, {"cancel-task", "target", "reason"},
		{"supersede-task", "target"}, {"unblock-task", "target"}, {"set-task-output", "target"},
		{"handoff", "target", "summary", "next"}, {"recover-agent", "coder-1"},
	}
	for _, args := range commands {
		t.Run(args[0], func(t *testing.T) {
			root, statePath := setupMutationTestProject(t, nil)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, flags := range [][]string{{"--request-id", "same-request"}, {"--expected-transition", strings.Repeat("a", 64)}, {"--request-id=", "--expected-transition="}, {"--request-id="}, {"--expected-transition="}} {
				call := append(append([]string{}, args...), flags...)
				call = append(call, "--json")
				stdout, err := executeRootCommandCapture(t, root, call...)
				if err == nil {
					t.Fatal("unpaired lifecycle request accepted")
				}
				env := parseEnvelope(t, stdout)
				result, ok := env["result"].(map[string]any)
				if !ok || env["ok"] != false || result["outcome"] != models.LifecycleInvalidInput || result["safe_action"] != "correct_input" || result["effects"] != "none" {
					t.Fatalf("invalid pair lacks lifecycle policy: %s", stdout)
				}
				if strings.Contains(stdout, testhelpers.TestAgentGeneration) || result["current_assignee"] != nil || result["transition_id"] != nil {
					t.Fatal("pre-admission failure exposed authority or a fabricated task observation")
				}
			}
			after, err := os.ReadFile(statePath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("input rejection mutated state: %v", err)
			}
			metrics := ops.ReadLifecycleOutcomes(root, ops.CaptureLifecycleSprint(readState(t, statePath).Sprint))
			if !metrics.Available || metrics.Counts[args[0]][models.LifecycleInvalidInput] != 5 {
				t.Fatalf("pre-ops invocations not counted exactly once: %+v", metrics)
			}
		})
	}
}

func assertLifecycleFailurePolicy(t *testing.T, envelope map[string]any, outcome, action string) {
	t.Helper()
	result, ok := envelope["result"].(map[string]any)
	if !ok || envelope["ok"] != false || result["outcome"] != outcome || result["safe_action"] != action || result["effects"] != "none" {
		t.Fatalf("failure policy = %#v, want %s/%s without effects", envelope, outcome, action)
	}
}

func TestLifecycleCLI_MetadataReplayPreservesIdentityAndCountsOnce(t *testing.T) {
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
	})
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	state := readState(t, statePath)
	original := models.TaskTransitionID(state.FindTask("target"))
	args := []string{"assess-blocked", "target", "--note", "inspected", "--agent-id", "orchestrator-1", "--request-id", "same-request", "--expected-transition", original, "--json"}
	first, err := executeRootCommandCapture(t, root, args...)
	if err != nil {
		t.Fatalf("first invocation: %v", err)
	}
	firstResult := parseEnvelope(t, first)["result"].(map[string]any)
	if firstResult["outcome"] != models.LifecycleCompleted {
		t.Fatalf("first result: %s", first)
	}
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := executeRootCommandCapture(t, root, args...)
	if err != nil {
		t.Fatalf("same request retry: %v", err)
	}
	env := parseEnvelope(t, second)
	replay := env["result"].(map[string]any)
	if env["ok"] != true || replay["outcome"] != models.LifecycleAlreadyCompleted || replay["safe_action"] != "stop" || replay["completed_transition_id"] != firstResult["completed_transition_id"] {
		t.Fatalf("exact retry lost completion identity or current policy: %s", second)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("replay rewrote state: %v", err)
	}
	metrics := ops.ReadLifecycleOutcomes(root, ops.CaptureLifecycleSprint(readState(t, statePath).Sprint))
	if !metrics.Available || metrics.Counts["assess-blocked"][models.LifecycleCompleted] != 1 || metrics.Counts["assess-blocked"][models.LifecycleAlreadyCompleted] != 1 {
		t.Fatalf("CLI and ops double-counted invocation: %+v", metrics)
	}
}

func TestLifecycleCLI_RBACAndArgumentFailuresExposePolicy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		args            []string
		outcome, action string
	}{
		{"forbidden", []string{"claim-task", "target", "orchestrator-1", "--json"}, models.LifecycleForbidden, "stop"},
		{"missing-argument", []string{"submit-for-review", "--json"}, models.LifecycleInvalidInput, "correct_input"},
		{"missing-required-flag", []string{"mark-blocked", "target", "--json"}, models.LifecycleInvalidInput, "correct_input"},
		{"json-respawn", []string{"recover-agent", "coder-1", "--cli", "codex", "--json"}, models.LifecycleInvalidInput, "correct_input"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := setupMutationTestProject(t, nil)
			stdout, err := executeRootCommandCapture(t, root, tc.args...)
			if err == nil {
				t.Fatal("expected hard CLI failure")
			}
			env := parseEnvelope(t, stdout)
			result, ok := env["result"].(map[string]any)
			if !ok || env["ok"] != false || result["outcome"] != tc.outcome || result["safe_action"] != tc.action {
				t.Fatalf("wrong policy: %s", stdout)
			}
		})
	}
}
