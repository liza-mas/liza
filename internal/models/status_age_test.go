package models

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestIsStatusTransitionEvent(t *testing.T) {
	tests := []struct {
		name           string
		event          TaskEventName
		wantTransition bool
		wantClassified bool
	}{
		{"claimed is a transition", TaskEventClaimed, true, true},
		{"reclaim after rejection is a transition", TaskEventReclaimedAfterRejection, true, true},
		{"reassign after rejection is a transition", TaskEventReassignedAfterRejection, true, true},
		{"integration-fix claim is a transition", TaskEventClaimedForIntegrationFix, true, true},
		{"fresh recovery is a transition", TaskEventRecoveredFresh, true, true},
		{"failed fresh recovery is a transition", TaskEventRecoveryFreshFailed, true, true},
		{"claim release is not a transition", TaskEventClaimReleased, false, true},
		{"transition_executed is recorded on the trigger task", TaskEventTransitionExecuted, false, true},
		{"acceptance remap is bookkeeping", TaskEventAcceptanceCommitsRemapped, false, true},
		{"new attempt preserves status", TaskEventNewAttempt, false, true},
		{"handoff changes agent status only", TaskEventHandoffInitiated, false, true},
		{"unknown name is unclassified", "invented_event", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transition, classified := IsStatusTransitionEvent(tt.event)
			if transition != tt.wantTransition || classified != tt.wantClassified {
				t.Errorf("IsStatusTransitionEvent(%q) = (%v, %v), want (%v, %v)",
					tt.event, transition, classified, tt.wantTransition, tt.wantClassified)
			}
		})
	}
}

// Every declared task-event constant must be classified, so adding an event
// without deciding whether it moves status fails here rather than silently
// shifting a reported duration. Coverage is derived from the declarations
// themselves so the guard cannot fall behind history.go.
func TestEveryDeclaredEventIsClassified(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "history.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, decl := range file.Decls {
		constants, ok := decl.(*ast.GenDecl)
		if !ok || constants.Tok != token.CONST {
			continue
		}
		for _, spec := range constants.Specs {
			value := spec.(*ast.ValueSpec)
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "TaskEvent") {
					continue
				}
				// Fail explicitly if declarations change shape; never skip an
				// event merely because this guard cannot read its value.
				if i >= len(value.Values) {
					t.Fatalf("%s: expected explicit string value", name.Name)
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s: expected string literal", name.Name)
				}
				event, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("%s: %v", name.Name, err)
				}
				count++
				if _, classified := IsStatusTransitionEvent(event); !classified {
					t.Errorf("%s (%q) is declared but unclassified", name.Name, event)
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("no task-event constants found in history.go")
	}
}

func TestTimeInStatus(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }

	tests := []struct {
		name string
		task *Task
		want time.Duration
	}{
		{
			name: "measures from the last transition, not the last entry",
			task: &Task{
				Created: ago(10 * time.Hour),
				History: []TaskHistoryEntry{
					{Time: ago(10 * time.Hour), Event: TaskEventCreated},
					{Time: ago(6 * time.Hour), Event: TaskEventClaimed},
					{Time: ago(3 * time.Hour), Event: TaskEventMerged},
					{Time: ago(1 * time.Hour), Event: TaskEventClaimReleased},
				},
			},
			want: 3 * time.Hour,
		},
		{
			name: "reclaim after rejection restarts the clock",
			task: &Task{
				Created: ago(9 * time.Hour),
				History: []TaskHistoryEntry{
					{Time: ago(9 * time.Hour), Event: TaskEventCreated},
					{Time: ago(8 * time.Hour), Event: TaskEventRejected},
					{Time: ago(2 * time.Hour), Event: TaskEventReclaimedAfterRejection},
				},
			},
			want: 2 * time.Hour,
		},
		{
			name: "transition_executed on the trigger task does not restart it",
			task: &Task{
				Created: ago(9 * time.Hour),
				History: []TaskHistoryEntry{
					{Time: ago(5 * time.Hour), Event: TaskEventApproved},
					{Time: ago(1 * time.Hour), Event: TaskEventTransitionExecuted},
				},
			},
			want: 5 * time.Hour,
		},
		{
			name: "unclassified event does not restart it",
			task: &Task{
				Created: ago(9 * time.Hour),
				History: []TaskHistoryEntry{
					{Time: ago(4 * time.Hour), Event: TaskEventBlocked},
					{Time: ago(1 * time.Hour), Event: "invented_event"},
				},
			},
			want: 4 * time.Hour,
		},
		{
			name: "no transition in history falls back to created",
			task: &Task{
				Created: ago(7 * time.Hour),
				History: []TaskHistoryEntry{
					{Time: ago(7 * time.Hour), Event: TaskEventCreated},
				},
			},
			want: 7 * time.Hour,
		},
		{
			name: "empty history falls back to created",
			task: &Task{Created: ago(2 * time.Hour)},
			want: 2 * time.Hour,
		},
		{
			name: "nil task is zero",
			task: nil,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TimeInStatus(tt.task, now); got != tt.want {
				t.Errorf("TimeInStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}
