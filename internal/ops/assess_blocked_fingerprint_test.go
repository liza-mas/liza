package ops

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func assessmentFingerprintFixture() (*models.State, AssessmentFingerprintCandidate) {
	created := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	state := &models.State{Tasks: []models.Task{
		{ID: "blocked", Status: models.TaskStatusBlocked, Created: created, DependsOn: []string{"old", "provider"}},
		{ID: "old", Status: models.TaskStatusSuperseded, Created: created, SupersededBy: []string{"provider"}},
		{ID: "provider", Status: models.TaskStatusReady, Created: created},
		{ID: "child-z", Status: models.TaskStatusReady, Created: created, ParentTasks: []string{"provider"}},
		{ID: "child-a", Status: models.TaskStatusReady, Created: created, ParentTasks: []string{"provider"}},
	}}
	candidate := AssessmentFingerprintCandidate{
		Reason: "café needs repair", Questions: []string{"which provider?", "when ready?"}, Note: "wait for repair",
		RepairRequest: &models.RepairRequest{
			Operation: "apply-dependency-repair", Target: "blocked", Command: "repair blocked",
			Evidence: []string{"provider pending"}, Validation: []string{"check graph"},
			DependencyUpdates: []models.DependencyUpdate{{TaskID: "blocked", ExpectedDependsOn: []string{"old"}, DesiredDependsOn: []string{"provider"}}},
		},
	}
	return state, candidate
}

