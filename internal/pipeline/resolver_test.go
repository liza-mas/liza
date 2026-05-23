package pipeline

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func loadPhase2Config(t *testing.T) *PipelineConfig {
	t.Helper()
	cfg, err := Load("testdata/valid-phase2-full.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg
}

func loadDecompositionRootResolverConfig(t *testing.T) *PipelineConfig {
	t.Helper()
	cfg, err := LoadFromBytes([]byte(`
pipeline:
  roles:
    planner:
      type: doer
      display-name: "Planner"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    epic-planning-main-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: DRAFT_EPIC_PLAN_MAIN
        executing: EPIC_PLANNING_MAIN
        submitted: EPIC_PLAN_MAIN_TO_REVIEW
        reviewing: REVIEWING_EPIC_PLAN_MAIN
        approved: EPIC_PLAN_MAIN_APPROVED
        rejected: EPIC_PLAN_MAIN_REJECTED
    epic-planning-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: DRAFT_EPIC_PLAN
        executing: EPIC_PLANNING
        submitted: EPIC_PLAN_TO_REVIEW
        reviewing: REVIEWING_EPIC_PLAN
        approved: EPIC_PLAN_APPROVED
        rejected: EPIC_PLAN_REJECTED
    us-writing-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: DRAFT_USER_STORIES
        executing: WRITING_USER_STORIES
        submitted: USER_STORIES_TO_REVIEW
        reviewing: REVIEWING_USER_STORIES
        approved: USER_STORIES_APPROVED
        rejected: USER_STORIES_REJECTED
    architecture-main-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: arch_ref
      states:
        initial: DRAFT_ARCHITECTURE_MAIN
        executing: ARCHITECTING_MAIN
        submitted: ARCHITECTURE_MAIN_TO_REVIEW
        reviewing: REVIEWING_ARCHITECTURE_MAIN
        approved: ARCHITECTURE_MAIN_APPROVED
        rejected: ARCHITECTURE_MAIN_REJECTED
    architecture-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: DRAFT_ARCHITECTURE
        executing: ARCHITECTING
        submitted: ARCHITECTURE_TO_REVIEW
        reviewing: REVIEWING_ARCHITECTURE
        approved: ARCHITECTURE_APPROVED
        rejected: ARCHITECTURE_REJECTED
    code-planning-main-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: DRAFT_CODING_PLAN_MAIN
        executing: CODE_PLANNING_MAIN
        submitted: CODING_PLAN_MAIN_TO_REVIEW
        reviewing: REVIEWING_CODING_PLAN_MAIN
        approved: CODING_PLAN_MAIN_APPROVED
        rejected: CODING_PLAN_MAIN_REJECTED
    code-planning-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: DRAFT_CODING_PLAN
        executing: CODE_PLANNING
        submitted: CODING_PLAN_TO_REVIEW
        reviewing: REVIEWING_CODING_PLAN
        approved: CODING_PLAN_APPROVED
        rejected: CODING_PLAN_REJECTED
    coding-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    epic-spec-subpipeline:
      steps: [epic-planning-main-pair, epic-planning-pair, us-writing-pair]
      transitions:
        - name: epic-decompose
          from: epic-planning-main-pair.approved
          to: epic-planning-pair.initial
          trigger: auto
          cardinality: per-subtask
        - name: epic-to-us
          from: epic-planning-pair.approved
          to: us-writing-pair.initial
          trigger: manual
          cardinality: per-subtask
    architecture-subpipeline:
      steps: [architecture-main-pair, architecture-pair]
      transitions:
        - name: arch-decompose
          from: architecture-main-pair.approved
          to: architecture-pair.initial
          trigger: auto
          cardinality: per-subtask
    coding-subpipeline:
      steps: [code-planning-main-pair, code-planning-pair, coding-pair]
      transitions:
        - name: code-plan-decompose
          from: code-planning-main-pair.approved
          to: code-planning-pair.initial
          trigger: auto
          cardinality: per-subtask
        - name: code-plan-to-coding
          from: code-planning-pair.approved
          to: coding-pair.initial
          trigger: manual
          cardinality: per-subtask
  entry-points: {}
`))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	return cfg
}

func TestResolver_Transition_PipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Should find pipeline-transition "us-to-coding".
	tr, err := r.Transition("us-to-coding")
	if err != nil {
		t.Fatalf("Transition(us-to-coding): unexpected error: %v", err)
	}
	if tr.Name != "us-to-coding" {
		t.Errorf("name = %q, want %q", tr.Name, "us-to-coding")
	}
	if tr.From != "epic-spec-subpipeline.us-writing-pair.approved" {
		t.Errorf("from = %q, want 3-part ref", tr.From)
	}
	if tr.To != "architecture-subpipeline.architecture-main-pair.initial" {
		t.Errorf("to = %q, want 3-part ref", tr.To)
	}
	if tr.Trigger != "manual" {
		t.Errorf("trigger = %q, want %q", tr.Trigger, "manual")
	}
	if tr.Cardinality != "many-to-one" {
		t.Errorf("cardinality = %q, want %q", tr.Cardinality, "many-to-one")
	}
}

func TestResolver_Transition_SubPipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	tr, err := r.Transition("epic-to-us")
	if err != nil {
		t.Fatalf("Transition(epic-to-us): unexpected error: %v", err)
	}
	if tr.From != "epic-planning-pair.approved" {
		t.Errorf("from = %q, want %q", tr.From, "epic-planning-pair.approved")
	}

	tr, err = r.Transition("code-plan-to-coding")
	if err != nil {
		t.Fatalf("Transition(code-plan-to-coding): unexpected error: %v", err)
	}
	if tr.From != "code-planning-pair.approved" {
		t.Errorf("from = %q, want %q", tr.From, "code-planning-pair.approved")
	}
}

