package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// D-79 live shape: a merged architecture over-placed a coding-order wait on
// its code plan; the generated, unstarted code plan carries it verbatim.

const cliDeferTransition = "code-plan-to-coding"

func cliDeferOutput() models.OutputEntry {
	return models.OutputEntry{Desc: "Plan the shared contract", DoneWhen: "Contract reviewed", Scope: "contract", SpecRef: "README.md"}
}

func cliDeferTask(id, rolePair string, status models.TaskStatus) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, time.Now().UTC())
	task.RolePair = rolePair
	return task
}

const cliDeferArchRef = "README.md"

func cliDeferDecomposition() *models.DecompositionManifest {
	return &models.DecompositionManifest{OwnedFiles: []string{"apps/store.py", "apps/README.md"}, ReadOnlyTaskDependsOn: []string{"peer"}, CoverageNotes: "Own the disposition writer."}
}

func cliGateWait() []models.ProviderDependency {
	return []models.ProviderDependency{{ProviderTask: "gate", Transition: cliDeferTransition, Outputs: []int{0}}}
}

func setupDeferProviderDependencyCLI(t *testing.T, mutate func(child *models.Task)) string {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef = "README.md"

	gate := cliDeferTask("gate", "code-planning-pair", models.TaskStatusMerged)
	gate.Output = []models.OutputEntry{cliDeferOutput()}
	peer := cliDeferTask("peer", "architecture-pair", models.TaskStatusMerged)
	peer.Output = []models.OutputEntry{cliDeferOutput()}
	arch := cliDeferTask("arch", "architecture-pair", models.TaskStatusMerged)
	output := cliDeferOutput()
	output.ProviderDependencies = cliGateWait()
	arch.Output = []models.OutputEntry{output}
	arch.TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	child := cliDeferTask("arch-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	child.ParentTasks = []string{arch.ID}
	child.ProviderDependencies = cliGateWait()
	child.ArchRef = cliDeferArchRef
	child.Decomposition = cliDeferDecomposition()
	if mutate != nil {
		mutate(&child)
	}
	state.Tasks = []models.Task{gate, peer, arch, child}
	for _, task := range state.Tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	state.Agents["code-planner-1"] = testhelpers.RegisteredTestAgent("code-planner")
	testhelpers.WriteInitialState(t, statePath, state)
	return root
}

func readDeferCLIState(t *testing.T, root string) *models.State {
	t.Helper()
	state, err := db.For(paths.New(root).StatePath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// cliDescendants reads descendant_dependencies back from the task's YAML form.
func cliDescendants(t *testing.T, task *models.Task) []map[string]any {
	t.Helper()
	raw, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Deps []map[string]any `yaml:"descendant_dependencies"`
	}
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.Deps
}

func deferArgs(extra ...string) []string {
	return append([]string{"defer-provider-dependency", "arch-cp-0", "--provider-task", "gate", "--transition", cliDeferTransition, "--reason", "D64: the writer-order ruling gates coding only"}, extra...)
}

func TestDeferProviderDependencyCLI(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonMode], func(t *testing.T) {
			root := setupDeferProviderDependencyCLI(t, nil)
			var stdout string
			var err error
			if jsonMode {
				stdout, err = executeRootCommandCapture(t, root, deferArgs("--json")...)
			} else {
				var output bytes.Buffer
				rootCmd.SetOut(&output)
				rootCmd.SetArgs(append([]string{"-C", root}, deferArgs()...))
				err = rootCmd.Execute()
				stdout = output.String()
			}
			if err != nil {
				t.Fatal(err)
			}
			if jsonMode {
				envelope := parseEnvelope(t, stdout)
				result, _ := envelope["result"].(map[string]any)
				if envelope["ok"] != true || result["task_id"] != "arch-cp-0" || result["at_transition"] != cliDeferTransition {
					t.Fatalf("missing success envelope: %s", stdout)
				}
			} else if !strings.Contains(stdout, "arch-cp-0") || !strings.Contains(stdout, cliDeferTransition) {
				t.Fatalf("missing acknowledgement: %s", stdout)
			}

			// THEN the wait moved in place to the plan's descendants, with an audit entry.
			state := readDeferCLIState(t, root)
			child := state.FindTask("arch-cp-0")
			if len(child.ProviderDependencies) != 0 {
				t.Fatalf("wrong-stage wait still holds the planner: %+v", child.ProviderDependencies)
			}
			want := []map[string]any{{"at_transition": cliDeferTransition, "provider_dependencies": []any{map[string]any{"provider_task": "gate", "transition": cliDeferTransition, "outputs": []any{0}}}}}
			if got := cliDescendants(t, child); !reflect.DeepEqual(got, want) {
				t.Fatalf("descendant_dependencies = %#v, want %#v", got, want)
			}
			last := child.History[len(child.History)-1]
			if last.Event != "provider_dependency_deferred" || last.Reason == nil || !strings.Contains(*last.Reason, "D64") {
				t.Fatalf("missing deferral audit entry: %+v", last)
			}
			if child.Status != models.TaskStatusDraftCodingPlan || !reflect.DeepEqual(state.FindTask("arch").Output[0].ProviderDependencies, cliGateWait()) {
				t.Fatal("deferral changed the planner's status or the reviewed architecture output")
			}
			if child.ArchRef != cliDeferArchRef || !reflect.DeepEqual(child.Decomposition, cliDeferDecomposition()) {
				t.Fatalf("deferral lost the planner's scope: arch_ref=%q decomposition=%+v", child.ArchRef, child.Decomposition)
			}
			// AND planning is admissible while the gate writer is unborn.
			resetRootCmdForTest(t)
			if _, err := ops.ClaimTask(root, "arch-cp-0", "code-planner-1"); err != nil {
				t.Fatalf("deferred planner still held: %v", err)
			}
		})
	}
}

