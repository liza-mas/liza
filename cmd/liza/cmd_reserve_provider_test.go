package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// D-80: reserve-provider makes an existing unstarted writer wait for every
// child a correction generates, and sets the correction's output cap when
// other writers select it by index.

const reserveProviderTransition = "architecture-to-code-plan"

// setupReserveProviderCLI: vb-cp-0 (Unit 5) is the unstarted generated child
// of MERGED plan vb; corr is a correction architecture in its initial status.
func setupReserveProviderCLI(t *testing.T, mutate func(*models.State)) (string, string) {
	t.Helper()
	return setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC()
		state.Goal.SpecRef = "README.md"
		vb := testhelpers.BuildTaskByStatus("vb", models.TaskStatusMerged, now)
		vb.RolePair = "architecture-pair"
		vb.Output = []models.OutputEntry{{Desc: "Plan the shared contract", DoneWhen: "Contract reviewed", Scope: "contract", SpecRef: "README.md"}}
		vb.TransitionsExecuted = map[string]bool{reserveProviderTransition: true}
		unit5 := testhelpers.BuildTaskByStatus("vb-cp-0", models.TaskStatusDraftCodingPlan, now)
		unit5.RolePair = "code-planning-pair"
		unit5.ParentTasks = []string{"vb"}
		corr := testhelpers.BuildTaskByStatus("corr", models.TaskStatus("DRAFT_ARCHITECTURE"), now)
		corr.RolePair = "architecture-pair"
		state.Tasks = []models.Task{vb, unit5, corr}
		state.Agents["coder-1"] = mutationTestAgent("coder")
		if mutate != nil {
			mutate(state)
		}
	})
}

func runReserveProvider(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd, _, err := rootCmd.Find([]string{"reserve-provider"})
	if err != nil || cmd.Name() != "reserve-provider" {
		t.Fatalf("reserve-provider command is not registered: %v", err)
	}
	for _, flag := range []string{"transition", "reason", "release", "provider-max-outputs"} {
		resetFlagIfPresent(cmd, flag)
	}
	t.Cleanup(func() {
		for _, flag := range []string{"transition", "reason", "release", "provider-max-outputs"} {
			resetFlagIfPresent(cmd, flag)
		}
	})
	return executeRootCommandCapture(t, root, append(append([]string{"reserve-provider"}, args...), "--json")...)
}

