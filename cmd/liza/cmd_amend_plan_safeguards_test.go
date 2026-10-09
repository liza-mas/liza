package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func assertAmendmentRefusalAtomic(t *testing.T, root, statePath string, args ...string) string {
	t.Helper()
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, commandErr := executeRootCommandCapture(t, root, args...)
	err = commandErr
	if err == nil {
		t.Fatalf("%v unexpectedly succeeded", args)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(before) != string(after) {
		t.Fatalf("refused %v mutated state: %v", args, err)
	}
	return stdout
}

func TestAmendPlanCLIBeginRefusalsAreAtomic(t *testing.T) {
	cases := []struct {
		name   string
		change func(*models.State)
	}{
		{"human hold", func(s *models.State) {
			s.FindTask("original").PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld, By: "operator", At: time.Now().UTC(), Ask: "human must provision input"}
		}},
		{"already generated", func(s *models.State) {
			s.FindTask("original").TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
		}},
		{"child exists without marker", func(s *models.State) { s.FindTask("started-consumer").ParentTask = testhelpers.StringPtr("original") }},
		{"non planning", func(s *models.State) { s.FindTask("original").RolePair = "coding-pair" }},
		{"not merged", func(s *models.State) { s.FindTask("original").Status = models.TaskStatusAbandoned }},
		{"empty output", func(s *models.State) { s.FindTask("original").Output = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath := setupAmendmentCLI(t)
			if err := db.For(statePath).Modify(func(s *models.State) error { tc.change(s); return nil }); err != nil {
				t.Fatal(err)
			}
			assertAmendmentRefusalAtomic(t, root, statePath, "amend-plan", "original", "--reason", "fix scheduling", "--agent-id", "orchestrator-1", "--json")
		})
	}
}

