package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func directAllocationTask(pair string) models.Task {
	task := handoffPlan("architecture", pair)
	task.Type = models.TaskTypeArchitecture
	task.Output[0].CodingAllocation = true
	task.Output[0].ArchRef = "specs/arch.md#Scope One"
	task.Output[0].PlanRef = "specs/arch.md#Unit One"
	task.Output[0].Validation = []string{"go test ./internal/example"}
	task.Output[0].Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"internal/example.go"}}
	return task
}

func TestDirectCodingGenerationFencesReviewedEvidenceAndIntegration(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	parent := state.FindTask("acceptance-parent")
	path := filepath.Join(f.root, "specs/acceptance-plan.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "## Task 1\n\n### Acceptance Contract", "## Scope One\n\n### Task 1\n\n#### Acceptance Contract", 1))
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: reviewed direct coding scope")
	commit := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")
	parent.Type, parent.RolePair = models.TaskTypeArchitecture, "architecture-pair"
	parent.ReviewCommit, parent.MergeCommit = &commit, &commit
	parent.Output[0].CodingAllocation = true
	parent.Output[0].ArchRef = "specs/acceptance-plan.md#Scope One"
	parent.Output[0].Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"boundary_test.sh"}}
	check := validateDirectCodingGeneration(f.root, state, parent)
	if check.err != nil {
		t.Fatal(check.err)
	}
	if err := check.validateState(state, parent); err != nil {
		t.Fatal(err)
	}
	state.ProofReaffirmations = append(state.ProofReaffirmations, models.ProofReaffirmation{ParentTask: parent.ID})
	if err := check.validateState(state, parent); err == nil {
		t.Fatal("proof reaffirmation race admitted unchanged output")
	}
	state.ProofReaffirmations = nil
	parent.ApprovedBy = nil
	parent.Approvals = nil
	if err := check.validateState(state, parent); err == nil {
		t.Fatal("review authority race admitted unchanged output")
	}
	testhelpers.MustGit(t, f.root, "commit", "--allow-empty", "-m", "test: move integration after allocation check")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")
	if err := verifyDirectCodingIntegration(f.root, state.Config.IntegrationBranch, map[string]directCodingGenerationCheck{parent.ID: check}); err == nil {
		t.Fatal("integration moved after allocation check without refusal")
	}
}

func TestDirectCodingAllocationSelectedHandoffAndRecovery(t *testing.T) {
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	domain := NewPlanHandoffDomain(resolver)
	for _, pair := range []string{"architecture-pair", "architecture-main-pair"} {
		t.Run(pair, func(t *testing.T) {
			task := directAllocationTask(pair)
			route := "architecture-to-coding"
			if pair == "architecture-main-pair" {
				route = "architecture-main-to-coding"
			}
			if !domain.Pending(&task) || !domain.GatesTransition(&task, route) || domain.GatesTransition(&task, "architecture-to-code-plan") {
				t.Fatal("wrong selected handoff")
			}
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task}
			tDef, err := buildTransitionDefFromPipeline(resolver, route)
			if err != nil {
				t.Fatal(err)
			}
			tDef.requiredStatus = models.TaskStatusMerged
			result := &ProceedResult{}
			if err := proceedInner(state, task.ID, route, tDef, inheritedDepSet{}, resolver, time.Now().UTC(), result); err != nil {
				t.Fatal(err)
			}
			if len(result.ChildTaskIDs) != 1 {
				t.Fatal(result.ChildTaskIDs)
			}
			child := state.FindTask(result.ChildTaskIDs[0])
			if child.EffectiveType() != models.TaskTypeCoding || child.PlanRef != task.Output[0].PlanRef || child.ArchRef != task.Output[0].ArchRef || child.Decomposition == nil {
				t.Fatal("coding allocation lost in generation")
			}
			if domain.Pending(state.FindTask(task.ID)) {
				t.Fatal("unselected planner route keeps handoff pending")
			}
			state.Tasks = state.Tasks[:1]
			result = &ProceedResult{}
			if err := proceedInner(state, task.ID, route, tDef, inheritedDepSet{}, resolver, time.Now().UTC(), result); err != nil {
				t.Fatal(err)
			}
			if state.FindTask(child.ID) == nil || len(result.ChildTaskIDs) != 1 {
				t.Fatal("selected direct route did not recover child")
			}
		})
	}
}

func TestCompletedArchitecturePendingCorrectionStillWakes(t *testing.T) {
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	domain := NewPlanHandoffDomain(pipeline.NewResolver(cfg))
	original := directAllocationTask("architecture-pair")
	original.TransitionsExecuted = map[string]bool{"architecture-to-coding": true}
	original.PlanAmendment = &models.PlanAmendment{Pending: "correction"}
	correction := models.Task{ID: "correction", RolePair: original.RolePair, AmendsPlan: original.ID, Status: models.TaskStatusMerged}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{original, correction}
	if domain.Pending(state.FindTask(original.ID)) {
		t.Fatal("completed route reopened")
	}
	if !domain.PlanningCompleteEligible(state, state.FindTask(original.ID)) {
		t.Fatal("expanded architecture correction hidden")
	}
	if class, _ := domain.Classify(state, state.FindTask(original.ID)); class != PlanHandoffAmendmentReady {
		t.Fatal(class)
	}
}

