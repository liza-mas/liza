package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/roles"
)

func TestSliceIntegrationContext(t *testing.T) {
	now := time.Now().UTC()
	baseCommit := "goal-base-111"
	worktree := ".worktrees/slice analysis; echo marker"
	mergeA := "slice-commit-aaa"
	mergeB := "slice-commit-bbb"
	state := &models.State{
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Compose bounded slices",
			SpecRef:     "specs/goal.md",
			BaseCommit:  &baseCommit,
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "slice-analysis",
				Description: "Analyze the originating slice",
				DoneWhen:    "The slice composes",
				Status:      models.TaskStatusImplementing,
				RolePair:    "slice-integration-pair",
				Worktree:    &worktree,
				Created:     now,
				IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
					Key:                   "slice:plan-alpha",
					Phase:                 models.IntegrationAnalysisPhaseSlice,
					OriginatingPlanTaskID: "plan-alpha",
					RootTaskIDs:           []string{"coding-b", "coding-a"},
					DescendantChanges: []models.IntegrationDescendantChange{
						{TaskID: "coding-b", Commit: mergeB},
						{TaskID: "coding-a", Commit: mergeA},
					},
					SourceCommit:        "slice-source-123",
					AffectedPaths:       []string{"internal/alpha/b.go", "internal/alpha/a file; echo marker.go"},
					SourceSnapshotPaths: []string{"internal/alpha/a file; echo marker.go"},
				},
			},
			{
				ID:          "plan-alpha",
				Description: "Plan alpha composition",
				DoneWhen:    "Alpha tasks share one coherent boundary",
				SpecRef:     "specs/goal.md#alpha",
				PlanRef:     "specs/plans/alpha.md",
				ArchRef:     "specs/architecture/alpha.md",
				Status:      models.TaskStatusMerged,
				Created:     now,
			},
			{
				ID:          "coding-a",
				Description: "Implement alpha producer",
				DoneWhen:    "Producer emits stable alpha values",
				SpecRef:     "specs/goal.md#alpha",
				Status:      models.TaskStatusMerged,
				MergeCommit: &mergeA,
				DependsOn:   []string{"shared-contract"},
				Decomposition: &models.DecompositionManifest{
					OwnedFiles:            []string{"internal/alpha/a.go"},
					OwnedModules:          []string{"internal/alpha"},
					ReadOnlyDependsOn:     []int{2},
					ReadOnlyTaskDependsOn: []string{"shared-read-only"},
					InterfacesOwned:       []string{"alpha.Producer"},
					InterfacesConsumed:    []string{"shared.Contract"},
					CoverageNotes:         "Producer ownership remains local to alpha.",
				},
				Created: now,
			},
			{
				ID:          "coding-b",
				Description: "Implement alpha consumer",
				DoneWhen:    "Consumer accepts stable alpha values",
				SpecRef:     "specs/goal.md#alpha",
				Status:      models.TaskStatusMerged,
				MergeCommit: &mergeB,
				DependsOn:   []string{"coding-a"},
				Decomposition: &models.DecompositionManifest{
					OwnedFiles:         []string{"internal/alpha/b.go"},
					InterfacesConsumed: []string{"alpha.Producer"},
				},
				Created: now,
			},
			{
				ID:          "unrelated-sibling",
				Description: "Distracting sibling scope",
				DoneWhen:    "Sibling work is complete",
				SpecRef:     "specs/unrelated.md",
				PlanRef:     "specs/plans/unrelated.md",
				Status:      models.TaskStatusMerged,
				MergeCommit: ptrString("unrelated-commit-999"),
				Created:     now,
			},
		},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "integration"},
	}

	analyst := renderIntegrationRoleContext(t, state, "slice-analysis", roles.IntegrationAnalyst)
	reviewer := renderIntegrationRoleContext(t, state, "slice-analysis", roles.IntegrationReviewer)

	assertContainsAll(t, analyst,
		"SLICE INTEGRATION CONTEXT",
		"SOURCE COMMIT: slice-source-123",
		"ORIGINATING PLAN: plan-alpha",
		"specs/plans/alpha.md",
		"specs/architecture/alpha.md",
		"coding-a",
		"Producer emits stable alpha values",
		"slice-commit-aaa",
		"coding-b",
		"Consumer accepts stable alpha values",
		"slice-commit-bbb",
		"internal/alpha/a.go",
		"internal/alpha/b.go",
		"alpha.Producer",
		"shared.Contract",
		"shared-read-only",
		"Read-only output dependencies: 2",
		"git -C '/project/.worktrees/slice analysis; echo marker' show 'slice-source-123:internal/alpha/a file; echo marker.go'",
	)
	assertContainsAll(t, reviewer,
		"intra-plan composition",
		"shared intent",
		"slice-source-123",
	)
	for _, output := range []string{analyst, reviewer} {
		for _, unwanted := range []string{
			"unrelated-sibling",
			"Distracting sibling scope",
			"specs/plans/unrelated.md",
			"unrelated-commit-999",
			"..HEAD",
			"show HEAD:",
			"goal-level merge readiness",
		} {
			assertNotContains(t, output, unwanted)
		}
	}

	t.Run("missing persisted descendant fails closed", func(t *testing.T) {
		broken := *state
		broken.Tasks = append([]models.Task(nil), state.Tasks...)
		broken.Tasks[0].IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
			Key:                   "slice:plan-alpha",
			Phase:                 models.IntegrationAnalysisPhaseSlice,
			OriginatingPlanTaskID: "plan-alpha",
			RootTaskIDs:           []string{"missing-task"},
			DescendantChanges: []models.IntegrationDescendantChange{
				{TaskID: "missing-task", Commit: "missing-commit"},
			},
			SourceCommit: "slice-source-123",
		}
		_, err := buildTaskRoleContextData(&broken.Tasks[0], &broken, integrationSupervisorConfig(roles.IntegrationAnalyst), embeddedPipelineResolver(t))
		if err == nil || !strings.Contains(err.Error(), "missing-task") {
			t.Fatalf("buildTaskRoleContextData error = %v, want missing descendant failure", err)
		}
	})
}

