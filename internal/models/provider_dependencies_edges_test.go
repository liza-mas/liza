package models_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
	"gopkg.in/yaml.v3"
)

func providerReadyFixture(t *testing.T) (*models.State, *pipeline.Resolver) {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatal(err)
	}
	return &models.State{Tasks: []models.Task{
		{ID: "provider", RolePair: "architecture-pair", Status: models.TaskStatusMerged,
			TransitionsExecuted: map[string]bool{"architecture-to-code-plan": true},
			Output:              []models.OutputEntry{{}, {}, {}}},
		{ID: "consumer", RolePair: "architecture-pair", Status: models.TaskStatus("DRAFT_ARCHITECTURE"),
			DependsOn: []string{"provider"}, ProviderDependencies: []models.ProviderDependency{
				{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0, 2}},
			}},
		{ID: "provider-cp-0", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}},
		{ID: "provider-cp-2", RolePair: "code-planning-pair", Status: models.TaskStatusMerged, ParentTasks: []string{"provider"}},
		// An unselected output does not delay the consumer.
		{ID: "provider-cp-1", RolePair: "code-planning-pair", Status: models.TaskStatus("DRAFT_CODING_PLAN"), ParentTasks: []string{"provider"}},
	}}, pipeline.NewResolver(cfg)
}

func TestProviderDependencyRuntimeFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*models.State)
		invalid bool
	}{
		{"missing provider", func(s *models.State) { s.Tasks[0].ID = "other" }, true},
		{"pending provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE") }, false},
		{"held provider", func(s *models.State) { s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld} }, false},
		{"replanned provider", func(s *models.State) { s.Tasks[0].TransitionsExecuted["replanned"] = true }, true},
		{"retired handoff", func(s *models.State) { s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced} }, true},
		{"abandoned provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatusAbandoned }, true},
		{"superseded provider", func(s *models.State) { s.Tasks[0].Status = models.TaskStatusSuperseded }, true},
		{"missing transition marker", func(s *models.State) { s.Tasks[0].TransitionsExecuted = nil }, false},
		{"missing provider output", func(s *models.State) { s.Tasks[0].Output = nil }, true},
		{"output out of range", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{3} }, true},
		{"deduplicated output", func(s *models.State) { s.Tasks[0].Output[2].Kind = "bootstrap" }, true},
		{"wrong source role", func(s *models.State) { s.Tasks[0].RolePair = "coding-pair" }, true},
		{"unknown transition", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Transition = "missing" }, true},
		{"non fan-out transition", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Transition = "us-to-coding" }, true},
		{"partial child generation", func(s *models.State) { s.Tasks[3].ID = "other-child" }, false},
		{"pending child", func(s *models.State) { s.Tasks[3].Status = models.TaskStatus("DRAFT_CODING_PLAN") }, false},
		{"approved only child", func(s *models.State) { s.Tasks[3].Status = models.TaskStatus("CODING_PLAN_APPROVED") }, false},
		{"wrong child role", func(s *models.State) { s.Tasks[3].RolePair = "architecture-pair" }, true},
		{"wrong parent", func(s *models.State) { s.Tasks[3].ParentTasks = []string{"other"} }, true},
		{"missing parent", func(s *models.State) { s.Tasks[3].ParentTasks = nil }, true},
		{"multiple parents", func(s *models.State) { s.Tasks[3].ParentTasks = []string{"provider", "other"} }, true},
		{"retired child", func(s *models.State) { s.Tasks[3].Status = models.TaskStatusSuperseded }, true},
		{"replanned child", func(s *models.State) { s.Tasks[3].TransitionsExecuted = map[string]bool{"replanned": true} }, true},
		{"malformed declaration", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{-1} }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, pr := providerReadyFixture(t)
			tc.mutate(state)
			consumer := state.FindTask("consumer")
			unmet := models.UnmetProviderDependencies(consumer, state.Tasks, pr)
			if len(unmet) != 1 || unmet[0].Invalid() != tc.invalid || unmet[0].Reason == "" {
				t.Fatalf("unmet prerequisites = %+v, want one explanatory result (invalid=%t)", unmet, tc.invalid)
			}
			if consumer.IsClaimable("architect", state.Tasks, pr) || models.IsRoleTaskReady(state, consumer, "architect", pr, time.Now()) {
				t.Fatal("consumer advertised as ready with unmet provider prerequisite")
			}
			if !models.BlockedByDependencies(consumer, pr, models.NewDependencyResolver(state)) {
				t.Fatal("dependency diagnostics omit provider hold")
			}
			if got := models.NewDependencyResolver(state).UnmetDependencies(consumer, pr); len(got) == 0 {
				t.Fatal("dependency resolver omitted provider prerequisites")
			}
		})
	}
}