func TestDeferProviderDependencyCLIRefusals(t *testing.T) {
	claimed := func(child *models.Task) {
		child.History = append(child.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: testhelpers.StringPtr("code-planner-1")})
	}
	planningLevel := func(child *models.Task) {
		child.ProviderDependencies = append(child.ProviderDependencies, models.ProviderDependency{ProviderTask: "peer", Transition: "architecture-to-code-plan", Outputs: []int{0}})
	}
	cases := []struct {
		name      string
		mutate    func(*models.Task)
		args      []string
		agent     bool
		fragments []string
	}{
		{name: "agent identity", agent: true, args: deferArgs("--json"), fragments: []string{"operator-only"}},
		{name: "claimed planner", mutate: claimed, args: deferArgs("--json"), fragments: []string{"arch-cp-0", "unstarted"}},
		{name: "undeclared provider", args: []string{"defer-provider-dependency", "arch-cp-0", "--provider-task", "peer", "--transition", cliDeferTransition, "--reason", "r", "--json"}, fragments: []string{"peer", "does not declare"}},
		{name: "planning-level wait", mutate: planningLevel, args: []string{"defer-provider-dependency", "arch-cp-0", "--provider-task", "peer", "--transition", "architecture-to-code-plan", "--reason", "r", "--json"}, fragments: []string{"architecture-to-code-plan", cliDeferTransition}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := setupDeferProviderDependencyCLI(t, tc.mutate)
			if tc.agent {
				t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
			}
			before, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := executeRootCommandCapture(t, root, tc.args...)
			if err == nil {
				t.Fatalf("refusal case accepted: %s", stdout)
			}
			for _, fragment := range tc.fragments {
				if !strings.Contains(stdout, fragment) {
					t.Fatalf("refusal %s does not name %q", stdout, fragment)
				}
			}
			if parseEnvelope(t, stdout)["ok"] != false {
				t.Fatal("missing refusal envelope")
			}
			after, err := os.ReadFile(paths.New(root).StatePath())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("refusal changed state")
			}
		})
	}
}

func TestDeferProviderDependencyCLIRequiresFlags(t *testing.T) {
	for _, args := range [][]string{
		{"defer-provider-dependency", "arch-cp-0", "--transition", cliDeferTransition, "--reason", "r"},
		{"defer-provider-dependency", "arch-cp-0", "--provider-task", "gate", "--reason", "r"},
		{"defer-provider-dependency", "arch-cp-0", "--provider-task", "gate", "--transition", cliDeferTransition},
	} {
		root := setupDeferProviderDependencyCLI(t, nil)
		before, err := os.ReadFile(paths.New(root).StatePath())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := executeRootCommandCapture(t, root, args...); err == nil || !strings.Contains(err.Error(), "required flag") {
			t.Fatalf("accepted missing flag %v: %v", args, err)
		}
		after, err := os.ReadFile(paths.New(root).StatePath())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("flag refusal changed state")
		}
	}
}
