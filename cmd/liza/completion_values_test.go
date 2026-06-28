package main

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestFilterCompletionValuesSortsDedupesAndMatchesPrefix(t *testing.T) {
	got := filterCompletionValues([]string{"coder", "codex", "coder", "", "claude", " code-reviewer "}, "co")
	want := []string{"code-reviewer", "coder", "codex"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterCompletionValues() = %#v, want %#v", got, want)
	}
}

func TestCompletionIDsFromStateAreSorted(t *testing.T) {
	state := &models.State{
		Tasks: []models.Task{
			{ID: "task-20"},
			{ID: "task-1"},
			{ID: ""},
		},
		Agents: map[string]models.Agent{
			"coder-2":         {Role: "coder"},
			"code-reviewer-1": {Role: "code-reviewer"},
			"":                {Role: "invalid"},
		},
	}

	if got, want := taskIDsFromState(state), []string{"task-1", "task-20"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("taskIDsFromState() = %#v, want %#v", got, want)
	}
	if got, want := agentIDsFromState(state), []string{"code-reviewer-1", "coder-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("agentIDsFromState() = %#v, want %#v", got, want)
	}
}
