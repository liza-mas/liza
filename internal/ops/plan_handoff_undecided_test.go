package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-49 gap: an undecided observation suppresses only the unchanged plan.

func undecidedState() *models.State {
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	state.Tasks = []models.Task{handoffPlan("p", "code-planning-pair"), handoffPlan("other", "code-planning-pair")}
	state.Tasks[1].TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	state.Sprint.Scope.Planned = []string{"p", "other"}
	return state
}

func TestUndecidedHandoffReadmission(t *testing.T) {
	t.Parallel()
	root, _ := setupReplanTest(t)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().UTC().Add(time.Minute)
	note := func(target string) func(*models.State) {
		return func(s *models.State) {
			s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: target, Message: "decide", Timestamp: later})
		}
	}

	cases := []struct {
		name       string
		mutate     func(*models.State)
		suppressed bool
	}{
		{"unchanged", func(*models.State) {}, true},
		{"unrelated task merges", func(s *models.State) {
			s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("unrelated", models.TaskStatusMerged, later))
			s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, "unrelated")
		}, true},
		{"note to another task", note("other"), true},
		{"note to the plan", note("p"), false},
		{"note to all", note("all"), false},
		{"plan passed", func(s *models.State) {
			s.FindTask("p").PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: "orchestrator-1", At: later}
		}, false},
		{"other plan history", func(s *models.State) {
			plan := s.FindTask("p")
			plan.History = append(plan.History, models.TaskHistoryEntry{Time: later, Event: models.TaskEventOrchestratorAssessment})
		}, false},
		{"output changed", func(s *models.State) { s.FindTask("p").Output[0].Desc = "revised" }, false},
		{"dependency added", func(s *models.State) { s.FindTask("p").DependsOn = []string{"other"} }, false},
		{"malformed observation", func(s *models.State) {
			plan := s.FindTask("p")
			plan.History[len(plan.History)-1].Extra["fingerprint"] = "not-a-digest"
		}, false},
		{"unknown version", func(s *models.State) {
			plan := s.FindTask("p")
			plan.History[len(plan.History)-1].Extra["version"] = 99
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a plan a turn left undecided
			state := undecidedState()
			if domain.RecordUndecided(undecidedState(), state, "p", time.Now().UTC()) == nil {
				t.Fatal("precondition: observation not recorded")
			}

			// WHEN its state changes (or not)
			tc.mutate(state)

			// THEN only an unchanged plan stays suppressed, and outstanding
			plan := state.FindTask("p")
			if got := domain.UndecidedHandoff(state, plan) != nil; got != tc.suppressed {
				t.Fatalf("suppressed = %v, want %v", got, tc.suppressed)
			}
			if eligible := domain.PlanningCompleteEligible(state, plan); eligible == tc.suppressed {
				t.Fatalf("PlanningCompleteEligible = %v with suppressed = %v", eligible, tc.suppressed)
			}
			if !domain.Pending(plan) {
				t.Fatal("an undecided plan must stay outstanding")
			}
		})
	}
}

func TestRecordUndecidedSkipsInputsTheTurnDidNotSee(t *testing.T) {
	t.Parallel()
	root, _ := setupReplanTest(t)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}

	// GIVEN a note to the plan that arrived during the turn
	current := undecidedState()
	current.HumanNotes = append(current.HumanNotes, models.HumanNote{For: "p", Message: "replan", Timestamp: time.Now().UTC()})

	// WHEN the turn's undecided verdict is recorded
	observation := domain.RecordUndecided(undecidedState(), current, "p", time.Now().UTC())

	// THEN nothing is recorded and the plan keeps its wake
	if observation != nil || !domain.PlanningCompleteEligible(current, current.FindTask("p")) {
		t.Fatalf("recorded %+v over an input the turn never saw", observation)
	}

	// AND an unchanged plan is recorded once, never duplicated
	state := undecidedState()
	if domain.RecordUndecided(undecidedState(), state, "p", time.Now().UTC()) == nil {
		t.Fatal("unchanged plan not recorded")
	}
	if domain.RecordUndecided(undecidedState(), state, "p", time.Now().UTC()) != nil {
		t.Fatal("matching observation duplicated")
	}
}

func TestUndecidedPlanIsACompletionBarrier(t *testing.T) {
	t.Parallel()
	root, _ := setupReplanTest(t)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}

	// GIVEN a sprint whose every planned task is merged
	state := undecidedState()
	if terminal, err := allPlannedTasksTerminalForProject(state, root); err != nil || !terminal {
		t.Fatalf("precondition: terminal = %v, %v", terminal, err)
	}

	// WHEN a turn leaves the unexpanded plan undecided
	domain.RecordUndecided(undecidedState(), state, "p", time.Now().UTC())

	// THEN the sprint cannot be completed past it
	if !domain.HasStalledHandoff(state) {
		t.Fatal("undecided plan is not a stalled hand-off")
	}
	if terminal, err := allPlannedTasksTerminalForProject(state, root); err != nil || terminal {
		t.Fatalf("terminal = %v, %v: completion would skip the plan's children", terminal, err)
	}
}
