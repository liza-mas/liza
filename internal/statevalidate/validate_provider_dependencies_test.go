package statevalidate

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func providerValidationFixture(t *testing.T) (*models.State, *pipeline.Resolver, *pipeline.PipelineConfig) {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatal(err)
	}
	return &models.State{Tasks: []models.Task{
		{ID: "provider", RolePair: "architecture-pair", Status: models.TaskStatusMerged, Output: []models.OutputEntry{providerPlanOutput()}},
		{ID: "consumer", RolePair: "architecture-pair", Status: models.TaskStatus("DRAFT_ARCHITECTURE"), ProviderDependencies: []models.ProviderDependency{providerPlanDependency("provider", 0)}},
	}}, pipeline.NewResolver(cfg), cfg
}

func providerPlanOutput() models.OutputEntry {
	return models.OutputEntry{Desc: "Plan contract", DoneWhen: "Contract reviewed", Scope: "Contract", SpecRef: "specs/contract.md"}
}

func providerPlanDependency(provider string, outputs ...int) models.ProviderDependency {
	return models.ProviderDependency{ProviderTask: provider, Transition: "architecture-to-code-plan", Outputs: outputs}
}

func requireProviderCycle(t *testing.T, err error, members ...string) {
	t.Helper()
	var cycle *DependencyCycleError
	if err == nil || !errors.As(err, &cycle) {
		t.Fatalf("validation = %v, want dependency cycle", err)
	}
	for _, member := range members {
		if !slices.Contains(cycle.CyclePath, member) {
			t.Fatalf("cycle path = %v, missing participant %s", cycle.CyclePath, member)
		}
	}
}

func TestValidateProviderDependenciesAcceptsUnbornAndPartialProviders(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*models.State)
	}{
		{"merged provider before expansion", func(*models.State) {}},
		{"pending provider with unknown outputs", func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE")
			s.Tasks[0].Output = nil
			s.Tasks[1].ProviderDependencies[0].Outputs = []int{7}
		}},
		{"executed marker before child recovery", func(s *models.State) {
			s.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
		}},
		{"held handoff is valid but unready", func(s *models.State) { s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, pr, _ := providerValidationFixture(t)
			tc.mutate(state)
			if err := ValidateProviderDependencies(state, pr); err != nil {
				t.Fatalf("valid future prerequisite refused: %v", err)
			}
			if state.FindTask("consumer").IsClaimable("architect", state.Tasks, pr) {
				t.Fatal("valid but unmaterialized prerequisite advertised as ready")
			}
		})
	}
}

func TestValidateProviderDependenciesRejectsInvalidReferences(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*models.State)
		want   string
	}{
		{"missing provider", func(s *models.State) { s.Tasks[0].ID = "other" }, "non-existent provider"},
		{"empty provider", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].ProviderTask = "" }, "provider_task"},
		{"empty transition", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Transition = "" }, "transition"},
		{"unknown transition", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Transition = "missing" }, "unknown transition"},
		{"source mismatch", func(s *models.State) { s.Tasks[0].RolePair = "code-planning-pair" }, "does not originate"},
		{"non per-subtask transition", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Transition = "us-to-coding" }, "per-subtask"},
		{"empty outputs", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = nil }, "outputs"},
		{"negative output", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{-1} }, "nonnegative"},
		{"duplicate output", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{0, 0} }, "distinct"},
		{"duplicate declaration", func(s *models.State) {
			s.Tasks[1].ProviderDependencies = append(s.Tasks[1].ProviderDependencies, providerPlanDependency("provider", 0))
		}, "duplicates"},
		{"known bounds", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{1} }, "outside"},
		{"pending known bounds", func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE")
			s.Tasks[1].ProviderDependencies[0].Outputs = []int{1}
		}, "outside"},
		{"merged missing outputs", func(s *models.State) { s.Tasks[0].Output = nil }, "outside"},
		{"selected Kind", func(s *models.State) { s.Tasks[0].Output[0].Kind = "bootstrap-precommit" }, "Kind"},
		{"abandoned provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatusAbandoned }, "retired provider"},
		{"superseded provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatusSuperseded }, "retired provider"},
		{"replanned provider", func(s *models.State) { s.Tasks[0].TransitionsExecuted = map[string]bool{"replanned": true} }, "retired provider"},
		{"retired handoff", func(s *models.State) { s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced} }, "retired provider"},
		{"wrong child role", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "architecture-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}})
		}, "provenance"},
		{"wrong child parent", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"other"}})
		}, "provenance"},
		{"multiple child parents", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider", "other"}})
		}, "provenance"},
		{"superseded child", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusSuperseded, ParentTasks: []string{"provider"}})
		}, "retired provider child"},
		{"replanned child", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}, TransitionsExecuted: map[string]bool{"replanned": true}})
		}, "retired provider child"},
		{"retired child handoff", func(s *models.State) {
			s.Tasks = append(s.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}, PlanCheck: &models.PlanCheck{Verdict: models.PlanCheckReplaced}})
		}, "retired provider child"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, pr, _ := providerValidationFixture(t)
			tc.mutate(state)
			if err := ValidateProviderDependencies(state, pr); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation = %v, want refusal containing %q", err, tc.want)
			}
		})
	}
}