func TestProviderDependencyPendingStateDoesNotHideInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*models.State)
		invalid bool
	}{
		{"known valid pending provider", func(*models.State) {}, false},
		{"unknown future outputs", func(s *models.State) { s.Tasks[0].Output = nil; s.Tasks = s.Tasks[:2] }, false},
		{"known invalid bound", func(s *models.State) { s.Tasks[1].ProviderDependencies[0].Outputs = []int{3} }, true},
		{"known invalid Kind", func(s *models.State) { s.Tasks[0].Output[2].Kind = "bootstrap-precommit" }, true},
		{"known wrong child role", func(s *models.State) { s.Tasks[3].RolePair = "architecture-pair" }, true},
		{"known multiple parents", func(s *models.State) { s.Tasks[3].ParentTasks = []string{"provider", "other"} }, true},
		// A retired child is stale evidence on an unstarted consumer (ADR-0187),
		// so these rows use a started one.
		{"known retired child", func(s *models.State) { startedConsumer(s); s.Tasks[3].Status = models.TaskStatusSuperseded }, true},
		{"known replanned child", func(s *models.State) {
			startedConsumer(s)
			s.Tasks[3].TransitionsExecuted = map[string]bool{"replanned": true}
		}, true},
		{"known retired child handoff", func(s *models.State) {
			startedConsumer(s)
			s.Tasks[3].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced}
		}, true},
		{"unknown outputs with invalid existing child", func(s *models.State) { s.Tasks[0].Output = nil; s.Tasks[3].ParentTasks = []string{"other"} }, true},
		{"earlier pending child cannot hide later invalid child", func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatusMerged
			s.Tasks[2].Status = models.TaskStatus("DRAFT_CODING_PLAN")
			s.Tasks[3].ParentTasks = []string{"other"}
		}, true},
		{"unexecuted marker cannot hide invalid child", func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatusMerged
			s.Tasks[0].TransitionsExecuted = nil
			s.Tasks[3].ParentTasks = []string{"other"}
		}, true},
		{"held provider cannot hide invalid child", func(s *models.State) {
			s.Tasks[0].Status = models.TaskStatusMerged
			s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld}
			s.Tasks[3].ParentTasks = []string{"other"}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, pr := providerReadyFixture(t)
			state.Tasks[0].Status = models.TaskStatus("DRAFT_ARCHITECTURE")
			tc.mutate(state)
			unmet := models.UnmetProviderDependencies(state.FindTask("consumer"), state.Tasks, pr)
			if len(unmet) != 1 || unmet[0].Invalid() != tc.invalid || unmet[0].Reason == "" {
				t.Fatalf("runtime prerequisite = %+v, want invalid=%t", unmet, tc.invalid)
			}
			if !tc.invalid && unmet[0].Kind != models.DependencyUnsatisfiedPending {
				t.Fatalf("future prerequisite = %+v, want pending", unmet)
			}
			if err := statevalidate.ValidateProviderDependencies(state, pr); (err != nil) != tc.invalid {
				t.Fatalf("state validation = %v disagrees with runtime invalid=%t", err, tc.invalid)
			}
		})
	}
}

