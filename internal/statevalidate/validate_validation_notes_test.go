package statevalidate

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestValidationNotesRequireActualOutputChild(t *testing.T) {
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	for _, tc := range []struct{ name, id, rolePair, want string }{
		{"selected child", "plan-code-1", "coding-pair", ""},
		{"wrong output child", "plan-code-0", "coding-pair", "selects another output's child"},
		{"wrong role", "plan-code-1", "code-planning-pair", "selects another output's child"},
		{"foreign kind incumbent", "foreign", "coding-pair", "selects another output's child"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := models.Task{ID: "plan", RolePair: "code-planning-pair", Output: make([]models.OutputEntry, 2), TransitionsExecuted: map[string]bool{"code-plan-to-coding": true}, PlanCheck: &models.PlanCheck{Verdict: models.PlanCheckPassed, Notes: []models.PlanValidationNote{{OutputIndex: 1, Message: "Probe guidance."}}}}
			child := models.Task{ID: tc.id, RolePair: tc.rolePair, ParentTasks: []string{parent.ID}, ValidationNotes: parent.ValidationNotesForOutput(1)}
			state := &models.State{Tasks: []models.Task{parent, child}}
			v := &violations{}
			validateValidationNotes(v, &child, state, resolver)
			err := v.err()
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validation: %v; want %q", err, tc.want)
			}
		})
	}
}

func TestValidationNotesChildPayloadTotalIsBounded(t *testing.T) {
	parent := models.Task{ID: "plan", Output: make([]models.OutputEntry, 1), PlanCheck: &models.PlanCheck{Verdict: models.PlanCheckPassed}}
	child := models.Task{ID: "child", ParentTasks: []string{parent.ID}}
	for i := 0; i < 5; i++ {
		child.ValidationNotes = append(child.ValidationNotes, models.ValidationNote{ParentTask: parent.ID, OutputIndex: 0, Message: strings.Repeat("x", models.MaxValidationNoteBytes)})
	}
	v := &violations{}
	validateValidationNotes(v, &child, &models.State{Tasks: []models.Task{parent, child}}, nil)
	if err := v.err(); err == nil || !strings.Contains(err.Error(), "total message limit") {
		t.Fatalf("oversized child note batch accepted: %v", err)
	}
}