func TestProviderDependenciesPreserveOrdinaryDirectionInvariant(t *testing.T) {
	state, pr, cfg := providerValidationFixture(t)
	state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	state.Tasks = append(state.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}})
	if err := dependenciesErr(state, "", true, pr, cfg, nil); err != nil {
		t.Fatalf("explicit cross-stage prerequisite should be legal: %v", err)
	}
	state.Tasks[1].ProviderDependencies = nil
	state.Tasks[1].DependsOn = []string{"provider-cp-0"}
	if err := dependenciesErr(state, "", true, pr, cfg, nil); err == nil || !strings.Contains(err.Error(), "downstream dependency") {
		t.Fatalf("ordinary downstream edge accepted: %v", err)
	}
}

func TestProviderGraphProjectedAndMaterializedChildrenAgree(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outputs  []models.OutputEntry
		children []models.Task
		cycle    bool
	}{
		{"concrete cycle", []models.OutputEntry{{TaskDependsOn: []string{"consumer"}}}, []models.Task{{ID: "provider-cp-0", DependsOn: []string{"consumer"}}}, true},
		{"sibling cycle", []models.OutputEntry{{DependsOn: []string{"1"}}, {TaskDependsOn: []string{"consumer"}}}, []models.Task{{ID: "provider-cp-0", DependsOn: []string{"provider-cp-1"}}, {ID: "provider-cp-1", DependsOn: []string{"consumer"}}}, true},
		{"unselected output independent", []models.OutputEntry{{}, {TaskDependsOn: []string{"consumer"}}}, []models.Task{{ID: "provider-cp-0"}, {ID: "provider-cp-1", DependsOn: []string{"consumer"}}}, false},
	} {
		for _, materialized := range []bool{false, true} {
			name := tc.name + "/projected"
			if materialized {
				name = tc.name + "/materialized"
			}
			t.Run(name, func(t *testing.T) {
				state, pr, _ := providerValidationFixture(t)
				state.Tasks[1].RolePair = "code-planning-pair"
				state.Tasks[1].Status = models.TaskStatus("DRAFT_CODING_PLAN")
				state.Tasks[0].Output = tc.outputs
				if materialized {
					state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
					for _, expected := range tc.children {
						child := expected
						child.RolePair, child.Status, child.ParentTasks = "code-planning-pair", models.TaskStatus("DRAFT_CODING_PLAN"), []string{"provider"}
						state.Tasks = append(state.Tasks, child)
					}
				}
				err := ValidateProviderDependencies(state, pr)
				if tc.cycle {
					requireProviderCycle(t, err, "consumer", "provider-cp-0")
				} else if err != nil {
					t.Fatalf("independent selected plan refused: %v", err)
				}
			})
		}
	}
}

func TestProviderGraphPendingProductionAndLaterOutputCycles(t *testing.T) {
	state, pr, _ := providerValidationFixture(t)
	state.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE")
	state.Tasks[0].Output = nil
	if err := ValidateProviderDependencies(state, pr); err != nil {
		t.Fatalf("unknown provider outputs refused: %v", err)
	}
	state.Tasks[0].DependsOn = []string{"consumer"}
	requireProviderCycle(t, ValidateProviderDependencies(state, pr), "provider", "consumer")
	state.Tasks[0].DependsOn = nil
	state.Tasks[0].Output = []models.OutputEntry{{TaskDependsOn: []string{"consumer"}}}
	requireProviderCycle(t, ValidateProviderDependencies(state, pr), "provider-cp-0", "consumer")
}

func TestProviderGraphReciprocalDeclarations(t *testing.T) {
	state, pr, _ := providerValidationFixture(t)
	state.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE")
	state.Tasks[0].Output = nil
	state.Tasks[0].ProviderDependencies = []models.ProviderDependency{providerPlanDependency("consumer", 0)}
	requireProviderCycle(t, ValidateProviderDependencies(state, pr), "provider", "consumer")
}