func TestSuccessorReservationSelectsActualArchitectureRoute(t *testing.T) {
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	for _, direct := range []bool{false, true} {
		provider := directAllocationTask("architecture-pair")
		provider.Output[0].CodingAllocation = direct
		writer := models.Task{ID: "writer", RolePair: "coding-pair", Status: models.TaskStatusReady}
		state := &models.State{Tasks: []models.Task{provider, writer}}
		if err := reserveSuccessorsInState(state, resolver, state.FindTask(provider.ID), []string{writer.ID}, "orchestrator-1"); err != nil {
			t.Fatal(err)
		}
		want := "architecture-to-code-plan"
		if direct {
			want = "architecture-to-coding"
		}
		reservations := state.FindTask(writer.ID).ProviderReservations
		if len(reservations) != 1 || reservations[0].Transition != want {
			t.Fatalf("direct=%v reservations=%v", direct, reservations)
		}
	}
}

// Both parents carry a real independently reviewed allocation. The legacy
// upstream's code plan is already MERGED, while its writer remains READY: a
// dependency on the plan alone would therefore release the writer too early.
func mixedArchitectureAllocationFixture(t *testing.T, upstreamRoute, downstreamRoute, inheritance string, recovery bool) (replacementFixture, string, string) {
	t.Helper()
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	parent := *state.FindTask("acceptance-parent")
	path := filepath.Join(f.root, "specs/acceptance-plan.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "## Task 1\n\n### Acceptance Contract", "## Scope One\n\n### Task 1\n\n#### Acceptance Contract", 1))
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: reviewed architecture allocation dependencies")
	commit := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", "HEAD")
	parent.Type = models.TaskTypeArchitecture
	parent.ReviewCommit, parent.MergeCommit = &commit, &commit
	parent.Output[0].ArchRef = "specs/acceptance-plan.md#Scope One"
	parent.Output[0].Decomposition = &models.DecompositionManifest{OwnedFiles: []string{"boundary_test.sh"}}
	architecture := func(id, route string) models.Task {
		task := parent
		task.ID, task.RolePair = id, "architecture-pair"
		if route == "architecture-main-to-coding" {
			task.RolePair = "architecture-main-pair"
		}
		task.Output = []models.OutputEntry{parent.Output[0]}
		task.Output[0].CodingAllocation = route != "architecture-to-code-plan"
		task.TransitionsExecuted = make(map[string]bool)
		return task
	}
	upstream := architecture("upstream", upstreamRoute)
	upstream.TransitionsExecuted[upstreamRoute] = true
	downstream := architecture("downstream", downstreamRoute)
	downstream.DependsOn = []string{upstream.ID}
	if recovery {
		downstream.TransitionsExecuted[downstreamRoute] = true
	}
	writerID := perSubtaskChildID(upstream.ID, "code", 0)
	writer := testhelpers.BuildTaskByStatus(writerID, models.TaskStatusReady, time.Now().UTC())
	writer.Type, writer.RolePair = models.TaskTypeCoding, "coding-pair"
	writer.ParentTasks = []string{upstream.ID}
	state.Tasks = []models.Task{upstream}
	if upstreamRoute == "architecture-to-code-plan" {
		planID := perSubtaskChildID(upstream.ID, "cp", 0)
		plan := testhelpers.BuildTaskByStatus(planID, models.TaskStatusMerged, time.Now().UTC())
		plan.Type, plan.RolePair = models.TaskTypePlanning, "code-planning-pair"
		plan.ParentTasks = []string{upstream.ID}
		plan.Output = []models.OutputEntry{upstream.Output[0]}
		plan.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
		writer.ID = perSubtaskChildID(planID, "code", 0)
		writer.ParentTasks = []string{planID}
		state.Tasks = append(state.Tasks, plan)
	}
	switch inheritance {
	case "selected":
		downstream.Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected,
			Selections: []models.InputSelection{{UpstreamTask: upstream.ID, Outputs: []int{0}}}}
	case "explicit":
		downstream.Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeNone}
		downstream.Output[0].TaskDependsOn = []string{writer.ID}
	}
	state.Tasks = append(state.Tasks, writer, downstream)
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted
	state.Sprint.Scope.Planned = nil
	for _, task := range state.Tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, f.statePath, state)
	slug := "code"
	if downstreamRoute == "architecture-to-code-plan" {
		slug = "cp"
	}
	return f, perSubtaskChildID(downstream.ID, slug, 0), writer.ID
}

