package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestStatusColor_ExactMatch(t *testing.T) {
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		// Active work
		{"WORKING", ColorActive},
		{"RUNNING", ColorActive},
		// Planning
		{"PLANNING", ColorPlanning},
		{"STARTING", ColorPlanning},
		{"PAUSED", ColorPlanning},
		// Review
		{"REVIEWING", ColorReview},
		// Idle/waiting
		{"IDLE", ColorIdle},
		{"WAITING", ColorIdle},
		// Handoff
		{"HANDOFF", ColorHandoff},
		{"CHECKPOINT", ColorHandoff},
		// Approved/done
		{"MERGED", ColorApproved},
		// Rejected/blocked
		{"BLOCKED", ColorRejected},
		{"INTEGRATION_FAILED", ColorRejected},
		{"STOPPED", ColorRejected},
		// Terminal
		{"ABANDONED", ColorTerminal},
		{"SUPERSEDED", ColorTerminal},
		// Bare draft
		{"DRAFT", ColorBareDraft},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusColor(tt.status)
			if got != tt.want {
				t.Errorf("StatusColor(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusColor_SuffixMatch(t *testing.T) {
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		// *_REJECTED → red
		{"CODE_REJECTED", ColorRejected},
		{"PLAN_REJECTED", ColorRejected},
		// *_APPROVED → green
		{"CODE_APPROVED", ColorApproved},
		{"PLAN_APPROVED", ColorApproved},
		// *_PARTIALLY_APPROVED → green dim
		{"CODE_PARTIALLY_APPROVED", ColorPartialApproved},
		// *_PLANNING → yellow
		{"CODE_PLANNING", ColorPlanning},
		{"US_PLANNING", ColorPlanning},
		// *_TO_REVIEW → blue
		{"CODE_TO_REVIEW", ColorReview},
		// *_READY_FOR_REVIEW → blue
		{"CODE_READY_FOR_REVIEW", ColorReview},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusColor(tt.status)
			if got != tt.want {
				t.Errorf("StatusColor(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusColor_PrefixMatch(t *testing.T) {
	tests := []struct {
		status string
		want   lipgloss.Color
	}{
		// IMPLEMENTING_* → cyan
		{"IMPLEMENTING_CODE", ColorActive},
		{"IMPLEMENTING_PLAN", ColorActive},
		// REVIEWING_* → blue
		{"REVIEWING_CODE", ColorReview},
		{"REVIEWING_PLAN", ColorReview},
		// DRAFT_* (qualified) → yellow
		{"DRAFT_CODING_PLAN", ColorPlanning},
		{"DRAFT_EPIC_PLAN", ColorPlanning},
		{"DRAFT_CODE", ColorPlanning},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusColor(tt.status)
			if got != tt.want {
				t.Errorf("StatusColor(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStatusColor_QualifiedDraftVsBareDraft(t *testing.T) {
	// DRAFT (unqualified) → dim white
	got := StatusColor("DRAFT")
	if got != ColorBareDraft {
		t.Errorf("StatusColor(\"DRAFT\") = %q, want %q (bare draft / dim white)", got, ColorBareDraft)
	}

	// DRAFT_CODING_PLAN (qualified) → yellow
	got = StatusColor("DRAFT_CODING_PLAN")
	if got != ColorPlanning {
		t.Errorf("StatusColor(\"DRAFT_CODING_PLAN\") = %q, want %q (planning / yellow)", got, ColorPlanning)
	}
}

func TestStatusColor_Fallback(t *testing.T) {
	got := StatusColor("UNKNOWN_STATUS_XYZ")
	if got != ColorFallback {
		t.Errorf("StatusColor(\"UNKNOWN_STATUS_XYZ\") = %q, want %q (fallback / white)", got, ColorFallback)
	}

	got = StatusColor("")
	if got != ColorFallback {
		t.Errorf("StatusColor(\"\") = %q, want %q (fallback / white)", got, ColorFallback)
	}
}

func TestStatusDot_ActiveStatuses(t *testing.T) {
	active := []string{"WORKING", "IMPLEMENTING_CODE", "REVIEWING", "PLANNING", "STARTING", "HANDOFF"}
	for _, s := range active {
		got := StatusDot(s)
		if got != "●" {
			t.Errorf("StatusDot(%q) = %q, want \"●\" (filled dot for active)", s, got)
		}
	}
}

func TestStatusDot_IdleStatuses(t *testing.T) {
	idle := []string{"IDLE", "WAITING"}
	for _, s := range idle {
		got := StatusDot(s)
		if got != "○" {
			t.Errorf("StatusDot(%q) = %q, want \"○\" (hollow dot for idle)", s, got)
		}
	}
}

func TestNewStyles_ReturnsPopulatedStruct(t *testing.T) {
	s := NewStyles(120)

	// Verify key styles are not zero-value (check that they have been set by verifying render produces output)
	if s.HeaderBar.GetWidth() != 120 {
		t.Errorf("HeaderBar width = %d, want 120", s.HeaderBar.GetWidth())
	}
	if s.FooterBar.GetWidth() != 120 {
		t.Errorf("FooterBar width = %d, want 120", s.FooterBar.GetWidth())
	}
	if s.AgentPanel.GetWidth() != 120 {
		t.Errorf("AgentPanel width = %d, want 120", s.AgentPanel.GetWidth())
	}
	if s.TaskPanel.GetWidth() != 120 {
		t.Errorf("TaskPanel width = %d, want 120", s.TaskPanel.GetWidth())
	}
	if s.ActivityPanel.GetWidth() != 120 {
		t.Errorf("ActivityPanel width = %d, want 120", s.ActivityPanel.GetWidth())
	}
}

func TestNewStyles_AdaptsToWidth(t *testing.T) {
	narrow := NewStyles(80)
	wide := NewStyles(160)

	if narrow.HeaderBar.GetWidth() != 80 {
		t.Errorf("narrow HeaderBar width = %d, want 80", narrow.HeaderBar.GetWidth())
	}
	if wide.HeaderBar.GetWidth() != 160 {
		t.Errorf("wide HeaderBar width = %d, want 160", wide.HeaderBar.GetWidth())
	}
}

func TestColorConstants_AllDefined(t *testing.T) {
	// Verify all 11 semantic color constants are non-empty
	colors := map[string]lipgloss.Color{
		"ColorActive":          ColorActive,
		"ColorPlanning":        ColorPlanning,
		"ColorReview":          ColorReview,
		"ColorIdle":            ColorIdle,
		"ColorHandoff":         ColorHandoff,
		"ColorApproved":        ColorApproved,
		"ColorPartialApproved": ColorPartialApproved,
		"ColorRejected":        ColorRejected,
		"ColorTerminal":        ColorTerminal,
		"ColorBareDraft":       ColorBareDraft,
		"ColorFallback":        ColorFallback,
	}

	for name, c := range colors {
		if string(c) == "" {
			t.Errorf("%s is empty", name)
		}
	}

	if len(colors) != 11 {
		t.Errorf("expected 11 color constants, got %d", len(colors))
	}
}