func TestProviderGraphInheritanceMatchesMaterializedOrdering(t *testing.T) {
	for _, tc := range []struct {
		name         string
		inherit      *models.InheritInputs
		expectedDeps []string
		cycle        bool
	}{
		{"default barrier", nil, []string{"upstream-cp-0", "upstream-cp-1"}, true},
		{"explicit barrier", &models.InheritInputs{Mode: models.InheritModeAll}, []string{"upstream-cp-0", "upstream-cp-1"}, true},
		{"selected cycle", &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "upstream", Outputs: []int{0}}}}, []string{"upstream-cp-0"}, true},
		{"selected independent", &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "upstream", Outputs: []int{1}}}}, []string{"upstream-cp-1"}, false},
		{"no inheritance", &models.InheritInputs{Mode: models.InheritModeNone}, nil, false},
	} {
		for _, materialized := range []bool{false, true} {
			name := tc.name + "/projected"
			if materialized {
				name = tc.name + "/materialized"
			}
			t.Run(name, func(t *testing.T) {
				state, pr, _ := providerValidationFixture(t)
				state.Tasks[1].RolePair, state.Tasks[1].Status = "code-planning-pair", models.TaskStatus("DRAFT_CODING_PLAN")
				state.Tasks[0].DependsOn = []string{"upstream"}
				state.Tasks[0].Output[0].InheritInputs = tc.inherit
				state.Tasks = append(state.Tasks,
					models.Task{ID: "upstream", RolePair: "architecture-pair", Status: models.TaskStatusMerged, Output: []models.OutputEntry{providerPlanOutput(), providerPlanOutput()}, TransitionsExecuted: map[string]bool{"architecture-to-code-plan": true}},
					models.Task{ID: "upstream-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"upstream"}, DependsOn: []string{"consumer"}},
					models.Task{ID: "upstream-cp-1", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"upstream"}},
				)
				if materialized {
					state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
					state.Tasks = append(state.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"provider"}, DependsOn: tc.expectedDeps})
				}
				err := ValidateProviderDependencies(state, pr)
				if tc.cycle {
					requireProviderCycle(t, err, "consumer", "provider-cp-0", "upstream-cp-0")
				} else if err != nil {
					t.Fatalf("narrowed prerequisite introduced false cycle: %v", err)
				}
			})
		}
	}
}

func TestProviderGraphInheritanceRequiresExecutedTransition(t *testing.T) {
	for _, materialized := range []bool{false, true} {
		name := "projected"
		if materialized {
			name = "materialized"
		}
		t.Run(name, func(t *testing.T) {
			state, pr, _ := providerValidationFixture(t)
			state.Tasks[1].RolePair, state.Tasks[1].Status = "code-planning-pair", models.TaskStatus("DRAFT_CODING_PLAN")
			state.Tasks[0].DependsOn = []string{"upstream"}
			state.Tasks = append(state.Tasks, models.Task{ID: "upstream", RolePair: "architecture-pair", Status: models.TaskStatusMerged, Output: []models.OutputEntry{{TaskDependsOn: []string{"consumer"}}}})
			if materialized {
				state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
				state.Tasks = append(state.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"provider"}})
			}
			if err := ValidateProviderDependencies(state, pr); err != nil {
				t.Fatalf("unexecuted upstream handoff created false inheritance cycle: %v", err)
			}
			state.FindTask("upstream").TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
			state.Tasks = append(state.Tasks, models.Task{ID: "upstream-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"upstream"}, DependsOn: []string{"consumer"}})
			err := ValidateProviderDependencies(state, pr)
			if materialized {
				if err != nil {
					t.Fatalf("later upstream expansion retroactively changed actual child dependencies: %v", err)
				}
			} else {
				requireProviderCycle(t, err, "consumer", "provider-cp-0", "upstream-cp-0")
			}
		})
	}
}

func TestProviderGraphReplacementExcludesRetiringInheritedChildren(t *testing.T) {
	for _, selected := range []bool{false, true} {
		name := "default"
		if selected {
			name = "selected"
		}
		t.Run(name, func(t *testing.T) {
			state, pr, _ := providerValidationFixture(t)
			state.Tasks[1].RolePair, state.Tasks[1].Status = "code-planning-pair", models.TaskStatus("DRAFT_CODING_PLAN")
			state.Tasks[0].DependsOn = []string{"upstream"}
			if selected {
				state.Tasks[0].Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "upstream", Outputs: []int{0}}}}
			}
			state.Tasks = append(state.Tasks,
				models.Task{ID: "upstream", RolePair: "architecture-pair", Status: models.TaskStatusMerged, Output: []models.OutputEntry{providerPlanOutput()}, TransitionsExecuted: map[string]bool{"architecture-to-code-plan": true}},
				models.Task{ID: "upstream-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"upstream"}, DependsOn: []string{"consumer"}},
			)
			requireProviderCycle(t, ValidateProviderDependencies(state, pr), "consumer", "upstream-cp-0")
			state.Tasks[0].Output[0].Supersedes = "upstream-cp-0"
			if err := ValidateProviderDependencies(state, pr); err != nil {
				t.Fatalf("replacement waits on the inherited child it retires: %v", err)
			}
		})
	}
}

