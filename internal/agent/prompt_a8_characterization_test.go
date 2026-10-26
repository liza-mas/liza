package agent

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// A8 characterization: these tests drive entry points that exist before and
// after the orchestrator input resolution moves out of prompts, so they pin
// the behaviour the move must keep.

func a8OrchestratorFixture(t *testing.T) (string, SupervisorConfig, *pipeline.Resolver) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatalf("load frozen pipeline: %v", err)
	}
	return root, SupervisorConfig{
		Role:        "orchestrator",
		AgentID:     "orchestrator-1",
		ProjectRoot: root,
		StatePath:   statePath,
		SpecsDir:    filepath.Join(root, "specs"),
	}, pipeline.NewResolver(cfg)
}

func a8WakeLine(t *testing.T, dashboard string) string {
	t.Helper()
	for _, line := range strings.Split(dashboard, "\n") {
		if trigger, ok := strings.CutPrefix(line, "WAKE TRIGGER: "); ok {
			return trigger
		}
	}
	t.Fatalf("dashboard has no WAKE TRIGGER line:\n%s", dashboard)
	return ""
}

func a8AssessedHeld(state *models.State, id string, now time.Time) models.Task {
	held := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, now.Add(-time.Hour))
	held.History = append(held.History, models.TaskHistoryEntry{
		Time:  now.Add(-time.Minute),
		Event: models.TaskEventOrchestratorAssessment,
		Extra: map[string]any{
			ops.AssessmentFingerprintExtraKey: ops.BuildAssessmentFingerprint(state, &held, ops.AssessmentFingerprintCandidate{
				Reason: *held.BlockedReason, Questions: held.BlockedQuestions,
			}),
		},
	})
	return held
}

// A selected wake given the standalone trigger and projection must render the
// very bytes the standalone path renders for the same state.
func TestA8SelectedWakeMatchesStandaloneBytes(t *testing.T) {
	now := time.Now().UTC()
	manyToOne := func() *models.State {
		state := testhelpers.CreateValidState()
		parent := "epic-1"
		for _, id := range []string{"us-1", "us-2", "us-3"} {
			task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
			task.RolePair = "us-writing-pair"
			task.ParentTask = &parent
			state.Tasks = append(state.Tasks, task)
			state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, id)
		}
		return state
	}
	tests := []struct {
		name  string
		want  string
		state func() *models.State
	}{
		{"initial planning", "INITIAL_PLANNING", func() *models.State {
			state := testhelpers.CreateValidState()
			state.Tasks = nil
			return state
		}},
		{"blocked tasks", "BLOCKED_TASKS", func() *models.State {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
				testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
			}
			return state
		}},
		{"hypothesis exhausted", "HYPOTHESIS_EXHAUSTED", func() *models.State {
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
			task.FailedBy = []string{"coder-1", "coder-2"}
			state.Tasks = []models.Task{task}
			return state
		}},
		{"immediate discovery", "IMMEDIATE_DISCOVERY", func() *models.State {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)}
			state.Discovered = []models.Discovery{{
				ID: "disc-1", By: "coder-1", During: "task-1", Description: "found a gap",
				Severity: models.DiscoverySeverity("high"), Urgency: models.DiscoveryUrgencyImmediate, Created: now,
			}}
			return state
		}},
		{"human note", "HUMAN_NOTE", func() *models.State {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)}
			state.HumanNotes = []models.HumanNote{{Timestamp: now, Message: "please look at task-1", For: "all"}}
			return state
		}},
		{"planning complete", "PLANNING_COMPLETE", func() *models.State {
			return handoffState(handoffPlanTask("a"), handoffPlanTask("b", "a"))
		}},
		{"many to one ready", "MANY_TO_ONE_READY", manyToOne},
		{"integration unavailable", "INTEGRATION_UNAVAILABLE", func() *models.State {
			state := testhelpers.CreateValidState()
			state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
			state.Tasks = []models.Task{
				testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
				testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
			}
			return state
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, config, resolver := a8OrchestratorFixture(t)
			state := tt.state()

			standalone, err := buildOrchestratorRoleContextDataForWake(state, config, resolver, nil)
			if err != nil {
				t.Fatalf("standalone: %v", err)
			}
			trigger := a8WakeLine(t, standalone.DashboardOutput)
			if trigger != tt.want {
				t.Fatalf("standalone trigger = %s, want %s", trigger, tt.want)
			}

			var projection prompts.EffectiveIntegrationCompletion
			if strings.HasPrefix(trigger, "INTEGRATION_") {
				decision, evaluationErr := ops.EvaluateLiveIntegrationProgress(state, root)
				projection = prompts.ProjectEffectiveIntegrationCompletion(decision, nil, evaluationErr)
			}
			selected, err := buildOrchestratorRoleContextDataForWake(state, config, resolver, &OrchestratorWakeResult{
				Trigger: OrchestratorWakeTrigger(trigger), Integration: projection,
			})
			if err != nil {
				t.Fatalf("selected: %v", err)
			}
			if selected.DashboardOutput != standalone.DashboardOutput {
				t.Errorf("selected dashboard differs from standalone:\n--- standalone\n%s\n--- selected\n%s", standalone.DashboardOutput, selected.DashboardOutput)
			}
			if selected.WakeInstruction != standalone.WakeInstruction {
				t.Errorf("selected wake instruction differs from standalone:\n--- standalone\n%s\n--- selected\n%s", standalone.WakeInstruction, selected.WakeInstruction)
			}
		})
	}
}