func TestAmendPlanCLIOnlyReviewedArtifactsCanApply(t *testing.T) {
	for _, name := range []string{"unmerged", "self approved", "forged history", "artifact drift"} {
		t.Run(name, func(t *testing.T) {
			root, statePath := setupAmendmentCLI(t)
			id := beginAmendmentCLI(t, root, statePath)
			if name != "unmerged" {
				output := mustFindTask(t, readState(t, statePath), "original").Output
				mergeAmendmentCLI(t, root, statePath, id, output)
				if name == "artifact drift" {
					testhelpers.MustGit(t, root, "checkout", "integration")
					path := filepath.Join(root, "specs/acceptance-plan.md")
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					writeAmendmentFile(t, root, "specs/acceptance-plan.md", strings.Replace(string(data), "timeout_seconds\":10", "timeout_seconds\":11", 1))
					testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
					testhelpers.MustGit(t, root, "commit", "-m", "test: drift reviewed allocation")
				} else if err := db.For(statePath).Modify(func(s *models.State) error {
					task := s.FindTask(id)
					if name == "self approved" {
						task.ApprovedBy = task.AssignedTo
						task.Approvals = []models.Approval{{Agent: *task.AssignedTo, Timestamp: time.Now().UTC()}}
					} else {
						task.History = nil
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			stdout := assertAmendmentRefusalAtomic(t, root, statePath, "amend-plan", "original", "--apply", id, "--agent-id", "orchestrator-1", "--json")
			want := map[string]string{"unmerged": "requires the independent MERGED", "self approved": "independent approval", "forged history": "review authority", "artifact drift": "artifact drift"}[name]
			if !strings.Contains(stdout, want) {
				t.Fatalf("wrong refusal: %s; want %s", stdout, want)
			}
		})
	}
}

func TestAmendPlanCLICanceledPendingCorrectionCanRecover(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	id := beginAmendmentCLI(t, root, statePath)
	assertAmendmentRefusalAtomic(t, root, statePath, "amend-plan", "original", "--replace-pending", id, "--reason", "premature", "--agent-id", "orchestrator-1", "--json")
	runAmendmentCLI(t, root, "cancel-task", id, "correction needs replacement", "--json")
	runAmendmentCLI(t, root, "amend-plan", "original", "--replace-pending", id, "--reason", "fresh bounded correction", "--agent-id", "orchestrator-1", "--json")
	after := readState(t, statePath)
	if after.FindTask(id).Status != models.TaskStatusAbandoned || after.FindTask("original").PlanAmendment.Pending == id || len(after.FindTask("original").PlanAmendment.Quarantined) != 1 {
		t.Fatal("canceled correction recovery did not retain lineage and fence")
	}
}

func TestAmendPlanCLIRepeatedCorrectionsAppendProducerWithoutRemapping(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	originalReview := *readState(t, statePath).FindTask("original").ReviewCommit
	first := beginAmendmentCLI(t, root, statePath)
	output := append([]models.OutputEntry(nil), readState(t, statePath).FindTask("original").Output...)
	output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeNone}
	mergeAmendmentCLI(t, root, statePath, first, output)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", first, "--agent-id", "orchestrator-1", "--json")
	second := beginAmendmentCLI(t, root, statePath)
	output[0].DependsOn = []string{"1"}
	output = append(output, models.OutputEntry{Desc: "supply prerequisite", DoneWhen: "input ready", Scope: "prerequisite", SpecRef: "specs/acceptance-goal.md", PlanRef: "specs/acceptance-goal.md#Identity"})
	mergeAmendmentCLI(t, root, statePath, second, output)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", second, "--agent-id", "orchestrator-1", "--json")
	if stdout := assertAmendmentRefusalAtomic(t, root, statePath, "amend-plan", "original", "--apply", first, "--agent-id", "orchestrator-1", "--json"); !strings.Contains(stdout, "latest applied") {
		t.Fatal(stdout)
	}
	runAmendmentCLI(t, root, "plan-check", "original", "--pass", "--agent-id", "orchestrator-1", "--json")
	if _, err := ops.ExecuteAvailableTransitions(root, "manual"); err != nil {
		t.Fatal(err)
	}
	after := readState(t, statePath)
	resolver, err := loadResolverForRBAC(root)
	if err != nil {
		t.Fatal(err)
	}
	_, ids, err := models.ProviderDependencyChildren(models.ProviderDependency{ProviderTask: "original", Transition: "code-plan-to-coding", Outputs: []int{0, 1}}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	child := after.FindTask(ids[0])
	producer := after.FindTask(ids[1])
	if child == nil || producer == nil || !slices.Contains(child.DependsOn, producer.ID) || *after.FindTask("original").ReviewCommit != originalReview || len(after.FindTask("original").PlanAmendment.Applied) != 2 {
		t.Fatalf("amended original did not preserve allocation identities and append ordered producer: child=%+v producer=%+v original=%+v", child, producer, after.FindTask("original"))
	}
	before, _ := os.ReadFile(statePath)
	runAmendmentCLI(t, root, "amend-plan", "original", "--apply", second, "--agent-id", "orchestrator-1", "--json")
	replay, _ := os.ReadFile(statePath)
	if string(before) != string(replay) {
		t.Fatal("apply replay after generation changed state")
	}
}

func TestAmendPlanCLIRejectsRedefiningPublishedSlot(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	id := beginAmendmentCLI(t, root, statePath)
	runAmendmentCLI(t, root, "claim-task", id, "code-planner-1", "--json")
	output := append([]models.OutputEntry(nil), readState(t, statePath).FindTask("original").Output...)
	output[0].Scope = "unreviewed wider scope"
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(manifest, data, 0644); err != nil {
		t.Fatal(err)
	}
	stdout := assertAmendmentRefusalAtomic(t, root, statePath, "set-task-output", id, "--output", manifest, "--agent-id", "code-planner-1", "--json")
	if !strings.Contains(stdout, "existing slot identity") {
		t.Fatal(stdout)
	}
}

func TestAmendPlanCLIRejectsCycleInOriginalProviderNamespace(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	id := beginAmendmentCLI(t, root, statePath)
	runAmendmentCLI(t, root, "claim-task", id, "code-planner-1", "--json")
	output := append([]models.OutputEntry(nil), readState(t, statePath).FindTask("original").Output...)
	output[0].ProviderDependencies = []models.ProviderDependency{{ProviderTask: "original", Transition: "code-plan-to-coding", Outputs: []int{0}}}
	data, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(t.TempDir(), "output.json")
	if err := os.WriteFile(manifest, data, 0644); err != nil {
		t.Fatal(err)
	}
	stdout := assertAmendmentRefusalAtomic(t, root, statePath, "set-task-output", id, "--output", manifest, "--agent-id", "code-planner-1", "--json")
	if !strings.Contains(stdout, "provider dependency cycle") || strings.Contains(stdout, id+"-code") {
		t.Fatalf("cycle was not checked under original provider identity: %s", stdout)
	}
}

func TestAmendPlanCLIReasonCannotConsumeRegisteredFlag(t *testing.T) {
	root, statePath := setupAmendmentCLI(t)
	stdout := assertAmendmentRefusalAtomic(t, root, statePath, "amend-plan", "original", "--reason", "--apply", "--agent-id", "orchestrator-1", "--json")
	if !strings.Contains(stdout, "empty shell expansion") {
		t.Fatalf("reason policy was bypassed: %s", stdout)
	}
}

func TestAmendPlanCLIReviewedCorrectionKeepsManyToOneProgression(t *testing.T) {
	for _, recovery := range []string{"adopt", "merged replacement", "canceled replacement"} {
		t.Run(recovery, func(t *testing.T) {
			root, statePath := setupAmendmentCLI(t)
			configPath := filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml")
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var config pipeline.PipelineConfig
			if err := yaml.Unmarshal(data, &config); err != nil {
				t.Fatal(err)
			}
			for name, sub := range config.Pipeline.SubPipelines {
				for i := range sub.Transitions {
					if sub.Transitions[i].Name == "code-plan-to-coding" {
						sub.Transitions[i].Cardinality = "many-to-one"
					}
				}
				config.Pipeline.SubPipelines[name] = sub
			}
			data, err = yaml.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(configPath, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := db.For(statePath).Modify(func(s *models.State) error {
				original := s.FindTask("original")
				parent := *original
				parent.ID = "cohort"
				parent.RolePair = "architecture-pair"
				parent.Type = models.TaskTypeArchitecture
				parent.Output = nil
				parent.TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
				parent.ParentTasks = nil
				parent.ParentTask = nil
				original.ParentTasks = []string{parent.ID}
				s.FindTask("started-consumer").ProviderDependencies = nil
				s.Tasks = append(s.Tasks, parent)
				s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, parent.ID)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			id := beginAmendmentCLI(t, root, statePath)
			output := readState(t, statePath).FindTask("original").Output
			if recovery == "canceled replacement" {
				runAmendmentCLI(t, root, "cancel-task", id, "obtain fresh review", "--json")
			} else {
				mergeAmendmentCLI(t, root, statePath, id, output)
			}
			if recovery != "adopt" {
				old := id
				runAmendmentCLI(t, root, "amend-plan", "original", "--replace-pending", old, "--reason", "reconcile predecessor", "--agent-id", "orchestrator-1", "--json")
				id = readState(t, statePath).FindTask("original").PlanAmendment.Pending
				mergeAmendmentCLI(t, root, statePath, id, output)
			}
			detection, err := ops.LoadDetectionContext(root)
			if err != nil {
				t.Fatal(err)
			}
			if ops.CountReadyManyToOneCohorts(readState(t, statePath), detection.ManyToOneTransitions) != 0 {
				t.Fatal("pending amendment admitted fan-in")
			}
			runAmendmentCLI(t, root, "amend-plan", "original", "--apply", id, "--agent-id", "orchestrator-1", "--json")
			if ops.CountReadyManyToOneCohorts(readState(t, statePath), detection.ManyToOneTransitions) != 1 {
				t.Fatal("adoption left cohort blocked")
			}
			results, err := ops.ExecuteAvailableTransitions(root, "manual")
			if err != nil || len(results) != 1 || len(results[0].ChildTaskIDs) != 1 {
				t.Fatalf("many-to-one generation: %+v %v", results, err)
			}
			child := readState(t, statePath).FindTask(results[0].ChildTaskIDs[0])
			if !slices.Equal(child.EffectiveParentTasks(), []string{"original"}) {
				t.Fatal("correction became cohort producer", child.EffectiveParentTasks())
			}
		})
	}
}