// startedConsumer records a past claim on the fixture consumer.
func startedConsumer(s *models.State) {
	consumer := s.FindTask("consumer")
	consumer.History = append(consumer.History, models.TaskHistoryEntry{Event: models.TaskEventClaimed})
}

type legacyProviderResolver struct{ models.PipelineResolver }

func TestProviderDependencyRequiresOptionalProjection(t *testing.T) {
	state, pr := providerReadyFixture(t)
	legacy := legacyProviderResolver{pr}
	consumer := state.FindTask("consumer")
	if consumer.IsClaimable("architect", state.Tasks, legacy) {
		t.Fatal("explicit prerequisite accepted without transition projection")
	}
	if consumer.IsClaimable("architect", nil, pr) {
		t.Fatal("explicit prerequisite accepted without task state")
	}
	consumer.ProviderDependencies = nil
	if !consumer.IsClaimable("architect", state.Tasks, legacy) {
		t.Fatal("legacy task without declarations changed behavior")
	}
}

func TestProviderDependencySelectedMergedChildrenReleaseReadiness(t *testing.T) {
	state, pr := providerReadyFixture(t)
	consumer := state.FindTask("consumer")
	if !consumer.IsClaimable("architect", state.Tasks, pr) || len(models.UnmetProviderDependencies(consumer, state.Tasks, pr)) != 0 {
		t.Fatal("merged selected children failed to release consumer")
	}
	parent := "provider"
	state.Tasks[3].ParentTasks = nil
	state.Tasks[3].ParentTask = &parent
	if !consumer.IsClaimable("architect", state.Tasks, pr) {
		t.Fatal("legacy single-parent provenance rejected")
	}
}

func TestProviderDependencyStrandedClaimCannotBypassHold(t *testing.T) {
	state, pr := providerReadyFixture(t)
	consumer := state.FindTask("consumer")
	consumer.Status, _ = pr.ExecutingStatus(consumer.RolePair)
	owner, worktree, base := "departed-agent", "worktree", "base"
	expired := time.Now().Add(-time.Hour)
	consumer.AssignedTo, consumer.Worktree, consumer.BaseCommit, consumer.LeaseExpires = &owner, &worktree, &base, &expired
	if models.StrandedDoerClaimReason(state, consumer, pr, time.Now()) == "" {
		t.Fatal("fixture with satisfied prerequisites should permit stranded takeover")
	}
	state.Tasks[3].Status = models.TaskStatus("DRAFT_CODING_PLAN")
	if models.StrandedDoerClaimReason(state, consumer, pr, time.Now()) != "" || models.IsDoerClaimableByAgent(state, consumer, "architect", "new-agent", pr, time.Now()) {
		t.Fatal("stranded takeover bypassed provider hold")
	}
}