// The dashboard shows raw counts while the BLOCKED_TASKS scope lists only the
// actionable tasks; a computed empty scope is not the stateless nil scope.
func TestA8DisplayedCountsStayRaw(t *testing.T) {
	now := time.Now().UTC()
	_, config, resolver := a8OrchestratorFixture(t)

	state := testhelpers.CreateValidState()
	changed := testhelpers.BuildTaskByStatus("changed-blocked", models.TaskStatusBlocked, now.Add(-time.Hour))
	held := a8AssessedHeld(state, "held-blocked", now)
	held.FailedBy = []string{"coder-1", "coder-2"}
	state.Tasks = []models.Task{changed, held}

	for _, wake := range []*OrchestratorWakeResult{nil, {Trigger: WakeTriggerBlocked}} {
		data, err := buildOrchestratorRoleContextDataForWake(state, config, resolver, wake)
		if err != nil {
			t.Fatalf("wake %v: %v", wake, err)
		}
		for _, want := range []string{"WAKE TRIGGER: BLOCKED_TASKS\n", "- Blocked: 2\n", "- Hypothesis exhausted: 1\n"} {
			if !strings.Contains(data.DashboardOutput, want) {
				t.Errorf("wake %v: dashboard missing raw count line %q:\n%s", wake, want, data.DashboardOutput)
			}
		}
		if !strings.Contains(data.WakeInstruction, "changed-blocked") || strings.Contains(data.WakeInstruction, "held-blocked") {
			t.Errorf("wake %v: scope must list only the actionable task:\n%s", wake, data.WakeInstruction)
		}
	}

	onlyHeld := testhelpers.CreateValidState()
	onlyHeld.Tasks = []models.Task{a8AssessedHeld(onlyHeld, "held-blocked", now)}
	computedEmpty, err := buildOrchestratorRoleContextDataForWake(onlyHeld, config, resolver, &OrchestratorWakeResult{Trigger: WakeTriggerBlocked})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(computedEmpty.WakeInstruction, "Record no assessment") || strings.Contains(computedEmpty.WakeInstruction, "held-blocked") {
		t.Errorf("computed empty scope must mean no assessment:\n%s", computedEmpty.WakeInstruction)
	}
	stateless, err := prompts.RenderWakeInstructions("BLOCKED_TASKS", "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stateless, "Record no assessment") || stateless == "INSTRUCTIONS:\n"+computedEmpty.WakeInstruction {
		t.Errorf("stateless nil scope must differ from the computed empty scope")
	}
}

