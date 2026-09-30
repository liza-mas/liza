package statevalidate

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestValidateSprint_NegativeNumber(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Sprint.Number = -1

	err := sprintErr(state, "", true)
	if err == nil {
		t.Fatal("Expected error for negative sprint number")
	}
	if !strings.Contains(err.Error(), "non-negative") {
		t.Errorf("Error = %q, want to contain 'non-negative'", err.Error())
	}
}

func TestValidateSprint_ZeroNumber_Legacy(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.Sprint.Number = 0 // legacy pre-multi-sprint state

	err := sprintErr(state, "", true)
	if err != nil {
		t.Fatalf("Expected no error for legacy zero sprint number, got: %v", err)
	}
}

func TestValidateSprintHistory_Valid(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.SprintHistory = []models.SprintSummary{
		{
			ID:        "sprint-1",
			Number:    1,
			Status:    models.SprintStatusCompleted,
			Started:   now.Add(-2 * time.Hour),
			Ended:     now.Add(-1 * time.Hour),
			TasksDone: 3,
		},
	}

	err := sprintHistoryErr(state)
	if err != nil {
		t.Fatalf("Expected no error for valid sprint history, got: %v", err)
	}
}

func TestValidateSprintHistory_DuplicateID(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.SprintHistory = []models.SprintSummary{
		{
			ID:      "sprint-1",
			Number:  1,
			Status:  models.SprintStatusCompleted,
			Started: now.Add(-2 * time.Hour),
			Ended:   now.Add(-1 * time.Hour),
		},
		{
			ID:      "sprint-1",
			Number:  2,
			Status:  models.SprintStatusCompleted,
			Started: now.Add(-1 * time.Hour),
			Ended:   now,
		},
	}

	err := sprintHistoryErr(state)
	if err == nil {
		t.Fatal("Expected error for duplicate sprint ID in history")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("Error = %q, want to contain 'duplicate'", err.Error())
	}
}

func TestValidateSprintHistory_MissingID(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.SprintHistory = []models.SprintSummary{
		{
			ID:      "",
			Number:  1,
			Status:  models.SprintStatusCompleted,
			Started: now.Add(-2 * time.Hour),
			Ended:   now.Add(-1 * time.Hour),
		},
	}

	err := sprintHistoryErr(state)
	if err == nil {
		t.Fatal("Expected error for missing sprint history ID")
	}
	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("Error = %q, want to contain 'missing id'", err.Error())
	}
}

func TestValidateSprintHistory_InvalidNumber(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.SprintHistory = []models.SprintSummary{
		{
			ID:      "sprint-0",
			Number:  0,
			Status:  models.SprintStatusCompleted,
			Started: now.Add(-2 * time.Hour),
			Ended:   now.Add(-1 * time.Hour),
		},
	}

	err := sprintHistoryErr(state)
	if err == nil {
		t.Fatal("Expected error for sprint history number < 1")
	}
	if !strings.Contains(err.Error(), "number must be >= 1") {
		t.Errorf("Error = %q, want to contain 'number must be >= 1'", err.Error())
	}
}

func TestValidateSprintHistory_Empty(t *testing.T) {
	state := testhelpers.CreateValidState()
	state.SprintHistory = []models.SprintSummary{}

	err := sprintHistoryErr(state)
	if err != nil {
		t.Fatalf("Expected no error for empty sprint history, got: %v", err)
	}
}