func TestGlobalIntegrationContext(t *testing.T) {
	now := time.Now().UTC()
	baseCommit := "goal-base-222"
	worktree := ".worktrees/global analysis; echo marker"
	reviewedCommit := "reviewed-single-aaa"
	mergeCommit := "merged-single-bbb"
	state := &models.State{
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Compose all scopes",
			SpecRef:     "specs/goal.md",
			BaseCommit:  &baseCommit,
			Status:      models.GoalStatusInProgress,
			Created:     now,
			Integration: &models.IntegrationLifecycle{
				ContributingSet: &models.IntegrationContributingSet{Scopes: []models.IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
					{PlanTaskID: "plan-sliced", RootTaskIDs: []string{"coding-left", "coding-right"}},
				}},
				Coverage: []models.IntegrationCoverageRecord{
					{
						PlanTaskID: "plan-sliced",
						Kind:       models.IntegrationCoverageSliceReport,
						SliceReport: &models.IntegrationSliceReport{
							AnalysisTaskID: "slice-report-task",
							AnalysisKey:    "slice:plan-sliced",
							Verdict:        models.IntegrationAnalysisVerdictClean,
							SourceCommit:   "slice-source-789",
							ReportCommit:   "slice-report-commit",
						},
					},
					{
						PlanTaskID: "plan-single",
						Kind:       models.IntegrationCoverageApprovalAttestation,
						ApprovalAttestations: []models.IntegrationApprovalAttestation{{
							ReviewedTaskID:     "coding-single",
							AcceptanceCriteria: "Single lineage remains approved",
							ReviewedCommit:     reviewedCommit,
							Approver:           "code-reviewer-1",
							Validation:         []string{"project-test --scope single"},
							MergeCommit:        mergeCommit,
						}},
					},
				},
			},
		},
		Tasks: []models.Task{
			{
				ID:          "global-analysis",
				Description: "Analyze the aggregate branch",
				DoneWhen:    "The goal is merge ready",
				Status:      models.TaskStatusImplementing,
				RolePair:    "integration-pair",
				Worktree:    &worktree,
				Created:     now,
				IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
					Key:          "global:2",
					Phase:        models.IntegrationAnalysisPhaseGlobal,
					Generation:   2,
					SourceCommit: "global-source-456",
				},
			},
			{ID: "plan-single", Description: "Single lineage plan", Status: models.TaskStatusMerged, Created: now},
			{ID: "plan-sliced", Description: "Multi-lineage plan", Status: models.TaskStatusMerged, Created: now},
			{ID: "coding-single", DoneWhen: "Single lineage remains approved", Status: models.TaskStatusMerged, MergeCommit: &mergeCommit, Created: now},
			{ID: "coding-left", Status: models.TaskStatusMerged, Created: now},
			{ID: "coding-right", Status: models.TaskStatusMerged, Created: now},
			{
				ID:       "slice-report-task",
				Status:   models.TaskStatusMerged,
				Created:  now,
				RolePair: "slice-integration-pair",
				IntegrationAnalysis: &models.IntegrationAnalysisMetadata{
					Key:                   "slice:plan-sliced",
					Phase:                 models.IntegrationAnalysisPhaseSlice,
					OriginatingPlanTaskID: "plan-sliced",
					RootTaskIDs:           []string{"coding-left", "coding-right"},
					SourceCommit:          "slice-source-789",
				},
			},
			{ID: "unrelated-merged", Description: "Distracting merged task", Status: models.TaskStatusMerged, Created: now},
		},
		Agents: map[string]models.Agent{},
		Config: models.Config{IntegrationBranch: "integration"},
	}

	analyst := renderIntegrationRoleContext(t, state, "global-analysis", roles.IntegrationAnalyst)
	reviewer := renderIntegrationRoleContext(t, state, "global-analysis", roles.IntegrationReviewer)

	assertContainsAll(t, analyst,
		"GLOBAL INTEGRATION CONTEXT",
		"GENERATION: 2",
		"SOURCE COMMIT: global-source-456",
		"COVERAGE MAP",
		"navigation evidence, not proof of aggregate correctness",
		"plan-single",
		"approval_attestation",
		"coding-single",
		"reviewed-single-aaa",
		"plan-sliced",
		"slice_report",
		"slice-report-task",
		"git -C '/project/.worktrees/global analysis; echo marker' diff --name-only 'goal-base-222..global-source-456'",
		"git -C '/project/.worktrees/global analysis; echo marker' diff --stat 'goal-base-222..global-source-456'",
		"git -C '/project/.worktrees/global analysis; echo marker' diff 'goal-base-222..global-source-456' -- <path>",
		"independent aggregate review",
	)
	assertContainsAll(t, reviewer,
		"cross-scope interactions",
		"shared interfaces",
		"aggregate tests and specifications",
		"architectural drift",
		"emergent risks",
		"omissions",
		"goal-level merge readiness",
		"global-source-456",
	)
	for _, output := range []string{analyst, reviewer} {
		for _, unwanted := range []string{
			"unrelated-merged",
			"Distracting merged task",
			"..HEAD",
			"intra-plan composition",
			"git -C /project/.worktrees/global analysis; echo marker diff --name-only goal-base-222..global-source-456",
			"git -C /project/.worktrees/global analysis; echo marker diff --stat goal-base-222..global-source-456",
			"git -C /project/.worktrees/global analysis; echo marker diff goal-base-222..global-source-456 -- <path>",
		} {
			assertNotContains(t, output, unwanted)
		}
	}

	stateWithIntegration := func(scopes []models.IntegrationScopeSnapshot, coverage []models.IntegrationCoverageRecord) *models.State {
		updated := *state
		updated.Goal = state.Goal
		updated.Goal.Integration = &models.IntegrationLifecycle{
			ContributingSet: &models.IntegrationContributingSet{Scopes: scopes},
			Coverage:        coverage,
		}
		return &updated
	}

	t.Run("fewer than two scopes render without coverage", func(t *testing.T) {
		testCases := []struct {
			name   string
			scopes []models.IntegrationScopeSnapshot
		}{
			{
				name: "one scope",
				scopes: []models.IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
				},
			},
			{name: "zero scopes"},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				reduced := stateWithIntegration(testCase.scopes, nil)
				task := reduced.FindTask("global-analysis")
				data := &prompts.RoleContextData{}
				if err := populateGlobalIntegrationContext(task, reduced, data); err != nil {
					t.Fatalf("populateGlobalIntegrationContext() error = %v", err)
				}
				if got := len(data.IntegrationCoverage); got != 0 {
					t.Fatalf("IntegrationCoverage length = %d, want 0", got)
				}

				for _, role := range []string{roles.IntegrationAnalyst, roles.IntegrationReviewer} {
					context := renderIntegrationRoleContext(t, reduced, "global-analysis", role)
					assertContainsAll(t, context,
						"GLOBAL INTEGRATION CONTEXT",
						"no local coverage records; fewer than two contributing scopes bypass local coverage",
					)
				}
			})
		}
	})

	t.Run("coverage validation remains fail closed", func(t *testing.T) {
		testCases := []struct {
			name     string
			scopes   []models.IntegrationScopeSnapshot
			coverage []models.IntegrationCoverageRecord
			want     string
		}{
			{
				name: "two scopes missing coverage",
				scopes: []models.IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
					{PlanTaskID: "plan-sliced", RootTaskIDs: []string{"coding-left", "coding-right"}},
				},
				coverage: []models.IntegrationCoverageRecord{state.Goal.Integration.Coverage[0]},
				want:     "lacks coverage for plan",
			},
			{
				name: "coverage outside contributing set",
				scopes: []models.IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
				},
				coverage: []models.IntegrationCoverageRecord{state.Goal.Integration.Coverage[0]},
				want:     "contains coverage outside the frozen contributing set",
			},
			{
				name: "invalid rendered coverage payload",
				scopes: []models.IntegrationScopeSnapshot{
					{PlanTaskID: "plan-single", RootTaskIDs: []string{"coding-single"}},
				},
				coverage: []models.IntegrationCoverageRecord{
					{PlanTaskID: "plan-single", Kind: models.IntegrationCoverageApprovalAttestation},
				},
				want: "has contradictory payload",
			},
		}

		for _, testCase := range testCases {
			t.Run(testCase.name, func(t *testing.T) {
				broken := stateWithIntegration(testCase.scopes, testCase.coverage)
				task := broken.FindTask("global-analysis")
				err := populateGlobalIntegrationContext(task, broken, &prompts.RoleContextData{})
				if err == nil || !strings.Contains(err.Error(), testCase.want) {
					t.Fatalf("populateGlobalIntegrationContext() error = %v, want containing %q", err, testCase.want)
				}
			})
		}
	})
}

func renderIntegrationRoleContext(t *testing.T, state *models.State, taskID, role string) string {
	t.Helper()
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("missing task %q", taskID)
	}
	data, err := buildTaskRoleContextData(task, state, integrationSupervisorConfig(role), embeddedPipelineResolver(t))
	if err != nil {
		t.Fatalf("buildTaskRoleContextData(%s): %v", role, err)
	}
	sections := []string{"branch-integration-context"}
	if role == roles.IntegrationReviewer {
		sections = append(sections, "review-instructions")
	}
	context, err := prompts.BuildRoleContext(role, sections, data)
	if err != nil {
		t.Fatalf("BuildRoleContext(%s): %v", role, err)
	}
	return context
}

func integrationSupervisorConfig(role string) SupervisorConfig {
	return SupervisorConfig{
		Role:        role,
		AgentID:     role + "-1",
		ProjectRoot: "/project",
		SpecsDir:    "/project/specs",
		StatePath:   "/project/.liza/state.yaml",
	}
}