func TestArchitectureMixedRouteInheritanceRefusesPublicGenerationAndRecovery(t *testing.T) {
	for _, routes := range [][2]string{
		{"architecture-to-code-plan", "architecture-to-coding"},
		{"architecture-to-coding", "architecture-to-code-plan"},
		{"architecture-main-to-coding", "architecture-to-coding"},
		{"architecture-to-coding", "architecture-main-to-coding"},
	} {
		for _, inheritance := range []string{"all", "selected"} {
			for _, recovery := range []bool{false, true} {
				for _, operation := range []string{"proceed", "transition-report"} {
					t.Run(strings.Join([]string{routes[0], routes[1], inheritance, operation, map[bool]string{false: "generation", true: "recovery"}[recovery]}, "/"), func(t *testing.T) {
						f, childID, _ := mixedArchitectureAllocationFixture(t, routes[0], routes[1], inheritance, recovery)
						before := replacementBytes(t, f.statePath)
						var refusal string
						if operation == "proceed" {
							_, err := Proceed(f.root, "downstream", routes[1])
							if err == nil {
								t.Fatal("mixed-route inheritance generated an unordered child")
							}
							refusal = err.Error()
							if string(replacementBytes(t, f.statePath)) != string(before) {
								t.Fatal("refused Proceed changed state")
							}
						} else {
							report, err := ExecuteTransitionsReportWith(f.root, "manual", AdmitOperator)
							if err != nil {
								t.Fatal(err)
							}
							for _, failure := range report.Failures {
								if failure.SourceTaskID == "downstream" && failure.Transition == routes[1] {
									refusal = failure.Error
								}
							}
							for _, result := range report.Results {
								if result.SourceTaskID == "downstream" {
									t.Fatal("mixed-route recovery published a result", result)
								}
							}
							state := replacementState(t, f)
							if state.FindTask("downstream").TransitionsExecuted[routes[1]] != recovery {
								t.Fatal("refusal changed the downstream transition marker")
							}
						}
						for _, want := range []string{"different architecture allocation routes", routes[0], routes[1], "task_depends_on", "writer tasks", "inherit_inputs mode none"} {
							if !strings.Contains(refusal, want) {
								t.Fatalf("refusal %q lacks %q", refusal, want)
							}
						}
						if replacementState(t, f).FindTask(childID) != nil {
							t.Fatal("refusal published an unordered child")
						}
					})
				}
			}
		}
	}
}

func TestArchitectureMixedRouteExplicitWriterOrderingPublicGenerationAndRecovery(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, operation := range []string{"proceed", "transition-report"} {
			t.Run(operation+"/"+map[bool]string{false: "generation", true: "recovery"}[recovery], func(t *testing.T) {
				f, childID, writerID := mixedArchitectureAllocationFixture(t, "architecture-to-code-plan", "architecture-to-coding", "explicit", recovery)
				if operation == "proceed" {
					if _, err := Proceed(f.root, "downstream", "architecture-to-coding"); err != nil {
						t.Fatal(err)
					}
				} else {
					report, err := ExecuteTransitionsReportWith(f.root, "manual", AdmitOperator)
					if err != nil || len(report.Failures) != 0 {
						t.Fatalf("report=%+v err=%v", report, err)
					}
				}
				state := replacementState(t, f)
				child := state.FindTask(childID)
				if child == nil || !slices.Equal(child.DependsOn, []string{writerID}) {
					t.Fatalf("child=%+v; want actual writer dependency %q", child, writerID)
				}
				if state.FindTask(writerID).Status != models.TaskStatusReady {
					t.Fatal("fixture prerequisite writer is already complete")
				}
			})
		}
	}
}

func TestArchitectureSameDirectRouteInheritancePublicGenerationAndRecovery(t *testing.T) {
	for _, inheritance := range []string{"all", "selected"} {
		for _, recovery := range []bool{false, true} {
			for _, operation := range []string{"proceed", "transition-report"} {
				t.Run(inheritance+"/"+operation+"/"+map[bool]string{false: "generation", true: "recovery"}[recovery], func(t *testing.T) {
					f, childID, writerID := mixedArchitectureAllocationFixture(t, "architecture-to-coding", "architecture-to-coding", inheritance, recovery)
					if operation == "proceed" {
						if _, err := Proceed(f.root, "downstream", "architecture-to-coding"); err != nil {
							t.Fatal(err)
						}
					} else {
						report, err := ExecuteTransitionsReportWith(f.root, "manual", AdmitOperator)
						if err != nil || len(report.Failures) != 0 {
							t.Fatalf("report=%+v err=%v", report, err)
						}
					}
					child := replacementState(t, f).FindTask(childID)
					if child == nil || !slices.Equal(child.DependsOn, []string{writerID}) {
						t.Fatalf("child=%+v; want inherited writer dependency %q", child, writerID)
					}
				})
			}
		}
	}
}