func TestResolver_Transition_ArchitecturePipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	tr, err := r.Transition("architecture-to-code-plan")
	if err != nil {
		t.Fatalf("Transition(architecture-to-code-plan): unexpected error: %v", err)
	}
	if tr.From != "architecture-subpipeline.architecture-pair.approved" {
		t.Errorf("from = %q, want %q", tr.From, "architecture-subpipeline.architecture-pair.approved")
	}
	if tr.To != "coding-subpipeline.code-planning-pair.initial" {
		t.Errorf("to = %q, want %q", tr.To, "coding-subpipeline.code-planning-pair.initial")
	}
}

func TestResolver_AvailableManualTransitions_PipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// US_APPROVED should have "us-to-coding" available.
	got := r.AvailableManualTransitions("US_APPROVED", nil)
	want := []string{"us-to-coding"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(US_APPROVED, nil) = %v, want %v", got, want)
	}

	// Already executed — should return empty.
	got = r.AvailableManualTransitions("US_APPROVED", map[string]bool{"us-to-coding": true})
	if len(got) != 0 {
		t.Errorf("AvailableTransitions with executed = %v, want []", got)
	}

	// EPIC_PLAN_APPROVED should have "epic-to-us" available.
	got = r.AvailableManualTransitions("EPIC_PLAN_APPROVED", nil)
	want = []string{"epic-to-us"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(EPIC_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	// CODING_PLAN_APPROVED should have "code-plan-to-coding" available.
	got = r.AvailableManualTransitions("CODING_PLAN_APPROVED", nil)
	want = []string{"code-plan-to-coding"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(CODING_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	// ARCHITECTURE_APPROVED should have "architecture-to-code-plan" available.
	got = r.AvailableManualTransitions("ARCHITECTURE_APPROVED", nil)
	want = []string{"architecture-to-code-plan"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(ARCHITECTURE_APPROVED, nil) = %v, want %v", got, want)
	}

	// DRAFT_CODE should have no transitions.
	got = r.AvailableManualTransitions("DRAFT_CODE", nil)
	if len(got) != 0 {
		t.Errorf("AvailableTransitions(DRAFT_CODE, nil) = %v, want []", got)
	}
}

func TestResolver_AvailableManualTransitions_NoFollowUpSuppressesPipelineTransitions(t *testing.T) {
	r := NewResolver(loadPhase2Config(t), WithNoFollowUp())

	got := r.AvailableManualTransitions("US_APPROVED", nil)
	if len(got) != 0 {
		t.Errorf("AvailableTransitions(US_APPROVED, nil) = %v, want []", got)
	}

	got = r.AvailableManualTransitions("ARCHITECTURE_APPROVED", nil)
	if len(got) != 0 {
		t.Errorf("AvailableTransitions(ARCHITECTURE_APPROVED, nil) = %v, want []", got)
	}

	got = r.AvailableManualTransitions("EPIC_PLAN_APPROVED", nil)
	want := []string{"epic-to-us"}
	if !slices.Equal(got, want) {
		t.Errorf("AvailableTransitions(EPIC_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	got = r.AvailableManualTransitions("CODING_PLAN_APPROVED", nil)
	want = []string{"code-plan-to-coding"}
	if !slices.Equal(got, want) {
		t.Errorf("AvailableTransitions(CODING_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	if !r.IsPipelineTransition("us-to-coding") {
		t.Error("IsPipelineTransition(us-to-coding) = false, want true")
	}
	if r.IsPipelineTransition("epic-to-us") {
		t.Error("IsPipelineTransition(epic-to-us) = true, want false")
	}

	if _, err := r.Transition("us-to-coding"); err != nil {
		t.Fatalf("Transition(us-to-coding) should still see full topology: %v", err)
	}
}

func TestResolver_SprintTerminalStates_Phase2(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.SprintTerminalStates()

	// Pipeline-transition sources: us-writing-pair.approved (US_APPROVED),
	//   architecture-pair.approved (ARCHITECTURE_APPROVED)
	// Sub-pipeline transition sources: main planning pairs plus specialized
	//   epic-planning-pair and code-planning-pair.
	// Plus MERGED always included.
	want := []models.TaskStatus{
		"ARCHITECTURE_APPROVED",
		"ARCHITECTURE_MAIN_APPROVED",
		"CODING_PLAN_APPROVED",
		"CODING_PLAN_MAIN_APPROVED",
		"EPIC_PLAN_APPROVED",
		"EPIC_PLAN_MAIN_APPROVED",
		"MERGED",
		"US_APPROVED",
	}
	slices.Sort(want)

	if len(got) != len(want) {
		t.Fatalf("SprintTerminalStates = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("SprintTerminalStates[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolver_RolePairNames(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.RolePairNames()

	want := []string{
		"architecture-main-pair",
		"architecture-pair",
		"code-planning-main-pair",
		"code-planning-pair",
		"coding-pair",
		"epic-planning-main-pair",
		"epic-planning-pair",
		"us-writing-pair",
	}
	if len(got) != len(want) {
		t.Fatalf("RolePairNames() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("RolePairNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestResolver_TransitionSourcePairs_Phase2(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.TransitionSourcePairs()

	// Sub-pipeline transition sources: main planning pairs plus specialized
	// epic-planning-pair and code-planning-pair.
	// Pipeline-transition sources: us-writing-pair and architecture-pair.
	want := map[string]bool{
		"epic-planning-main-pair": true,
		"epic-planning-pair":      true,
		"architecture-main-pair":  true,
		"architecture-pair":       true,
		"code-planning-main-pair": true,
		"code-planning-pair":      true,
		"us-writing-pair":         true,
	}

	if len(got) != len(want) {
		t.Fatalf("TransitionSourcePairs() = %v, want %v", got, want)
	}
	for k := range want {
		if !got[k] {
			t.Errorf("TransitionSourcePairs() missing %q", k)
		}
	}
}

func TestResolver_DownstreamRolePairs(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	got, err := r.DownstreamRolePairs("architecture-pair")
	if err != nil {
		t.Fatalf("DownstreamRolePairs: %v", err)
	}
	for _, want := range []string{"code-planning-pair", "coding-pair"} {
		if !got[want] {
			t.Errorf("architecture-pair downstream missing %q: %v", want, got)
		}
	}
	if got["architecture-pair"] {
		t.Errorf("architecture-pair should not be downstream of itself: %v", got)
	}

	got, err = r.DownstreamRolePairs("us-writing-pair")
	if err != nil {
		t.Fatalf("DownstreamRolePairs: %v", err)
	}
	for _, want := range []string{"architecture-pair", "code-planning-pair", "coding-pair"} {
		if !got[want] {
			t.Errorf("us-writing-pair downstream missing %q: %v", want, got)
		}
	}
}

func TestResolver_OutputConsumerRolePairs(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	tests := []struct {
		rolePair string
		want     []string
	}{
		{rolePair: "architecture-pair", want: []string{"code-planning-pair"}},
		{rolePair: "code-planning-pair", want: []string{"coding-pair"}},
		{rolePair: "us-writing-pair", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.rolePair, func(t *testing.T) {
			got, err := r.OutputConsumerRolePairs(tt.rolePair)
			if err != nil {
				t.Fatalf("OutputConsumerRolePairs: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("OutputConsumerRolePairs(%q) = %v, want %v", tt.rolePair, got, tt.want)
			}
		})
	}
}

func TestResolver_IsDecompositionRoot(t *testing.T) {
	r := NewResolver(loadDecompositionRootResolverConfig(t))

	tests := []struct {
		rolePair string
		want     bool
	}{
		{rolePair: "epic-planning-main-pair", want: true},
		{rolePair: "architecture-main-pair", want: true},
		{rolePair: "code-planning-main-pair", want: true},
		{rolePair: "epic-planning-pair", want: false},
		{rolePair: "coding-pair", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.rolePair, func(t *testing.T) {
			got, err := r.IsDecompositionRoot(tt.rolePair)
			if err != nil {
				t.Fatalf("IsDecompositionRoot(%q): %v", tt.rolePair, err)
			}
			if got != tt.want {
				t.Fatalf("IsDecompositionRoot(%q) = %v, want %v", tt.rolePair, got, tt.want)
			}
		})
	}

	_, err := r.IsDecompositionRoot("unknown-pair")
	if err == nil {
		t.Fatal("IsDecompositionRoot(unknown-pair): expected error, got nil")
	}
}

func TestResolver_DecompositionOutputRef(t *testing.T) {
	r := NewResolver(loadDecompositionRootResolverConfig(t))

	tests := []struct {
		rolePair string
		want     string
	}{
		{rolePair: "epic-planning-main-pair", want: "plan_ref"},
		{rolePair: "architecture-main-pair", want: "arch_ref"},
		{rolePair: "code-planning-main-pair", want: "plan_ref"},
	}
	for _, tt := range tests {
		got, err := r.DecompositionOutputRef(tt.rolePair)
		if err != nil {
			t.Fatalf("DecompositionOutputRef(%q): %v", tt.rolePair, err)
		}
		if got != tt.want {
			t.Fatalf("DecompositionOutputRef(%q) = %q, want %q", tt.rolePair, got, tt.want)
		}
	}

	if _, err := r.DecompositionOutputRef("code-planning-pair"); err == nil {
		t.Fatal("DecompositionOutputRef(non-root): expected error, got nil")
	}
	if _, err := r.DecompositionOutputRef("unknown-pair"); err == nil {
		t.Fatal("DecompositionOutputRef(unknown): expected error, got nil")
	}
}

func TestResolver_DecompositionRootForTarget(t *testing.T) {
	r := NewResolver(loadDecompositionRootResolverConfig(t))

	tests := []struct {
		target    string
		wantRoot  string
		wantFound bool
	}{
		{target: "epic-planning-pair", wantRoot: "epic-planning-main-pair", wantFound: true},
		{target: "architecture-pair", wantRoot: "architecture-main-pair", wantFound: true},
		{target: "code-planning-pair", wantRoot: "code-planning-main-pair", wantFound: true},
		{target: "coding-pair", wantFound: false},
		{target: "us-writing-pair", wantFound: false},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			gotRoot, gotFound, err := r.DecompositionRootForTarget(tt.target)
			if err != nil {
				t.Fatalf("DecompositionRootForTarget(%q): %v", tt.target, err)
			}
			if gotFound != tt.wantFound || gotRoot != tt.wantRoot {
				t.Fatalf("DecompositionRootForTarget(%q) = (%q, %v), want (%q, %v)",
					tt.target, gotRoot, gotFound, tt.wantRoot, tt.wantFound)
			}
		})
	}

	_, _, err := r.DecompositionRootForTarget("unknown-pair")
	if err == nil {
		t.Fatal("DecompositionRootForTarget(unknown-pair): expected error, got nil")
	}
}

func TestResolver_DecompositionRootForTarget_DoesNotInferFromUnmarkedPair(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(`
pipeline:
  roles:
    planner:
      type: doer
      display-name: "Planner"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    unmarked-main-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: MAIN_INITIAL
        executing: MAIN_EXECUTING
        submitted: MAIN_SUBMITTED
        reviewing: MAIN_REVIEWING
        approved: MAIN_APPROVED
        rejected: MAIN_REJECTED
    specialized-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: SPECIALIZED_INITIAL
        executing: SPECIALIZED_EXECUTING
        submitted: SPECIALIZED_SUBMITTED
        reviewing: SPECIALIZED_REVIEWING
        approved: SPECIALIZED_APPROVED
        rejected: SPECIALIZED_REJECTED
  sub-pipelines:
    planning:
      steps: [unmarked-main-pair, specialized-pair]
      transitions:
        - name: unmarked-decompose
          from: unmarked-main-pair.approved
          to: specialized-pair.initial
          trigger: auto
          cardinality: per-subtask
  entry-points: {}
`))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	gotRoot, gotFound, err := r.DecompositionRootForTarget("specialized-pair")
	if err != nil {
		t.Fatalf("DecompositionRootForTarget: %v", err)
	}
	if gotFound || gotRoot != "" {
		t.Fatalf("DecompositionRootForTarget = (%q, %v), want no root", gotRoot, gotFound)
	}
}

func TestResolver_DecompositionRootForTarget_DuplicateRootAmbiguity(t *testing.T) {
	cfg, err := LoadFromBytes([]byte(`
pipeline:
  roles:
    planner:
      type: doer
      display-name: "Planner"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    root-one-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: ROOT_ONE_INITIAL
        executing: ROOT_ONE_EXECUTING
        submitted: ROOT_ONE_SUBMITTED
        reviewing: ROOT_ONE_REVIEWING
        approved: ROOT_ONE_APPROVED
        rejected: ROOT_ONE_REJECTED
    root-two-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: ROOT_TWO_INITIAL
        executing: ROOT_TWO_EXECUTING
        submitted: ROOT_TWO_SUBMITTED
        reviewing: ROOT_TWO_REVIEWING
        approved: ROOT_TWO_APPROVED
        rejected: ROOT_TWO_REJECTED
    specialized-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: SPECIALIZED_INITIAL
        executing: SPECIALIZED_EXECUTING
        submitted: SPECIALIZED_SUBMITTED
        reviewing: SPECIALIZED_REVIEWING
        approved: SPECIALIZED_APPROVED
        rejected: SPECIALIZED_REJECTED
  sub-pipelines:
    planning:
      steps: [root-one-pair, root-two-pair, specialized-pair]
      transitions:
        - name: root-one-decompose
          from: root-one-pair.approved
          to: specialized-pair.initial
          trigger: auto
          cardinality: per-subtask
        - name: root-two-decompose
          from: root-two-pair.approved
          to: specialized-pair.initial
          trigger: auto
          cardinality: per-subtask
  entry-points: {}
`))
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	_, _, err = r.DecompositionRootForTarget("specialized-pair")
	if err == nil {
		t.Fatal("DecompositionRootForTarget: expected duplicate-root error, got nil")
	}
}

func TestResolver_IsTransitionSourcePair_Phase2(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	sources := []string{"epic-planning-pair", "architecture-pair", "code-planning-pair", "us-writing-pair"}
	for _, rp := range sources {
		if !r.IsTransitionSourcePair(rp) {
			t.Errorf("IsTransitionSourcePair(%q) = false, want true", rp)
		}
	}

	// coding-pair is a terminal pair, not a transition source
	if r.IsTransitionSourcePair("coding-pair") {
		t.Error("IsTransitionSourcePair(coding-pair) = true, want false")
	}
}

func TestRoleType(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	tests := []struct {
		role     string
		wantType string
	}{
		{"coder", "doer"},
		{"code-reviewer", "reviewer"},
		{"orchestrator", "orchestrator"},
	}
	for _, tt := range tests {
		got, err := r.RoleType(tt.role)
		if err != nil {
			t.Errorf("RoleType(%q): unexpected error: %v", tt.role, err)
			continue
		}
		if got != tt.wantType {
			t.Errorf("RoleType(%q) = %q, want %q", tt.role, got, tt.wantType)
		}
	}

	// Unknown role returns error.
	_, err := r.RoleType("unknown-role")
	if err == nil {
		t.Error("RoleType(unknown-role): expected error, got nil")
	}
}

func TestDoerRoleNames(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.DoerRoleNames()
	want := []string{"architect", "code-planner", "coder", "epic-planner", "us-writer"}
	if len(got) != len(want) {
		t.Fatalf("DoerRoleNames() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("DoerRoleNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReviewerRoleNames(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.ReviewerRoleNames()
	want := []string{"architecture-reviewer", "code-plan-reviewer", "code-reviewer", "epic-plan-reviewer", "us-reviewer"}
	if len(got) != len(want) {
		t.Fatalf("ReviewerRoleNames() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ReviewerRoleNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllRoleNames(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.AllRoleNames()
	want := []string{
		"architect", "architecture-reviewer",
		"code-plan-reviewer", "code-planner", "code-reviewer", "coder",
		"epic-plan-reviewer", "epic-planner", "orchestrator", "us-reviewer", "us-writer",
	}
	if len(got) != len(want) {
		t.Fatalf("AllRoleNames() = %v (len %d), want %v (len %d)", got, len(got), want, len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("AllRoleNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestAllowedOperations(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got, err := r.AllowedOperations("coder")
	if err != nil {
		t.Fatalf("AllowedOperations(coder): %v", err)
	}
	want := []string{"write-checkpoint", "submit-for-review", "mark-blocked", "handoff", "await-verdict"}
	if len(got) != len(want) {
		t.Fatalf("AllowedOperations(coder) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("AllowedOperations(coder)[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Reviewer operations
	gotR, errR := r.AllowedOperations("code-reviewer")
	if errR != nil {
		t.Fatalf("AllowedOperations(code-reviewer): %v", errR)
	}
	wantR := []string{"submit-verdict", "await-resubmission"}
	if len(gotR) != len(wantR) {
		t.Fatalf("AllowedOperations(code-reviewer) = %v, want %v", gotR, wantR)
	}
	for i := range gotR {
		if gotR[i] != wantR[i] {
			t.Errorf("AllowedOperations(code-reviewer)[%d] = %q, want %q", i, gotR[i], wantR[i])
		}
	}

	// Unknown role returns error.
	_, err = r.AllowedOperations("unknown-role")
	if err == nil {
		t.Error("AllowedOperations(unknown-role): expected error, got nil")
	}
}

func TestRoleTimeouts(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got, err := r.RoleTimeouts("coder")
	if err != nil {
		t.Fatalf("RoleTimeouts(coder): %v", err)
	}
	if got.Execution != 2*time.Hour {
		t.Errorf("Execution = %v, want 2h", got.Execution)
	}
	if got.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", got.PollInterval)
	}
	if got.MaxWait != 30*time.Minute {
		t.Errorf("MaxWait = %v, want 30m", got.MaxWait)
	}

	// Unknown role returns error.
	_, err = r.RoleTimeouts("unknown-role")
	if err == nil {
		t.Error("RoleTimeouts(unknown-role): expected error, got nil")
	}
}

func TestResolver_TransitionTargetRolePair_PipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Pipeline-transition target role-pair.
	rp, err := r.TransitionTargetRolePair("us-to-coding")
	if err != nil {
		t.Fatalf("TransitionTargetRolePair(us-to-coding): %v", err)
	}
	if rp != "architecture-main-pair" {
		t.Errorf("TransitionTargetRolePair = %q, want %q", rp, "architecture-main-pair")
	}

	// Sub-pipeline transition target role-pair should still work.
	rp, err = r.TransitionTargetRolePair("epic-to-us")
	if err != nil {
		t.Fatalf("TransitionTargetRolePair(epic-to-us): %v", err)
	}
	if rp != "us-writing-pair" {
		t.Errorf("TransitionTargetRolePair = %q, want %q", rp, "us-writing-pair")
	}
}

func TestResolver_RoleDisplayName(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Known role returns display-name.
	if got := r.RoleDisplayName("coder"); got != "Coder" {
		t.Errorf("RoleDisplayName(coder) = %q, want %q", got, "Coder")
	}
	if got := r.RoleDisplayName("code-reviewer"); got != "Code Reviewer" {
		t.Errorf("RoleDisplayName(code-reviewer) = %q, want %q", got, "Code Reviewer")
	}
	if got := r.RoleDisplayName("orchestrator"); got != "Orchestrator" {
		t.Errorf("RoleDisplayName(orchestrator) = %q, want %q", got, "Orchestrator")
	}

	// Unknown role returns key itself.
	if got := r.RoleDisplayName("nonexistent"); got != "nonexistent" {
		t.Errorf("RoleDisplayName(nonexistent) = %q, want %q", got, "nonexistent")
	}
}

func TestResolver_ContextSections(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Coder should have 16 context-sections.
	got, err := r.ContextSections("coder")
	if err != nil {
		t.Fatalf("ContextSections(coder): %v", err)
	}
	want := []string{
		"assigned-task", "worktree-rules", "collective-plan-scoping", "handoff-resume",
		"integration-fix", "prior-rejection", "prior-attempt", "doer-state-transitions", "coder-tools",
		"anomaly-logging", "blocking-protocol", "commit-workflow", "implementation-phase",
		"submission-phase", "mandatory-docs", "skills-affinity",
	}
	if len(got) != len(want) {
		t.Fatalf("ContextSections(coder) len = %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("ContextSections(coder)[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Code-reviewer should have 13 context-sections.
	got, err = r.ContextSections("code-reviewer")
	if err != nil {
		t.Fatalf("ContextSections(code-reviewer): %v", err)
	}
	if len(got) != 13 {
		t.Fatalf("ContextSections(code-reviewer) len = %d, want 13\ngot: %v", len(got), got)
	}
	if got[0] != "review-task" {
		t.Errorf("ContextSections(code-reviewer)[0] = %q, want %q", got[0], "review-task")
	}
	if got[len(got)-2] != "mandatory-docs" {
		t.Errorf("ContextSections(code-reviewer)[-2] = %q, want %q", got[len(got)-2], "mandatory-docs")
	}
	if got[len(got)-1] != "skills-affinity" {
		t.Errorf("ContextSections(code-reviewer)[-1] = %q, want %q", got[len(got)-1], "skills-affinity")
	}

	// Orchestrator should have 4 context-sections.
	got, err = r.ContextSections("orchestrator")
	if err != nil {
		t.Fatalf("ContextSections(orchestrator): %v", err)
	}
	wantOrch := []string{"orchestrator-dashboard", "wake-instructions", "mandatory-docs", "skills-affinity"}
	if len(got) != len(wantOrch) {
		t.Fatalf("ContextSections(orchestrator) len = %d, want %d\ngot: %v", len(got), len(wantOrch), got)
	}
	for i := range got {
		if got[i] != wantOrch[i] {
			t.Errorf("ContextSections(orchestrator)[%d] = %q, want %q", i, got[i], wantOrch[i])
		}
	}

	// Unknown role returns error.
	_, err = r.ContextSections("unknown-role")
	if err == nil {
		t.Error("ContextSections(unknown-role): expected error, got nil")
	}
}

func TestResolver_Skills(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Coder skills.
	got, err := r.Skills("coder")
	if err != nil {
		t.Fatalf("Skills(coder): %v", err)
	}
	want := []string{"debugging", "testing", "clean-code"}
	if len(got) != len(want) {
		t.Fatalf("Skills(coder) = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Skills(coder)[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// Code-reviewer skills.
	got, err = r.Skills("code-reviewer")
	if err != nil {
		t.Fatalf("Skills(code-reviewer): %v", err)
	}
	wantReviewer := []string{"code-review", "systemic-thinking", "software-architecture-review"}
	if len(got) != len(wantReviewer) {
		t.Fatalf("Skills(code-reviewer) = %v, want %v", got, wantReviewer)
	}
	for i := range got {
		if got[i] != wantReviewer[i] {
			t.Errorf("Skills(code-reviewer)[%d] = %q, want %q", i, got[i], wantReviewer[i])
		}
	}

	// Orchestrator skills.
	got, err = r.Skills("orchestrator")
	if err != nil {
		t.Fatalf("Skills(orchestrator): %v", err)
	}
	if len(got) != 1 || got[0] != "systemic-thinking" {
		t.Errorf("Skills(orchestrator) = %v, want [systemic-thinking]", got)
	}

	// Unknown role returns error.
	_, err = r.Skills("unknown-role")
	if err == nil {
		t.Error("Skills(unknown-role): expected error, got nil")
	}
}

func TestResolver_MandatoryDocs(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Coder has empty mandatory-docs.
	got, err := r.MandatoryDocs("coder")
	if err != nil {
		t.Fatalf("MandatoryDocs(coder): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MandatoryDocs(coder) = %v, want []", got)
	}

	// Code-reviewer has empty mandatory-docs.
	got, err = r.MandatoryDocs("code-reviewer")
	if err != nil {
		t.Fatalf("MandatoryDocs(code-reviewer): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MandatoryDocs(code-reviewer) = %v, want []", got)
	}

	// Orchestrator has empty mandatory-docs.
	got, err = r.MandatoryDocs("orchestrator")
	if err != nil {
		t.Fatalf("MandatoryDocs(orchestrator): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("MandatoryDocs(orchestrator) = %v, want []", got)
	}

	// Unknown role returns error.
	_, err = r.MandatoryDocs("unknown-role")
	if err == nil {
		t.Error("MandatoryDocs(unknown-role): expected error, got nil")
	}
}

func TestEffectiveQuorum(t *testing.T) {
	// Build a config with review-policy including overrides.
	yamlData := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
          provider-diversity: preferred
        architecture-impact:
          quorum: 2
          provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	tests := []struct {
		name       string
		rolePair   string
		impact     string
		wantQuorum int
	}{
		{"empty_impact_returns_base", "coding-pair", "", 1},
		{"standard_returns_base", "coding-pair", "standard", 1},
		{"significant_returns_override", "coding-pair", "significant", 2},
		{"architecture_returns_override", "coding-pair", "architecture", 2},
		{"unknown_impact_returns_base", "coding-pair", "unknown", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.EffectiveQuorum(tt.rolePair, tt.impact)
			if err != nil {
				t.Fatalf("EffectiveQuorum(%q, %q): %v", tt.rolePair, tt.impact, err)
			}
			if got != tt.wantQuorum {
				t.Errorf("EffectiveQuorum(%q, %q) = %d, want %d", tt.rolePair, tt.impact, got, tt.wantQuorum)
			}
		})
	}

	// Unknown role-pair returns error.
	_, err = r.EffectiveQuorum("nonexistent", "standard")
	if err == nil {
		t.Error("EffectiveQuorum(nonexistent, standard): expected error, got nil")
	}
}

func TestReviewPolicy(t *testing.T) {
	// Config with review-policy.
	yamlData := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
          provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	rp, err := r.ReviewPolicy("coding-pair")
	if err != nil {
		t.Fatalf("ReviewPolicy(coding-pair): %v", err)
	}
	if rp == nil {
		t.Fatal("ReviewPolicy returned nil")
	}
	if rp.Quorum != 1 {
		t.Errorf("Quorum = %d, want 1", rp.Quorum)
	}
	if rp.SignificantChange == nil {
		t.Fatal("SignificantChange is nil")
	}
	if rp.SignificantChange.Quorum != 2 {
		t.Errorf("SignificantChange.Quorum = %d, want 2", rp.SignificantChange.Quorum)
	}
	if rp.SignificantChange.ProviderDiversity != "preferred" {
		t.Errorf("SignificantChange.ProviderDiversity = %q, want %q", rp.SignificantChange.ProviderDiversity, "preferred")
	}
	if rp.ArchitectureImpact != nil {
		t.Errorf("ArchitectureImpact should be nil, got %+v", rp.ArchitectureImpact)
	}

	// Role-pair without review-policy returns nil, no error.
	yamlNoPolicy := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg2, err := LoadFromBytes(yamlNoPolicy)
	if err != nil {
		t.Fatalf("LoadFromBytes (no policy): %v", err)
	}
	r2 := NewResolver(cfg2)

	rp2, err := r2.ReviewPolicy("coding-pair")
	if err != nil {
		t.Fatalf("ReviewPolicy(coding-pair, no policy): %v", err)
	}
	if rp2 != nil {
		t.Errorf("expected nil ReviewPolicy when not configured, got %+v", rp2)
	}

	// EffectiveQuorum with no policy defaults to 1.
	q, err := r2.EffectiveQuorum("coding-pair", "architecture")
	if err != nil {
		t.Fatalf("EffectiveQuorum with no policy: %v", err)
	}
	if q != 1 {
		t.Errorf("EffectiveQuorum with no policy = %d, want 1", q)
	}

	// Unknown role-pair returns error.
	_, err = r2.ReviewPolicy("nonexistent")
	if err == nil {
		t.Error("ReviewPolicy(nonexistent): expected error, got nil")
	}
}

// TestMaxInstances_OrchestratorCoercedToOne verifies the spec invariant that
// orchestrator roles always return max-instances=1 regardless of YAML value.
// Regression test for the misconfiguration case where YAML sets max-instances: 2.
func TestMaxInstances_OrchestratorCoercedToOne(t *testing.T) {
	// Build a config where orchestrator explicitly sets max-instances: 2.
	yamlData := []byte(`
pipeline:
  roles:
    orchestrator:
      type: orchestrator
      max-instances: 2
      display-name: "Orchestrator"
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	// Despite YAML saying max-instances: 2, resolver must coerce to 1.
	got, err := r.MaxInstances("orchestrator")
	if err != nil {
		t.Fatalf("MaxInstances(orchestrator): %v", err)
	}
	if got != 1 {
		t.Errorf("MaxInstances(orchestrator) = %d, want 1 (spec invariant: orchestrator singularity)", got)
	}

	// Non-orchestrator roles should honor their YAML value.
	got, err = r.MaxInstances("coder")
	if err != nil {
		t.Fatalf("MaxInstances(coder): %v", err)
	}
	if got != 0 {
		t.Errorf("MaxInstances(coder) = %d, want 0 (unset = unlimited)", got)
	}
}

// TestResolver_WorktreeRulesBeforeReadBearingBlocks ensures worktree-rules is rendered
// before any block that can trigger file reads, preventing agents from reading files
// with the wrong path prefix before seeing worktree grounding instructions.
func TestResolver_WorktreeRulesBeforeReadBearingBlocks(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	readBearingBlocks := []string{
		"collective-plan-scoping",
		"integration-fix",
		"review-instructions",
	}

	for _, role := range []string{
		"coder", "code-reviewer",
		"code-planner", "code-plan-reviewer",
		"epic-planner", "epic-plan-reviewer",
		"us-writer", "us-reviewer",
	} {
		sections, err := r.ContextSections(role)
		if err != nil {
			t.Fatalf("ContextSections(%s): %v", role, err)
		}

		wtIdx := -1
		for i, s := range sections {
			if s == "worktree-rules" {
				wtIdx = i
				break
			}
		}
		if wtIdx == -1 {
			t.Errorf("%s: worktree-rules not found in context-sections", role)
			continue
		}

		for _, block := range readBearingBlocks {
			for i, s := range sections {
				if s == block && i < wtIdx {
					t.Errorf("%s: read-bearing block %q at index %d precedes worktree-rules at index %d",
						role, block, i, wtIdx)
				}
			}
		}
	}
}

// TestResolver_FixtureMatchesEmbeddedPipeline ensures the Phase 2 test fixture
// stays in sync with the production embedded pipeline for context-sections ordering.
func TestResolver_FixtureMatchesEmbeddedPipeline(t *testing.T) {
	fixture := NewResolver(loadPhase2Config(t))

	embeddedData, err := os.ReadFile("../embedded/pipeline.yaml")
	if err != nil {
		t.Fatalf("failed to read embedded pipeline.yaml: %v", err)
	}
	embeddedCfg, err := LoadFromBytes(embeddedData)
	if err != nil {
		t.Fatalf("LoadFromBytes(embedded): %v", err)
	}
	embedded := NewResolver(embeddedCfg)

	for _, role := range []string{
		"orchestrator",
		"coder", "code-reviewer",
		"code-planner", "code-plan-reviewer",
		"epic-planner", "epic-plan-reviewer",
		"us-writer", "us-reviewer",
		"architect", "architecture-reviewer",
	} {
		fixtureSections, err := fixture.ContextSections(role)
		if err != nil {
			t.Fatalf("fixture.ContextSections(%s): %v", role, err)
		}
		embeddedSections, err := embedded.ContextSections(role)
		if err != nil {
			t.Fatalf("embedded.ContextSections(%s): %v", role, err)
		}

		if !slices.Equal(fixtureSections, embeddedSections) {
			t.Errorf("%s: context-sections diverge between fixture and embedded pipeline\n  fixture:  %v\n  embedded: %v",
				role, fixtureSections, embeddedSections)
		}
	}
}

func TestProviderDiversity_BaseLevelFallthrough(t *testing.T) {
	yamlData := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	tests := []struct {
		name   string
		impact string
		want   string
	}{
		{"standard returns base-level", "", "preferred"},
		{"standard explicit returns base-level", "standard", "preferred"},
		{"significant falls through to base-level", "significant", "preferred"},
		{"architecture falls through to base-level", "architecture", "preferred"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.ProviderDiversity("coding-pair", tt.impact)
			if err != nil {
				t.Fatalf("ProviderDiversity(%q): %v", tt.impact, err)
			}
			if got != tt.want {
				t.Errorf("ProviderDiversity(%q) = %q, want %q", tt.impact, got, tt.want)
			}
		})
	}
}

func TestProviderDiversity_OverrideTakesPrecedence(t *testing.T) {
	// Base-level has no diversity; significant-change override has "preferred".
	yamlData := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
          provider-diversity: preferred
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	// Standard impact: no diversity (base has none).
	got, err := r.ProviderDiversity("coding-pair", "")
	if err != nil {
		t.Fatalf("ProviderDiversity(standard): %v", err)
	}
	if got != "" {
		t.Errorf("ProviderDiversity(standard) = %q, want empty", got)
	}

	// Significant: override returns "preferred".
	got, err = r.ProviderDiversity("coding-pair", "significant")
	if err != nil {
		t.Fatalf("ProviderDiversity(significant): %v", err)
	}
	if got != "preferred" {
		t.Errorf("ProviderDiversity(significant) = %q, want %q", got, "preferred")
	}
}

func TestProviderDiversity_OverrideOmittedFallsToBase(t *testing.T) {
	// Base has "preferred"; significant-change overrides only quorum (no provider-diversity).
	// ProviderDiversity("significant") should fall through to the base-level "preferred".
	yamlData := []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        provider-diversity: preferred
        significant-change:
          quorum: 2
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_TO_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
  sub-pipelines:
    coding-subpipeline:
      steps:
        - coding-pair
  entry-points:
    default: coding-subpipeline.coding-pair
`)
	cfg, err := LoadFromBytes(yamlData)
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	r := NewResolver(cfg)

	got, err := r.ProviderDiversity("coding-pair", "significant")
	if err != nil {
		t.Fatalf("ProviderDiversity(significant): %v", err)
	}
	if got != "preferred" {
		t.Errorf("ProviderDiversity(significant) = %q, want %q (fall through to base)", got, "preferred")
	}
}

func loadCleanConfig(t *testing.T) *PipelineConfig {
	t.Helper()
	cfg, err := Load("testdata/valid-with-clean.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg
}

func TestResolver_CleanStatus(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))

	t.Run("declared clean state", func(t *testing.T) {
		got, err := r.CleanStatus("integration-pair")
		if err != nil {
			t.Fatalf("CleanStatus(integration-pair): unexpected error: %v", err)
		}
		if got != "INTEGRATION_ANALYSIS_CLEAN" {
			t.Errorf("CleanStatus(integration-pair) = %q, want %q", got, "INTEGRATION_ANALYSIS_CLEAN")
		}
	})

	t.Run("no clean state declared", func(t *testing.T) {
		_, err := r.CleanStatus("coding-pair")
		if err == nil {
			t.Fatal("CleanStatus(coding-pair): expected error for role-pair without clean state")
		}
	})

	t.Run("unknown role-pair", func(t *testing.T) {
		_, err := r.CleanStatus("nonexistent-pair")
		if err == nil {
			t.Fatal("CleanStatus(nonexistent-pair): expected error for unknown role-pair")
		}
	})
}

func TestResolver_TransitionMap_CleanTerminal(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))
	tm := r.TransitionMap()

	clean := models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN")
	successors, ok := tm[clean]
	if !ok {
		t.Fatal("TransitionMap missing INTEGRATION_ANALYSIS_CLEAN")
	}
	if len(successors) != 0 {
		t.Errorf("TransitionMap[INTEGRATION_ANALYSIS_CLEAN] = %v, want empty (terminal)", successors)
	}
}

func TestResolver_AllDeclaredStates_IncludesClean(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))
	states := r.AllDeclaredStates()

	clean := models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN")
	if !slices.Contains(states, clean) {
		t.Errorf("AllDeclaredStates() missing INTEGRATION_ANALYSIS_CLEAN, got: %v", states)
	}
}

func TestResolver_SprintTerminalStates_IncludesClean(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))
	states := r.SprintTerminalStates()

	clean := models.TaskStatus("INTEGRATION_ANALYSIS_CLEAN")
	if !slices.Contains(states, clean) {
		t.Errorf("SprintTerminalStates() missing INTEGRATION_ANALYSIS_CLEAN, got: %v", states)
	}

	// Also verify approved state of transition source is included
	approved := models.TaskStatus("INTEGRATION_ANALYSIS_APPROVED")
	if !slices.Contains(states, approved) {
		t.Errorf("SprintTerminalStates() missing INTEGRATION_ANALYSIS_APPROVED (transition source), got: %v", states)
	}

	// MERGED always included
	if !slices.Contains(states, models.TaskStatusMerged) {
		t.Errorf("SprintTerminalStates() missing MERGED, got: %v", states)
	}
}

func TestResolver_AvailableAutoTransitions(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))

	t.Run("returns auto transitions for matching status", func(t *testing.T) {
		got := r.AvailableAutoTransitions("INTEGRATION_ANALYSIS_APPROVED", nil)
		want := []string{"integration-to-fix"}
		if len(got) != len(want) || got[0] != want[0] {
			t.Errorf("AvailableAutoTransitions(INTEGRATION_ANALYSIS_APPROVED, nil) = %v, want %v", got, want)
		}
	})

	t.Run("excludes already-executed transitions", func(t *testing.T) {
		got := r.AvailableAutoTransitions("INTEGRATION_ANALYSIS_APPROVED", map[string]bool{"integration-to-fix": true})
		if len(got) != 0 {
			t.Errorf("AvailableAutoTransitions with executed = %v, want []", got)
		}
	})

	t.Run("manual-only filter preserved", func(t *testing.T) {
		got := r.AvailableManualTransitions("INTEGRATION_ANALYSIS_APPROVED", nil)
		if len(got) != 0 {
			t.Errorf("AvailableTransitions(INTEGRATION_ANALYSIS_APPROVED, nil) = %v, want [] (no manual transitions)", got)
		}
	})
}

func TestResolver_AllTransitions(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	all := r.AllTransitions()

	// The phase2 fixture has 5 sub-pipeline transitions + 2 pipeline-transitions = 7 total.
	if len(all) != 7 {
		t.Fatalf("AllTransitions() returned %d transitions, want 7", len(all))
	}

	for i, td := range all {
		if td.Name == "" {
			t.Errorf("AllTransitions()[%d]: Name is empty", i)
		}
		if td.Cardinality == "" {
			t.Errorf("AllTransitions()[%d] (%s): Cardinality is empty", i, td.Name)
		}
	}
}

func TestResolver_TransitionSourceRolePair(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Pipeline-transition (3-part from ref): us-to-coding → us-writing-pair
	got, err := r.TransitionSourceRolePair("us-to-coding")
	if err != nil {
		t.Fatalf("TransitionSourceRolePair(us-to-coding): unexpected error: %v", err)
	}
	if got != "us-writing-pair" {
		t.Errorf("TransitionSourceRolePair(us-to-coding) = %q, want %q", got, "us-writing-pair")
	}

	// Sub-pipeline transition (2-part from ref): code-plan-to-coding → code-planning-pair
	got, err = r.TransitionSourceRolePair("code-plan-to-coding")
	if err != nil {
		t.Fatalf("TransitionSourceRolePair(code-plan-to-coding): unexpected error: %v", err)
	}
	if got != "code-planning-pair" {
		t.Errorf("TransitionSourceRolePair(code-plan-to-coding) = %q, want %q", got, "code-planning-pair")
	}

	// Unknown transition → error
	_, err = r.TransitionSourceRolePair("nonexistent")
	if err == nil {
		t.Error("TransitionSourceRolePair(nonexistent): expected error, got nil")
	}
}

func TestResolver_resolvePhase_Clean(t *testing.T) {
	r := NewResolver(loadCleanConfig(t))

	got := r.resolvePhase("integration-pair", "clean")
	if got != "INTEGRATION_ANALYSIS_CLEAN" {
		t.Errorf("resolvePhase(integration-pair, clean) = %q, want %q", got, "INTEGRATION_ANALYSIS_CLEAN")
	}

	// Role-pair without clean state returns empty string
	got = r.resolvePhase("coding-pair", "clean")
	if got != "" {
		t.Errorf("resolvePhase(coding-pair, clean) = %q, want empty", got)
	}
}