func TestProviderDependencyShapeAndClone(t *testing.T) {
	valid := models.ProviderDependency{ProviderTask: "provider", Transition: "custom-fan-out", Outputs: []int{0, 2}}
	for _, tc := range []struct {
		name string
		deps []models.ProviderDependency
		want string
	}{
		{"empty", nil, ""},
		{"valid", []models.ProviderDependency{valid}, ""},
		{"provider whitespace", []models.ProviderDependency{{ProviderTask: " provider", Transition: "custom", Outputs: []int{0}}}, "provider_task"},
		{"provider traversal", []models.ProviderDependency{{ProviderTask: "../provider", Transition: "custom", Outputs: []int{0}}}, "provider_task"},
		{"transition whitespace", []models.ProviderDependency{{ProviderTask: "provider", Transition: " custom", Outputs: []int{0}}}, "transition"},
		{"empty selection", []models.ProviderDependency{{ProviderTask: "provider", Transition: "custom"}}, "outputs"},
		{"negative index", []models.ProviderDependency{{ProviderTask: "provider", Transition: "custom", Outputs: []int{-1}}}, "outputs"},
		{"duplicate index", []models.ProviderDependency{{ProviderTask: "provider", Transition: "custom", Outputs: []int{0, 0}}}, "outputs"},
		{"duplicate declaration", []models.ProviderDependency{valid, valid}, "duplicates"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := models.ValidateProviderDependencies(tc.deps)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("validation = %v, want %q", err, tc.want)
			}
		})
	}
	cloned := models.CloneProviderDependencies([]models.ProviderDependency{valid})
	cloned[0].ProviderTask = "changed"
	cloned[0].Outputs[0] = 5
	if valid.ProviderTask != "provider" || valid.Outputs[0] != 0 {
		t.Fatal("cloned declarations alias their source")
	}
	if models.CloneProviderDependencies(nil) != nil {
		t.Fatal("cloning nil changed omitted-field semantics")
	}
	original := models.Task{ProviderDependencies: []models.ProviderDependency{valid}, Output: []models.OutputEntry{{ProviderDependencies: []models.ProviderDependency{valid}}}}
	for _, codec := range []struct {
		name      string
		marshal   func(any) ([]byte, error)
		unmarshal func([]byte, any) error
	}{{"json", json.Marshal, json.Unmarshal}, {"yaml", yaml.Marshal, yaml.Unmarshal}} {
		t.Run(codec.name, func(t *testing.T) {
			data, err := codec.marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			var restored models.Task
			if err := codec.unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored.ProviderDependencies, original.ProviderDependencies) || !reflect.DeepEqual(restored.Output, original.Output) {
				t.Fatal("task/output provider declarations lost during serialization")
			}
		})
	}
}

func TestHasProviderDependenciesIncludesAllStoredDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *models.State
		want  bool
	}{
		{"nil state", nil, false},
		{"empty state", &models.State{}, false},
		{"undeclared output", &models.State{Tasks: []models.Task{{Output: []models.OutputEntry{{}}}}}, false},
		{"terminal task declaration", &models.State{Tasks: []models.Task{{Status: models.TaskStatusMerged, ProviderDependencies: []models.ProviderDependency{{}}}}}, true},
		{"terminal output declaration", &models.State{Tasks: []models.Task{{Status: models.TaskStatusAbandoned, Output: []models.OutputEntry{{ProviderDependencies: []models.ProviderDependency{{}}}}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := models.HasProviderDependencies(tc.state); got != tc.want {
				t.Fatalf("declarations present = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestOutputSiblingProjectionPreservesKindDedup(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		{ID: "z-incumbent", Kind: "incumbent", Status: models.TaskStatusReady},
		{ID: "a-incumbent", Kind: "incumbent", Status: models.TaskStatusBlocked},
		{ID: "retiring", Kind: "incumbent", Status: models.TaskStatusReady},
		{ID: "terminal", Kind: "terminal-kind", Status: models.TaskStatusMerged},
	}}
	incumbents := models.NonTerminalTasksByKind(state, map[string]bool{"retiring": true})
	if !reflect.DeepEqual(incumbents, map[string]string{"incumbent": "a-incumbent"}) {
		t.Fatalf("canonical incumbents = %v", incumbents)
	}
	entries := []models.OutputEntry{{}, {Kind: "incumbent"}, {Kind: "new-kind"}, {Kind: "new-kind"}}
	siblings, skip, remap := models.ResolveOutputSiblings(entries, incumbents, "parent", "custom")
	if !reflect.DeepEqual(siblings, []string{"parent-custom-0", "a-incumbent", "parent-custom-2", "parent-custom-2"}) || !reflect.DeepEqual(remap, map[int]string{1: "a-incumbent", 3: "parent-custom-2"}) {
		t.Fatalf("projected siblings/remap = %v %v", siblings, remap)
	}
	wantSkip := map[int]string{
		1: `kind "incumbent" already in flight on task a-incumbent`,
		3: `kind "new-kind" emitted earlier in same output[] at sibling parent-custom-2`,
	}
	if !reflect.DeepEqual(skip, wantSkip) {
		t.Fatalf("skip reasons = %v, want %v", skip, wantSkip)
	}
}