func TestAssessmentFingerprint(t *testing.T) {
	t.Run("identical inputs and purity", func(t *testing.T) {
		state, candidate := assessmentFingerprintFixture()
		before, err := yaml.Marshal([]any{state, candidate})
		if err != nil {
			t.Fatal(err)
		}
		first := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
		if value, ok := IsAssessmentFingerprint(first); !ok || value != first {
			t.Fatalf("invalid digest %q", first)
		}
		for range 10 {
			if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != first {
				t.Fatalf("digest changed: %s != %s", got, first)
			}
		}
		after, err := yaml.Marshal([]any{state, candidate})
		if err != nil {
			t.Fatal(err)
		}
		if string(before) != string(after) {
			t.Fatal("fingerprinting mutated inputs")
		}
	})

	t.Run("structural normalization", func(t *testing.T) {
		state, candidate := assessmentFingerprintFixture()
		want := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
		variant := AssessmentFingerprintCandidate{
			Reason: " \t cafe\u0301\u00a0needs\nrepair ", Questions: []string{" which\tprovider? ", "when\nready?"}, Note: " wait\tfor\nrepair ",
			RepairRequest: &models.RepairRequest{
				Operation: " apply-dependency-repair\t", Target: " blocked\n", Command: " repair\tblocked ",
				Evidence: []string{" provider\npending "}, Validation: []string{" check\tgraph "},
				DependencyUpdates: []models.DependencyUpdate{{TaskID: " blocked ", ExpectedDependsOn: []string{" old\t"}, DesiredDependsOn: []string{" provider\n"}}},
			},
		}
		if got := BuildAssessmentFingerprint(state, &state.Tasks[0], variant); got != want {
			t.Fatalf("whitespace/NFC variants differ: %s != %s", got, want)
		}
		var reordered models.RepairRequest
		if err := json.Unmarshal([]byte(`{"validation":["check graph"],"evidence":["provider pending"],"dependency_updates":[{"desired_depends_on":["provider"],"expected_depends_on":["old"],"task_id":"blocked"}],"command":"repair blocked","target":"blocked","operation":"apply-dependency-repair"}`), &reordered); err != nil {
			t.Fatal(err)
		}
		candidate.RepairRequest = &reordered
		if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != want {
			t.Fatal("repair field order changed digest")
		}
	})

	t.Run("material changes", func(t *testing.T) {
		cases := map[string]func(*models.State, *AssessmentFingerprintCandidate){
			"self status": func(s *models.State, _ *AssessmentFingerprintCandidate) { s.Tasks[0].Status = models.TaskStatusReady },
			"self history": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[0].History = append(s.Tasks[0].History, models.TaskHistoryEntry{Event: models.TaskEventRejectionRCARecorded})
			},
			"blocker reason": func(_ *models.State, c *AssessmentFingerprintCandidate) { c.Reason = "other reason" },
			"blocker case":   func(_ *models.State, c *AssessmentFingerprintCandidate) { c.Reason = strings.ToUpper(c.Reason) },
			"question order": func(_ *models.State, c *AssessmentFingerprintCandidate) {
				c.Questions[0], c.Questions[1] = c.Questions[1], c.Questions[0]
			},
			"repair operation": func(_ *models.State, c *AssessmentFingerprintCandidate) { c.RepairRequest.Operation = "other" },
			"repair target":    func(_ *models.State, c *AssessmentFingerprintCandidate) { c.RepairRequest.Target = "other" },
			"repair command":   func(_ *models.State, c *AssessmentFingerprintCandidate) { c.RepairRequest.Command = "other command" },
			"repair evidence": func(_ *models.State, c *AssessmentFingerprintCandidate) {
				c.RepairRequest.Evidence[0] = "other evidence"
			},
			"repair validation": func(_ *models.State, c *AssessmentFingerprintCandidate) {
				c.RepairRequest.Validation[0] = "other validation"
			},
			"repair update": func(_ *models.State, c *AssessmentFingerprintCandidate) {
				c.RepairRequest.DependencyUpdates[0].DesiredDependsOn[0] = "other"
			},
			"disposition":       func(_ *models.State, c *AssessmentFingerprintCandidate) { c.Note = "repair now" },
			"dependency status": func(s *models.State, _ *AssessmentFingerprintCandidate) { s.Tasks[2].Status = models.TaskStatusMerged },
			"dependency history": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[2].History = append(s.Tasks[2].History, models.TaskHistoryEntry{Event: "claimed"})
			},
			"dependency created": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[2].Created = s.Tasks[2].Created.Add(time.Second)
			},
			"replacement ancestor": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[1].History = append(s.Tasks[1].History, models.TaskHistoryEntry{Event: "superseded"})
			},
			"descendant status": func(s *models.State, _ *AssessmentFingerprintCandidate) { s.Tasks[3].Status = models.TaskStatusMerged },
			"descendant history": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[3].History = append(s.Tasks[3].History, models.TaskHistoryEntry{Event: "claimed"})
			},
			"descendant created": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.Tasks[3].Created = s.Tasks[3].Created.Add(time.Second)
			},
			"human task": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: "blocked"})
			},
			"human all": func(s *models.State, _ *AssessmentFingerprintCandidate) {
				s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: "all"})
			},
		}
		for name, change := range cases {
			t.Run(name, func(t *testing.T) {
				state, candidate := assessmentFingerprintFixture()
				before := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
				change(state, &candidate)
				if BuildAssessmentFingerprint(state, &state.Tasks[0], candidate) == before {
					t.Fatal("material change did not change digest")
				}
			})
		}
	})

	t.Run("nonmaterial changes", func(t *testing.T) {
		state, candidate := assessmentFingerprintFixture()
		want := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
		for i := range state.Tasks {
			state.Tasks[i].History = append(state.Tasks[i].History, models.TaskHistoryEntry{Event: models.TaskEventOrchestratorAssessment})
			state.Tasks[i].Lifecycle = &models.TaskLifecycle{Revision: 1}
		}
		state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: "unrelated"})
		state.Tasks[0].DependsOn = []string{"provider", "old", "provider"}
		state.Tasks[3], state.Tasks[4] = state.Tasks[4], state.Tasks[3]
		if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != want {
			t.Fatal("assessment-only events, ordering or unrelated notes changed digest")
		}
	})

	t.Run("nil and empty repair", func(t *testing.T) {
		state, candidate := assessmentFingerprintFixture()
		candidate.RepairRequest = nil
		want := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
		for _, repair := range []*models.RepairRequest{{}, {Evidence: []string{}, Validation: []string{}, DependencyUpdates: []models.DependencyUpdate{}}} {
			candidate.RepairRequest = repair
			if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != want {
				t.Fatal("empty repair differs from nil")
			}
		}
	})

	t.Run("state restart", func(t *testing.T) {
		state, candidate := assessmentFingerprintFixture()
		task := &state.Tasks[0]
		task.BlockedReason, task.BlockedQuestions, task.RepairRequest = &candidate.Reason, candidate.Questions, candidate.RepairRequest
		want := BuildAssessmentFingerprint(state, task, candidate)
		task.History = append(task.History, models.TaskHistoryEntry{Event: models.TaskEventOrchestratorAssessment, Note: &candidate.Note, Extra: map[string]any{AssessmentFingerprintExtraKey: want}})
		data, err := yaml.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		var restored models.State
		if err := yaml.Unmarshal(data, &restored); err != nil {
			t.Fatal(err)
		}
		task = &restored.Tasks[0]
		last := task.History[len(task.History)-1]
		candidate = AssessmentFingerprintCandidate{Reason: *task.BlockedReason, Questions: task.BlockedQuestions, RepairRequest: task.RepairRequest, Note: *last.Note}
		if got := BuildAssessmentFingerprint(&restored, task, candidate); got != want {
			t.Fatalf("restart changed digest: %s != %s", got, want)
		}
		if got, ok := IsAssessmentFingerprint(last.Extra[AssessmentFingerprintExtraKey]); !ok || got != want {
			t.Fatal("persisted digest not recognized")
		}
	})

	t.Run("persisted digest validation", func(t *testing.T) {
		valid := strings.Repeat("0123456789abcdef", 4)
		for _, value := range []any{nil, 42, []byte(valid), map[string]any{"digest": valid}, "", valid[:63], valid + "0", strings.ToUpper(valid), strings.Repeat("g", 64), " " + valid} {
			if got, ok := IsAssessmentFingerprint(value); ok || got != "" {
				t.Fatalf("accepted malformed %v (%s)", reflect.TypeOf(value), got)
			}
		}
		if got, ok := IsAssessmentFingerprint(valid); !ok || got != valid {
			t.Fatal("rejected valid digest")
		}
	})
}
