package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestTaskCorrectiveContextDelivery(t *testing.T) {
	for _, role := range []string{"coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
			prior := "The requested fixture is missing; reframe before continuing."
			oldResolution := "Obsolete resolution marker"
			resolution := "Fixture now provisioned at fixtures/new-record.json; verify it."
			agentID := "orchestrator-1"
			state.Tasks = []models.Task{{
				ID: "task-42", Status: models.TaskStatusImplementing, Iteration: 2,
				RejectionReason: &prior,
				History: []models.TaskHistoryEntry{
					{Time: at.Add(-time.Hour), Event: models.TaskEventUnblocked, Reason: &oldResolution},
					{Time: at, Event: models.TaskEventUnblocked, Agent: &agentID, Reason: &resolution},
				},
			}}
			note := models.HumanNote{
				Timestamp: at.Add(time.Minute), For: "task-42", Message: "The human supplied the hardBreak record.",
				Extra: map[string]any{"source": "operator_cli", "operation": "add-human-note"},
			}
			note.MarkSeenByOrchestrator(at.Add(2 * time.Minute))
			state.HumanNotes = []models.HumanNote{
				note,
				{Timestamp: at.Add(3 * time.Minute), For: "all", Message: "Check the newly committed evidence before blocking."},
				{Timestamp: at.Add(4 * time.Minute), For: "other-task", Message: "UNRELATED NOTE MUST NOT LEAK"},
			}
			config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: t.TempDir()}
			output, err := testBuildPrompt(t, state, config, "task-42")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{resolution, note.Message, state.HumanNotes[1].Message, prior} {
				if !strings.Contains(output, want) {
					t.Errorf("%s prompt missing persisted context %q", role, want)
				}
			}
			for _, unwanted := range []string{oldResolution, state.HumanNotes[2].Message} {
				if strings.Contains(output, unwanted) {
					t.Errorf("%s prompt includes stale/unrelated context %q", role, unwanted)
				}
			}
			if index := strings.Index(output, resolution); index >= strings.Index(output, prior) || index < 0 {
				t.Error("latest unblock resolution must precede retained rejection")
			}
		})
	}
}

func TestTaskCorrectiveContextFirstIteration(t *testing.T) {
	resolver := embeddedPipelineResolver(t)
	for _, role := range []string{"epic-planner", "epic-plan-reviewer", "us-writer", "us-reviewer", "architect", "architecture-reviewer", "code-planner", "code-plan-reviewer", "coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{{ID: "first-task", Iteration: 1, Status: models.TaskStatusImplementing}}
			state.HumanNotes = []models.HumanNote{{For: "first-task", Message: "Evidence is already available in fixtures/record.json."}}
			config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: t.TempDir()}
			prepareLegacyReferenceContext(t, state, config, resolver, "first-task")
			strategy, err := NewRoleStrategy(role, resolver)
			if err != nil {
				t.Fatal(err)
			}
			output, err := strategy.BuildPrompt(state, config, "first-task")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output, state.HumanNotes[0].Message) || strings.Contains(output, "=== PRIOR REJECTION") {
				t.Fatal("first iteration must receive notes without requiring a prior rejection")
			}
		})
	}
}