func TestProviderGraphKindRemappingMatchesMaterializedOrdering(t *testing.T) {
	for _, cycle := range []bool{false, true} {
		for _, materialized := range []bool{false, true} {
			name := "independent/projected"
			if cycle {
				name = "cycle/projected"
			}
			if materialized {
				name = strings.Replace(name, "projected", "materialized", 1)
			}
			t.Run(name, func(t *testing.T) {
				state, pr, _ := providerValidationFixture(t)
				state.Tasks[1].RolePair, state.Tasks[1].Status = "code-planning-pair", models.TaskStatus("DRAFT_CODING_PLAN")
				state.Tasks[0].Output = []models.OutputEntry{{DependsOn: []string{"1"}}, {Kind: "bootstrap-precommit"}}
				incumbent := models.Task{ID: "foreign-incumbent", Kind: "bootstrap-precommit", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN")}
				if cycle {
					incumbent.DependsOn = []string{"consumer"}
				}
				state.Tasks = append(state.Tasks, incumbent)
				if materialized {
					state.Tasks[0].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
					state.Tasks = append(state.Tasks, models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"provider"}, DependsOn: []string{"foreign-incumbent"}})
				}
				err := ValidateProviderDependencies(state, pr)
				if cycle {
					requireProviderCycle(t, err, "consumer", "provider-cp-0", "foreign-incumbent")
				} else if err != nil {
					t.Fatalf("independent Kind remap refused: %v", err)
				}
			})
		}
	}
}

func TestProviderValidationLiveOutputsAndHistoricalDeclarations(t *testing.T) {
	state, pr, _ := providerValidationFixture(t)
	state.Tasks[1].ProviderDependencies = nil
	state.Tasks[1].Output = []models.OutputEntry{{ProviderDependencies: []models.ProviderDependency{providerPlanDependency("missing", 0)}}}
	if err := ValidateProviderDependencies(state, pr); err == nil || !strings.Contains(err.Error(), "consumer output[0]") {
		t.Fatalf("live output declaration not validated: %v", err)
	}
	state.Tasks[1].Status = models.TaskStatusMerged
	state.Tasks[1].TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	state.Tasks = append(state.Tasks, models.Task{
		ID: "consumer-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusDraftCodingPlan,
		ParentTasks: []string{"consumer"},
	})
	if err := ValidateProviderDependencies(state, pr); err == nil || !strings.Contains(err.Error(), "consumer output[0]") {
		t.Fatalf("metadata-incomplete output declaration treated as historical: %v", err)
	}
	state.Tasks[1].Status = models.TaskStatusAbandoned
	state.Tasks[1].ProviderDependencies = []models.ProviderDependency{providerPlanDependency("missing", 0)}
	if err := ValidateProviderDependencies(state, pr); err != nil {
		t.Fatalf("historical terminal declarations treated as live holds: %v", err)
	}
}

func TestProviderValidationRejectsExecutingConsumerWithPendingPrerequisite(t *testing.T) {
	state, pr, _ := providerValidationFixture(t)
	status, err := pr.ExecutingStatus("architecture-pair")
	if err != nil {
		t.Fatal(err)
	}
	state.Tasks[1].Status = status
	if err := ValidateProviderDependencies(state, pr); err == nil || !strings.Contains(err.Error(), "executing task consumer") {
		t.Fatalf("executing consumer accepted pending provider prerequisite: %v", err)
	}
}

