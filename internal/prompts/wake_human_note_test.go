package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestDetermineWakeTrigger_HumanNoteRanksAfterDiscoveriesBeforePlanning(t *testing.T) {
	t.Parallel()
	if got := determineWakeTrigger(5, 0, 0, 0, 1, false, false, []planningTaskData{{TaskID: "plan-1"}}, 0); got != "HUMAN_NOTE" {
		t.Fatalf("with an unseen note and planning complete, trigger = %q, want HUMAN_NOTE", got)
	}
	if got := determineWakeTrigger(5, 0, 0, 1, 1, false, false, nil, 0); got != "IMMEDIATE_DISCOVERY" {
		t.Fatalf("discovery must outrank a note, got %q", got)
	}
	if got := determineWakeTrigger(5, 0, 0, 0, 0, false, false, nil, 0); got != "UNKNOWN" {
		t.Fatalf("no note, no trigger: got %q", got)
	}
}

func TestBuildInstructionsForWakeTrigger_HumanNoteRendersEveryUnseenNote(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 14, 13, 21, 0, 0, time.UTC)
	notes := []models.HumanNote{
		{Timestamp: now, For: "all", Message: "Run omni-ee narrow-inherited-dependencies plan-1 --selections narrow.yaml"},
		{Timestamp: now.Add(time.Minute), For: "task-7", Message: "second request"},
	}
	rendered, err := buildInstructionsForWakeTrigger("HUMAN_NOTE", "orchestrator-1", wakeTemplateData{}, nil, notes)
	if err != nil {
		t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
	}
	for _, want := range []string{"2 note(s)", "for: all", "narrow-inherited-dependencies plan-1", "for: task-7", "second request", "stop at\n   the first failure"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered instructions lack %q:\n%s", want, rendered)
		}
	}
}
