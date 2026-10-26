package tui

import (
	"testing"

	"github.com/liza-mas/liza/internal/commands"
)

func TestA7ColumnTiersKeepTheirBoundaries(t *testing.T) {
	for width, want := range map[int]ColumnTier{
		200: ColumnTierFull, 160: ColumnTierFull, 159: ColumnTierWide, 120: ColumnTierWide,
		119: ColumnTierStandard, 80: ColumnTierStandard, 79: ColumnTierMinimal, 0: ColumnTierMinimal,
	} {
		if got := ColumnTierForWidth(width); got != want {
			t.Errorf("ColumnTierForWidth(%d) = %v, want %v", width, got, want)
		}
	}
}

func TestA7TaskFilterLabelsKeepTheirSpellings(t *testing.T) {
	for filter, want := range map[TaskFilter]string{TaskFilterActive: "active", TaskFilterActiveBlocked: "active+blocked"} {
		if got := filter.Label(); got != want {
			t.Errorf("filter %v label = %q, want %q", filter, got, want)
		}
	}
}

func TestA7StatusMapKeysKeepModelSpellings(t *testing.T) {
	for status, want := range map[string]string{
		"WORKING": "●", "PLANNING": "●", "STARTING": "●", "REVIEWING": "●", "HANDOFF": "●",
		"IDLE": "○", "WAITING": "○",
	} {
		if got := StatusDot(status); got != want {
			t.Errorf("StatusDot(%q) = %q, want %q", status, got, want)
		}
	}
	for status, want := range map[string]string{
		"WORKING": string(ColorActive), "PLANNING": string(ColorPlanning), "STARTING": string(ColorPlanning),
		"REVIEWING": string(ColorReview), "IDLE": string(ColorIdle), "WAITING": string(ColorIdle), "HANDOFF": string(ColorHandoff),
		"RUNNING": string(ColorActive), "PAUSED": string(ColorPlanning), "CHECKPOINT": string(ColorHandoff), "STOPPED": string(ColorRejected),
		"COMPLETED": string(ColorApproved), "IN_PROGRESS": string(ColorActive), "ABORTED": string(ColorRejected),
		"MERGED": string(ColorApproved), "BLOCKED": string(ColorRejected), "INTEGRATION_FAILED": string(ColorRejected),
		"ABANDONED": string(ColorTerminal), "SUPERSEDED": string(ColorTerminal), "DRAFT": string(ColorBareDraft),
	} {
		if got := string(StatusColor(status)); got != want {
			t.Errorf("StatusColor(%q) = %q, want %q", status, got, want)
		}
	}
	if len(exactStatusColors) != 20 || len(idleDotStatuses) != 2 {
		t.Errorf("exact status colors = %d (want 20), idle dot statuses = %d (want 2)", len(exactStatusColors), len(idleDotStatuses))
	}
}

func TestA7LayoutAndAnswerConstantsKeepTheirValues(t *testing.T) {
	for name, pair := range map[string][2]int{
		"panel chrome": {panelChromeWidth, 4}, "overlay chrome": {overlayChromeWidth, 8}, "header and footer": {headerFooterHeight, 2},
		"expanded identity minimum": {expandedIdentityMinWidth, 90}, "task title gap": {taskPanelTitleMinGap, 2},
		"compact ID": {compactAgentIDWidth, 20}, "compact role": {compactAgentRoleWidth, 16},
		"expanded ID": {expandedAgentIDWidth, 25}, "expanded role": {expandedAgentRoleWidth, 21},
		"full tier": {columnTierFullMinWidth, 160}, "wide tier": {columnTierWideMinWidth, 120}, "standard tier": {columnTierStandardMinWidth, 80},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}
	if inlineConfirmYes != "y" || pausedBreakerLabel != "BREAKER" || commands.OperatorActor != "operator" {
		t.Errorf("answers changed: confirm %q, breaker label %q, actor %q", inlineConfirmYes, pausedBreakerLabel, commands.OperatorActor)
	}
	for answer, want := range map[string]bool{"y": true, "Yes": true, "YES": true, "n": false, "": false, "no": false} {
		if got := isYes(answer); got != want {
			t.Errorf("isYes(%q) = %v, want %v", answer, got, want)
		}
	}
}
