package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupReplaceTaskCLI(t *testing.T, collision bool, consumers ...string) (string, string, []string) {
	t.Helper()
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Goal.SpecRef = "README.md"
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("source", models.TaskStatusReady, time.Now().UTC())}
		if collision {
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("replacement", models.TaskStatusReady, time.Now().UTC()))
		}
		for _, id := range consumers {
			consumer := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, time.Now().UTC())
			consumer.DependsOn = []string{"source"}
			state.Tasks = append(state.Tasks, consumer)
		}
		state.Agents["coder-1"] = mutationTestAgent("coder")
	})
	input := ops.ReplaceTaskInput{
		SourceTaskID: "source", Reason: "reviewed correction", Consumers: []models.DependencyUpdate{},
		Replacement: ops.AddTaskInput{ID: "replacement", RolePair: "coding-pair", Description: "replacement work", SpecRef: "README.md", DoneWhen: "behavior verified", Scope: "command", Priority: 1},
	}
	for _, id := range consumers {
		input.Consumers = append(input.Consumers, models.DependencyUpdate{
			TaskID: id, ExpectedDependsOn: []string{"source"}, DesiredDependsOn: []string{"replacement"},
		})
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "replacement.json")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	token := models.TaskTransitionID(mustFindTask(t, readState(t, statePath), "source"))
	return root, statePath, []string{"replace-task", "--replacement-file", file, "--request-id", "replace-cli", "--expected-transition", token}
}

func executeReplaceTaskCLI(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd, _, err := rootCmd.Find([]string{"replace-task"})
	if err != nil || cmd.Name() != "replace-task" {
		t.Fatalf("replace-task command is not registered: %v", err)
	}
	resetFlagIfPresent(cmd, "replacement-file")
	t.Cleanup(func() { resetFlagIfPresent(cmd, "replacement-file") })
	return executeRootCommandCapture(t, root, args...)
}

func TestReplaceTaskCmd_JSONEnvelope(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "completed"
		if collision {
			name = "conflict"
		}
		t.Run(name, func(t *testing.T) {
			root, statePath, args := setupReplaceTaskCLI(t, collision)
			before := readStateBytes(t, statePath)
			stdout, err := executeReplaceTaskCLI(t, root, append(args, "--agent-id", "orchestrator-1", "--json")...)
			result := envelopeResult(t, stdout)
			if result["operation"] != "replace-task" || result["transition_id"] == "" || result["transition_id"] == nil {
				t.Fatalf("missing operation or observed boundary: %s", stdout)
			}
			if collision {
				if err == nil {
					t.Fatal("conflict succeeded")
				}
				assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleConflict, "stop")
				diagnostics, ok := result["diagnostics"].([]any)
				if !ok || len(diagnostics) != 1 || diagnostics[0].(map[string]any)["field"] != "replacement.id" {
					t.Fatalf("missing collision diagnostic: %s", stdout)
				}
				if _, present := result["changed"]; present || readStateBytes(t, statePath) != before {
					t.Fatal("conflict claimed or wrote a change")
				}
				return
			}
			if err != nil || parseEnvelope(t, stdout)["ok"] != true || result["outcome"] != models.LifecycleCompleted || result["safe_action"] != "continue" || result["changed"] != true || result["effects"] != "committed" {
				t.Fatalf("completion envelope: %s, error: %v", stdout, err)
			}
			if result["source_task_id"] != "source" || result["replacement_task_id"] != "replacement" {
				t.Fatalf("lost ops result fields: %s", stdout)
			}
		})
	}
}

func TestReplaceTaskCmd_RBAC(t *testing.T) {
	root, statePath, args := setupReplaceTaskCLI(t, false)
	before := readStateBytes(t, statePath)
	stdout, err := executeReplaceTaskCLI(t, root, append(args, "--agent-id", "coder-1", "--json")...)
	if err == nil {
		t.Fatal("coder invocation succeeded")
	}
	assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleForbidden, "stop")
	if readStateBytes(t, statePath) != before {
		t.Fatal("unauthorized invocation changed state")
	}
}

func TestReplaceTaskCmd_RequiresFile(t *testing.T) {
	root, statePath, _ := setupReplaceTaskCLI(t, false)
	before := readStateBytes(t, statePath)
	stdout, err := executeReplaceTaskCLI(t, root, "replace-task", "--agent-id", "orchestrator-1", "--json")
	if err == nil {
		t.Fatal("missing file succeeded")
	}
	assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleInvalidInput, "correct_input")
	if !strings.Contains(stdout, "--replacement-file is required") || readStateBytes(t, statePath) != before {
		t.Fatalf("missing-file rejection: %s", stdout)
	}
}

func TestReplaceTaskCmd_InvalidPayload(t *testing.T) {
	for _, payload := range []string{"null", "{}", `{"source_task_id":42}`, `{"source_task_id":"PRIVATE_REJECTED_VALUE" invalid}`} {
		t.Run(payload, func(t *testing.T) {
			root, statePath, args := setupReplaceTaskCLI(t, false)
			before := readStateBytes(t, statePath)
			if err := os.WriteFile(args[2], []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, err := executeReplaceTaskCLI(t, root, append(args, "--agent-id", "orchestrator-1", "--json")...)
			if err == nil {
				t.Fatal("invalid payload succeeded")
			}
			assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleInvalidInput, "correct_input")
			if strings.Contains(stdout, "PRIVATE_REJECTED_VALUE") || readStateBytes(t, statePath) != before {
				t.Fatalf("rejection leaked payload or changed state: %s", stdout)
			}
		})
	}
}