// The matcher text reaches every render that names the test-file patterns.
func TestA8TestFilePatternsReachEveryRender(t *testing.T) {
	patterns := strings.Join(ops.TestFileMatcherPatterns(), ", ")
	if patterns == "" {
		t.Fatal("matcher patterns are empty")
	}
	resolver := embeddedPipelineResolver(t)
	now := time.Now().UTC()
	taskContext := func(role, rolePair string, reviewing bool) string {
		t.Helper()
		var status models.TaskStatus
		var err error
		if reviewing {
			status, err = resolver.ReviewingStatus(rolePair)
		} else {
			status, err = resolver.ExecutingStatus(rolePair)
		}
		if err != nil {
			t.Fatal(err)
		}
		task := testhelpers.BuildTaskByStatus("task-1", status, now)
		task.RolePair = rolePair
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{task}
		projectRoot := t.TempDir()
		config := SupervisorConfig{
			Role: role, AgentID: role + "-1", CLIName: "claude", ProjectRoot: projectRoot,
			SpecsDir: filepath.Join(projectRoot, "specs"), StatePath: filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml"),
		}
		data, err := testBuildTaskRoleContextData(t, &state.Tasks[0], state, config, resolver)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		sections, err := resolver.ContextSections(role)
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := prompts.BuildRoleContext(role, sections, data)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		return rendered
	}
	for _, tt := range []struct {
		role, rolePair string
		reviewing      bool
		want           string
	}{
		{"coder", "coding-pair", false, "contains no recognized test file (" + patterns + ")"},
		{"code-planner", "code-planning-pair", false, "must match a submission test-file pattern (" + patterns + ")"},
		{"code-plan-reviewer", "code-planning-pair", true, "none of the submission test-file patterns (" + patterns + ";"},
	} {
		t.Run(tt.role, func(t *testing.T) {
			if rendered := taskContext(tt.role, tt.rolePair, tt.reviewing); !strings.Contains(rendered, tt.want) {
				t.Errorf("%s context lacks %q", tt.role, tt.want)
			}
		})
	}

	t.Run("planning complete wake", func(t *testing.T) {
		_, config, wakeResolver := a8OrchestratorFixture(t)
		state := handoffState(handoffPlanTask("a"))
		data, err := buildOrchestratorRoleContextDataForWake(state, config, wakeResolver, nil)
		if err != nil {
			t.Fatal(err)
		}
		if a8WakeLine(t, data.DashboardOutput) != "PLANNING_COMPLETE" || !strings.Contains(data.WakeInstruction, "test-file pattern ("+patterns+")") {
			t.Errorf("PLANNING_COMPLETE wake lacks the matcher text:\n%s", data.WakeInstruction)
		}
	})

	// The stateless render has no merged plan, so the plans-to-review block that
	// names the patterns is unreachable: its bytes must carry no matcher text,
	// before and after the text becomes an input.
	t.Run("stateless planning complete wake", func(t *testing.T) {
		rendered, err := prompts.RenderWakeInstructions("PLANNING_COMPLETE", "orchestrator-1")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered, "test-file pattern") || strings.Contains(rendered, patterns) {
			t.Errorf("stateless PLANNING_COMPLETE render reached the matcher block:\n%s", rendered)
		}
	})
}

// An unresolvable entry-point set fails both wake modes with a nil record and
// the wrapped template-data error, never a partial prompt.
func TestA8DashboardEntryPointErrorWrapped(t *testing.T) {
	_, _, resolver := a8OrchestratorFixture(t)
	empty := t.TempDir()
	config := SupervisorConfig{
		Role: "orchestrator", AgentID: "orchestrator-1", ProjectRoot: empty,
		StatePath: filepath.Join(empty, paths.ProjectDirName(), "state.yaml"), SpecsDir: filepath.Join(empty, "specs"),
	}
	state := testhelpers.CreateValidState()
	state.Tasks = nil
	for name, wake := range map[string]*OrchestratorWakeResult{
		"standalone": nil,
		"selected":   {Trigger: WakeTriggerInitialPlanning},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := buildOrchestratorRoleContextDataForWake(state, config, resolver, wake)
			if data != nil {
				t.Errorf("record = %+v, want nil", data)
			}
			if err == nil || !strings.HasPrefix(err.Error(), "building wake template data:") {
				t.Errorf("error = %v, want prefix %q", err, "building wake template data:")
			}
		})
	}
}