func providerCycleCandidateFixture(t *testing.T) (*models.State, *pipeline.Resolver) {
	t.Helper()
	state, pr, _ := providerValidationFixture(t)
	state.FindTask("provider").TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
	state.FindTask("consumer").RolePair = "code-planning-pair"
	state.FindTask("consumer").Status = models.TaskStatusDraftCodingPlan
	state.Tasks = append(state.Tasks,
		models.Task{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusDraftCodingPlan, ParentTasks: []string{"provider"}, DependsOn: []string{"branch"}},
		models.Task{ID: "branch", RolePair: "code-planning-pair", Status: models.TaskStatusDraftCodingPlan, DependsOn: []string{"consumer"}},
		models.Task{ID: "second", RolePair: "code-planning-pair", Status: models.TaskStatusDraftCodingPlan, DependsOn: []string{"consumer"}},
	)
	requireProviderCycle(t, ValidateProviderDependencies(state, pr), "consumer", "provider-cp-0", "branch")
	baseline := candidateFixture(state.Tasks...)
	for _, task := range state.Tasks {
		baseline.Sprint.Scope.Planned = append(baseline.Sprint.Scope.Planned, task.ID)
	}
	return baseline, pr
}

// B3: the added direct edge closes a second effective cycle through the typed
// provider edge that was already cyclic; no ordinary DependsOn cycle exists.
func TestValidateCandidateNewCycleBehindExistingProviderCycle(t *testing.T) {
	baseline, _ := providerCycleCandidateFixture(t)
	candidate := db.CloneState(baseline)
	candidate.FindTask("branch").DependsOn = append(candidate.FindTask("branch").DependsOn, "second")
	err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, io.Discard)
	requireProviderCycle(t, err, "branch", "second", "consumer", "provider-cp-0")
	if !strings.Contains(err.Error(), "provider dependency cycle branch -> second") {
		t.Fatalf("new cyclic edge was not reported: %v", err)
	}
}

func TestValidateCandidateAllowsUnrelatedChangesAndPartialProviderCycleRepairs(t *testing.T) {
	for _, tc := range []struct {
		name        string
		secondCycle bool
		mutate      func(*models.State)
	}{
		{"unchanged", false, func(*models.State) {}},
		{"unrelated priority", false, func(s *models.State) { s.FindTask("second").Priority = 2 }},
		{"acyclic edge", false, func(s *models.State) {
			s.FindTask("second").DependsOn = append(s.FindTask("second").DependsOn, "provider")
		}},
		{"remove second cycle", true, func(s *models.State) { s.FindTask("branch").DependsOn = []string{"consumer"} }},
		{"remove original cycle", true, func(s *models.State) { s.FindTask("branch").DependsOn = []string{"second"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline, pr := providerCycleCandidateFixture(t)
			if tc.secondCycle {
				baseline.FindTask("branch").DependsOn = append(baseline.FindTask("branch").DependsOn, "second")
			}
			candidate := db.CloneState(baseline)
			tc.mutate(candidate)
			requireProviderCycle(t, ValidateProviderDependencies(candidate, pr), "consumer", "provider-cp-0")
			var warnings bytes.Buffer
			if err := ValidateCandidate(candidate, baselineOf(baseline), pipelineRoot(t), true, &warnings); err != nil {
				t.Fatalf("change introducing no new cyclic edge refused: %v", err)
			}
			if !strings.Contains(warnings.String(), "provider dependency cycle") {
				t.Fatal("remaining provider cycle lost its pre-existing violation warning")
			}
		})
	}
}

func TestProviderValidationKindDedupRetainsOwnedMetadataLiveness(t *testing.T) {
	for _, tc := range []struct {
		name, childID string
		status        models.TaskStatus
		live          bool
	}{
		{"own child", "consumer-cp-0", models.TaskStatusDraftCodingPlan, true},
		{"foreign incumbent", "foreign-incumbent", models.TaskStatusDraftCodingPlan, false},
		{"terminal own child", "consumer-cp-0", models.TaskStatusMerged, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, pr, _ := providerValidationFixture(t)
			consumer := state.FindTask("consumer")
			consumer.ProviderDependencies = nil
			consumer.Status = models.TaskStatusMerged
			consumer.TransitionsExecuted = map[string]bool{"architecture-to-code-plan": true}
			consumer.Output = []models.OutputEntry{{Kind: "bootstrap-precommit", ProviderDependencies: []models.ProviderDependency{providerPlanDependency("missing", 0)}}}
			state.Tasks = append(state.Tasks, models.Task{ID: tc.childID, Kind: "bootstrap-precommit", RolePair: "code-planning-pair", Status: tc.status, ParentTasks: []string{"consumer"}})
			err := ValidateProviderDependencies(state, pr)
			if tc.live {
				if err == nil || !strings.Contains(err.Error(), "consumer output[0]") {
					t.Fatalf("owned child mismatch suppressed live output validation: %v", err)
				}
			} else if err != nil {
				t.Fatalf("consumed output treated as live metadata: %v", err)
			}
		})
	}
}