func TestReplaceTaskCmd_Text(t *testing.T) {
	root, _, args := setupReplaceTaskCLI(t, false)
	stdout, err := executeReplaceTaskCLI(t, root, append(args, "--agent-id", "orchestrator-1")...)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"outcome: COMPLETED", "safe_action: continue", "Replaced task source with replacement"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output %q lacks %q", stdout, want)
		}
	}
}

func TestReplaceTaskCmd_BrandBoundary(t *testing.T) {
	bin := buildNonDefaultBrandBinary(t)
	help := runBrandSmokeCommand(t, bin, "replace-task", "--help")
	assertContains(t, help, "--replacement-file")
	assertContains(t, help, "acme-agent replace-task")
	assertNoDefaultBrandLeaks(t, "replacement help", help)
	cmd := exec.Command(bin, "replace-task", "--json")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "ACME_AGENT_SKIP_AUTO_UPDATE=1")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("missing-file invocation succeeded")
	}
	assertLifecycleFailurePolicy(t, parseEnvelope(t, string(output)), models.LifecycleInvalidInput, "correct_input")
	assertNoDefaultBrandLeaks(t, "replacement error", string(output))
}

type replaceTaskE2EFixture struct {
	root      string
	statePath string
	binary    string
	args      []string
}

func setupReplaceTaskE2E(t *testing.T) replaceTaskE2EFixture {
	t.Helper()
	root, statePath, args := setupReplaceTaskCLI(t, false, "consumer-a", "consumer-b", "consumer-c")
	binary := filepath.Join(t.TempDir(), "replace-task-e2e.exe")
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}
	t.Setenv(brand.EnvName("SKIP_AUTO_UPDATE"), "1")
	t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "orchestrator-1")
	t.Setenv(brand.LegacyEnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)
	return replaceTaskE2EFixture{
		root: root, statePath: statePath, binary: binary,
		args: append(args, "--agent-id", "orchestrator-1", "--json"),
	}
}

func (f replaceTaskE2EFixture) run(t *testing.T) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, f.binary, f.args...)
	cmd.Dir = f.root
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("replacement CLI timed out: %s", output)
	}
	return string(output), err
}

func (f replaceTaskE2EFixture) complete(t *testing.T) map[string]any {
	t.Helper()
	stdout, err := f.run(t)
	result := envelopeResult(t, stdout)
	if err != nil || parseEnvelope(t, stdout)["ok"] != true || result["outcome"] != models.LifecycleCompleted || result["changed"] != true || result["safe_action"] != "continue" || result["effects"] != "committed" {
		t.Fatalf("replacement failed: %v\n%s", err, stdout)
	}
	if result["source_task_id"] != "source" || result["replacement_task_id"] != "replacement" {
		t.Fatalf("unexpected lineage: %s", stdout)
	}
	return result
}

func TestReplaceTaskE2E_Lineage(t *testing.T) {
	f := setupReplaceTaskE2E(t)
	f.complete(t)

	// The observer reads the state persisted by the separate CLI process.
	state := readState(t, f.statePath)
	source := mustFindTask(t, state, "source")
	if source.Status != models.TaskStatusSuperseded || !slices.Equal(source.SupersededBy, []string{"replacement"}) {
		t.Fatalf("source lineage: status=%s superseded_by=%v", source.Status, source.SupersededBy)
	}
	resolver, err := loadResolverForRBAC(f.root)
	if err != nil {
		t.Fatal(err)
	}
	replacement := mustFindTask(t, state, "replacement")
	if !replacement.IsClaimable("coder", state.Tasks, resolver) || replacement.AssignedTo != nil {
		t.Fatalf("replacement is not claimable: %+v", replacement)
	}
	for _, id := range []string{"consumer-a", "consumer-b", "consumer-c"} {
		consumer := mustFindTask(t, state, id)
		if !slices.Equal(consumer.DependsOn, []string{"replacement"}) {
			t.Errorf("%s depends_on=%v, want [replacement]", id, consumer.DependsOn)
		}
	}
	if len(state.Tasks) != 5 {
		t.Fatalf("got %d tasks, want one source, one replacement and three consumers", len(state.Tasks))
	}
}

func TestReplaceTaskE2E_Replay(t *testing.T) {
	f := setupReplaceTaskE2E(t)
	first := f.complete(t)
	before := readStateBytes(t, f.statePath)

	stdout, err := f.run(t)
	result := envelopeResult(t, stdout)
	if err != nil || parseEnvelope(t, stdout)["ok"] != true || result["outcome"] != models.LifecycleAlreadyCompleted || result["changed"] != false || result["effects"] != "none" {
		t.Fatalf("identical replay failed: %v\n%s", err, stdout)
	}
	if result["replacement_task_id"] != first["replacement_task_id"] || result["completed_transition_id"] != first["transition_id"] {
		t.Fatalf("replay lost original completion: first=%v replay=%v", first, result)
	}
	if readStateBytes(t, f.statePath) != before {
		t.Fatal("identical replay changed persisted state or history")
	}
}

func TestReplaceTaskE2E_Conflict(t *testing.T) {
	f := setupReplaceTaskE2E(t)
	f.complete(t)
	before := readStateBytes(t, f.statePath)

	// Keep the request identity and original boundary, but change its intent.
	payload, err := os.ReadFile(f.args[2])
	if err != nil {
		t.Fatal(err)
	}
	var input ops.ReplaceTaskInput
	if err := json.Unmarshal(payload, &input); err != nil {
		t.Fatal(err)
	}
	input.Replacement.Description = "conflicting replacement work"
	payload, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.args[2], payload, 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, err := f.run(t)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() <= 0 {
		t.Fatalf("conflict must exit nonzero: %v\n%s", err, stdout)
	}
	assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleConflict, "stop")
	if readStateBytes(t, f.statePath) != before {
		t.Fatal("conflicting replay changed persisted state or history")
	}
}