func TestTaskCorrectiveContextBoundsAndNonmutation(t *testing.T) {
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	longText := strings.Repeat("界", 4100)
	longLabel := strings.Repeat("界", 132)
	state := &models.State{Tasks: []models.Task{{ID: "task", History: []models.TaskHistoryEntry{
		{Time: at, Event: models.TaskEventUnblocked, Agent: &longLabel, Reason: &longText},
		{Time: at.Add(time.Hour), Event: models.TaskEventUnblocked}, // empty history does not erase a resolution
	}}}}
	for i := 0; i < 10; i++ {
		target := "task"
		if i%2 == 0 {
			target = "all"
		}
		state.HumanNotes = append(state.HumanNotes, models.HumanNote{Timestamp: at.Add(time.Duration(i) * time.Minute), For: target, Message: fmt.Sprintf("note-%d", i)})
	}
	state.HumanNotes[9].Message = longText
	state.HumanNotes[9].Extra = map[string]any{"source": longLabel, "operation": longLabel, "arbitrary": "PRIVATE METADATA MUST NOT RENDER"}
	state.HumanNotes = append(state.HumanNotes,
		models.HumanNote{For: "other", Message: "UNRELATED OVERFLOW"},
		models.HumanNote{For: "task", Message: " \n\t"})
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	context := buildTaskCorrectiveContext(&state.Tasks[0], state)
	output, err := prompts.BuildRoleContext("coder", nil, &prompts.RoleContextData{RoleType: "doer", TaskID: "task", CorrectiveContext: context})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"2 earlier matching notes omitted", "get human_notes --json", "get task --json", "TRUNCATED", "LEGACY/AUDIT CONTEXT", strings.Repeat("界", 4096) + "...", strings.Repeat("界", 128) + "..."} {
		if !strings.Contains(output, want) {
			t.Errorf("bounded render missing %q", prompts.TruncateText(want, 80))
		}
	}
	for _, unwanted := range []string{"note-0", "note-1", "UNRELATED OVERFLOW", "PRIVATE METADATA MUST NOT RENDER", longText} {
		if strings.Contains(output, unwanted) {
			t.Errorf("bounded render includes %q", prompts.TruncateText(unwanted, 80))
		}
	}
	wantLabel := strings.Repeat("界", correctiveLabelRunes) + "..."
	if context.Unblock.Agent != wantLabel || context.Notes[7].Source != wantLabel || context.Notes[7].Operation != wantLabel {
		t.Fatal("each provenance label must be bounded independently of the message body")
	}
	if strings.Index(output, "note-2") >= strings.Index(output, "note-8") {
		t.Error("retained notes must preserve append order")
	}
	if len(output) > 40000 {
		t.Fatalf("corrective render grew beyond bounded fixture ceiling: %d bytes", len(output))
	}
	after, err := json.Marshal(state)
	if err != nil || string(before) != string(after) {
		t.Fatal("corrective projection/render mutated source state")
	}
}

func TestTaskCorrectiveContextSeenFlagsAndProvenance(t *testing.T) {
	state := &models.State{Tasks: []models.Task{{ID: "task"}}, HumanNotes: []models.HumanNote{
		{For: "task", Message: "Verify current fixture", Extra: map[string]any{"source": "operator_cli", "operation": "add-human-note"}},
		{For: "all", Message: "Task other deleted: historical audit"},
		{For: "task", Message: "Legacy recovery evidence"},
	}}
	context := buildTaskCorrectiveContext(&state.Tasks[0], state)
	for i := range state.HumanNotes {
		state.HumanNotes[i].MarkSeenByOrchestrator(time.Now().UTC())
	}
	seen := buildTaskCorrectiveContext(&state.Tasks[0], state)
	before, _ := json.Marshal(context)
	after, _ := json.Marshal(seen)
	if string(before) != string(after) {
		t.Fatal("orchestrator consumption must not suppress task evidence")
	}
	output, err := prompts.BuildRoleContext("code-reviewer", nil, &prompts.RoleContextData{RoleType: "reviewer", TaskID: "task", ReviewCommit: "reviewed-sha", CorrectiveContext: seen})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(output, "OPERATOR INPUT —") != 1 || strings.Count(output, "LEGACY/AUDIT CONTEXT —") != 2 || !strings.Contains(output, "source=operator_cli; operation=add-human-note") {
		t.Fatal("operator provenance must be distinguished from unclassified legacy/audit records")
	}
	state.HumanNotes = nil
	if buildTaskCorrectiveContext(&state.Tasks[0], state) != nil {
		t.Fatal("empty source should not add corrective context")
	}
}