// stateTaskField reads key of one task from the state file, nil when absent.
func stateTaskField(t *testing.T, statePath, taskID, key string) any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Tasks []map[string]any `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, task := range raw.Tasks {
		if task["id"] == taskID {
			return task[key]
		}
	}
	t.Fatalf("task %s not in state file", taskID)
	return nil
}

func reserveArgs(consumer, provider string, extra ...string) []string {
	return append([]string{consumer, provider, "--transition", reserveProviderTransition, "--reason", "D65 placement", "--agent-id", "orchestrator-1"}, extra...)
}

func requireReserveRefused(t *testing.T, root, statePath string, args []string, fragments ...string) {
	t.Helper()
	before := readStateBytes(t, statePath)
	stdout, err := runReserveProvider(t, root, args...)
	if err == nil || parseEnvelope(t, stdout)["ok"] != false {
		t.Fatalf("refusal expected, got %v: %s", err, stdout)
	}
	for _, fragment := range fragments {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("refusal %s does not name %q", stdout, fragment)
		}
	}
	if readStateBytes(t, statePath) != before {
		t.Fatal("refused reservation changed state")
	}
}

func TestReserveProviderCLIAddsAndReleases_D80(t *testing.T) {
	root, statePath := setupReserveProviderCLI(t, nil)

	stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr")...)
	if err != nil || parseEnvelope(t, stdout)["ok"] != true {
		t.Fatalf("reservation refused: %v %s", err, stdout)
	}
	if stateTaskField(t, statePath, "vb-cp-0", "provider_reservations") == nil {
		t.Fatal("reservation not persisted")
	}
	// A retry is a no-op success.
	before := readStateBytes(t, statePath)
	if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr")...); err != nil {
		t.Fatalf("idempotent retry refused: %v %s", err, stdout)
	}
	if readStateBytes(t, statePath) != before {
		t.Fatal("idempotent retry changed state")
	}
	// Release withdraws the placement.
	if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--release")...); err != nil {
		t.Fatalf("release refused: %v %s", err, stdout)
	}
	if stateTaskField(t, statePath, "vb-cp-0", "provider_reservations") != nil {
		t.Fatal("release left the reservation")
	}
}

func TestReserveProviderCLIRefusals_D80(t *testing.T) {
	cases := map[string]struct {
		mutate    func(*models.State)
		args      []string
		fragments []string
	}{
		"executing writer": {
			mutate: func(s *models.State) {
				task := s.FindTask("vb-cp-0")
				task.Status = models.TaskStatusCodePlanning
				task.AssignedTo = testhelpers.StringPtr("code-planner-1")
			},
			args:      reserveArgs("vb-cp-0", "corr"),
			fragments: []string{"vb-cp-0"},
		},
		"terminal writer": {
			mutate:    func(s *models.State) { s.FindTask("vb-cp-0").Status = models.TaskStatusMerged },
			args:      reserveArgs("vb-cp-0", "corr"),
			fragments: []string{"vb-cp-0"},
		},
		"self": {
			args:      reserveArgs("corr", "corr"),
			fragments: []string{"corr"},
		},
		"cycle": {
			mutate:    func(s *models.State) { s.FindTask("corr").DependsOn = []string{"vb-cp-0"} },
			args:      reserveArgs("vb-cp-0", "corr"),
			fragments: []string{"cycle"},
		},
		"wrong transition source": {
			args:      []string{"vb-cp-0", "corr", "--transition", "code-plan-to-coding", "--reason", "D65 placement", "--agent-id", "orchestrator-1"},
			fragments: []string{"code-plan-to-coding"},
		},
		"non-orchestrator agent": {
			args: []string{"vb-cp-0", "corr", "--transition", reserveProviderTransition, "--reason", "D65 placement", "--agent-id", "coder-1"},
		},
		"missing reason": {
			args: []string{"vb-cp-0", "corr", "--transition", reserveProviderTransition, "--agent-id", "orchestrator-1"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root, statePath := setupReserveProviderCLI(t, tc.mutate)
			requireReserveRefused(t, root, statePath, tc.args, tc.fragments...)
		})
	}
}

// R14/R15: placing a provider that another writer already selects by index
// needs a cap covering that selection, set in the same transaction.
func TestReserveProviderCLIPlacedWriterCap_D80(t *testing.T) {
	selector := func(s *models.State) {
		q := testhelpers.BuildTaskByStatus("q", models.TaskStatusDraftCodingPlan, time.Now().UTC())
		q.RolePair = "code-planning-pair"
		q.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "corr", Transition: reserveProviderTransition, Outputs: []int{0}}}
		s.Tasks = append(s.Tasks, q)
	}
	t.Run("uncapped placement of a selected provider is refused", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, selector)
		requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr"), "q", "max_outputs")
	})
	t.Run("cap not covering the selection is refused", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, selector)
		requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "2"), "q", "max_outputs")
	})
	t.Run("covering cap is set with the reservation; it never rises", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, selector)
		if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "1")...); err != nil {
			t.Fatalf("covering cap refused: %v %s", err, stdout)
		}
		if got := stateTaskField(t, statePath, "corr", "max_outputs"); got != 1 {
			t.Fatalf("corr max_outputs = %v, want 1", got)
		}
		requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "2"), "max_outputs")
	})
	t.Run("release of the last reservation makes the selection ordinary (control)", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, selector)
		if _, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "1")...); err != nil {
			t.Fatal(err)
		}
		if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--release")...); err != nil {
			t.Fatalf("release refused: %v %s", err, stdout)
		}
		if stateTaskField(t, statePath, "vb-cp-0", "provider_reservations") != nil {
			t.Fatal("release left the reservation")
		}
	})
}

func TestReserveProviderCLICapEdges_D80(t *testing.T) {
	output := models.OutputEntry{Desc: "Plan the shared contract", DoneWhen: "Contract reviewed", Scope: "contract", SpecRef: "README.md"}
	t.Run("selection in live draft output needs the cap (R15)", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, func(s *models.State) {
			q := testhelpers.BuildTaskByStatus("q", models.TaskStatus("DRAFT_ARCHITECTURE"), time.Now().UTC())
			q.RolePair = "architecture-pair"
			draft := output
			draft.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "corr", Transition: reserveProviderTransition, Outputs: []int{0}}}
			q.Output = []models.OutputEntry{draft}
			s.Tasks = append(s.Tasks, q)
		})
		requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr"), "q", "max_outputs")
	})
	t.Run("cap on an executing provider without output (R14)", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, func(s *models.State) {
			corr := s.FindTask("corr")
			corr.Status = models.TaskStatus("ARCHITECTING")
			corr.AssignedTo = testhelpers.StringPtr("architect-1")
		})
		if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "1")...); err != nil {
			t.Fatalf("cap on an executing provider refused: %v %s", err, stdout)
		}
		if got := stateTaskField(t, statePath, "corr", "max_outputs"); got != 1 {
			t.Fatalf("corr max_outputs = %v, want 1", got)
		}
	})
	t.Run("cap below existing output count is refused (R14)", func(t *testing.T) {
		root, statePath := setupReserveProviderCLI(t, func(s *models.State) {
			s.FindTask("corr").Output = []models.OutputEntry{output, output}
		})
		requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "1"), "max_outputs")
	})
}

func TestReserveProviderCLIDescendantSelectionNeedsCap_D80(t *testing.T) {
	root, statePath := setupReserveProviderCLI(t, func(s *models.State) {
		q := testhelpers.BuildTaskByStatus("q", models.TaskStatusDraftCodingPlan, time.Now().UTC())
		q.RolePair = "code-planning-pair"
		q.DescendantDependencies = []models.DescendantDependency{{
			AtTransition:         "code-plan-to-coding",
			ProviderDependencies: []models.ProviderDependency{{ProviderTask: "corr", Transition: reserveProviderTransition, Outputs: []int{0}}},
		}}
		s.Tasks = append(s.Tasks, q)
	})
	requireReserveRefused(t, root, statePath, reserveArgs("vb-cp-0", "corr"), "q", "max_outputs")
	if stdout, err := runReserveProvider(t, root, reserveArgs("vb-cp-0", "corr", "--provider-max-outputs", "1")...); err != nil {
		t.Fatalf("covering cap refused: %v %s", err, stdout)
	}
}
