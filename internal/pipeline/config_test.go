package pipeline

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestLoad_ValidConfig(t *testing.T) {
	cfg, err := Load("testdata/valid-coding-subpipeline.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify role-pairs parsed.
	if len(cfg.Pipeline.RolePairs) != 3 {
		t.Fatalf("expected 3 role-pairs, got %d", len(cfg.Pipeline.RolePairs))
	}
	if _, ok := cfg.Pipeline.RolePairs["coding-pair"]; !ok {
		t.Error("missing role-pair coding-pair")
	}
	if _, ok := cfg.Pipeline.RolePairs["code-planning-pair"]; !ok {
		t.Error("missing role-pair code-planning-pair")
	}
	if _, ok := cfg.Pipeline.RolePairs["architecture-pair"]; !ok {
		t.Error("missing role-pair architecture-pair")
	}

	// Verify roles parsed.
	if len(cfg.Pipeline.Roles) != 6 {
		t.Fatalf("expected 6 roles, got %d", len(cfg.Pipeline.Roles))
	}

	// Verify sub-pipelines parsed.
	sp, ok := cfg.Pipeline.SubPipelines["coding-subpipeline"]
	if !ok {
		t.Fatal("missing sub-pipeline coding-subpipeline")
	}
	if len(sp.Steps) != 2 {
		t.Fatalf("expected 2 coding steps, got %d", len(sp.Steps))
	}
	if len(sp.Transitions) != 2 {
		t.Fatalf("expected 2 coding transitions, got %d", len(sp.Transitions))
	}
	architectureSP, ok := cfg.Pipeline.SubPipelines["architecture-subpipeline"]
	if !ok {
		t.Fatal("missing sub-pipeline architecture-subpipeline")
	}
	if !slices.Equal(architectureSP.Steps, []string{"architecture-pair"}) {
		t.Fatalf("architecture steps = %v, want [architecture-pair]", architectureSP.Steps)
	}

	// Verify entry-points.
	if ep, ok := cfg.Pipeline.EntryPoints["functional-spec"]; !ok {
		t.Error("missing entry-point functional-spec")
	} else if ep != "architecture-subpipeline.architecture-pair" {
		t.Errorf("functional-spec entry-point value = %q, want %q", ep, "architecture-subpipeline.architecture-pair")
	}
	if ep, ok := cfg.Pipeline.EntryPoints["detailed-spec"]; !ok {
		t.Error("missing entry-point detailed-spec")
	} else if ep != "architecture-subpipeline.architecture-pair" {
		t.Errorf("detailed-spec entry-point value = %q, want %q", ep, "architecture-subpipeline.architecture-pair")
	}
	if ep, ok := cfg.Pipeline.EntryPoints["technical-spec"]; !ok {
		t.Error("missing entry-point technical-spec")
	} else if ep != "coding-subpipeline.code-planning-pair" {
		t.Errorf("technical-spec entry-point value = %q, want %q", ep, "coding-subpipeline.code-planning-pair")
	}
}

func TestLoad_MissingStateField(t *testing.T) {
	yaml := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    pair-a:
      doer: coder
      reviewer: reviewer
      states:
        initial: S1
        executing: ""
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for empty state field")
	}
	assertContains(t, err.Error(), "executing state is empty")
}

func TestLoad_DuplicateStateNames(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: DRAFT
        executing: WORKING
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: OTHER
        executing: WORKING
        submitted: SUB2
        reviewing: REV2
        approved: APP2
        rejected: REJ2
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate state name")
	}
	assertContains(t, err.Error(), "duplicate state name")
	assertContains(t, err.Error(), "WORKING")
}

func TestLoad_DuplicateTransitionNames(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_A
        executing: WORKING_A
        submitted: SUBMITTED_A
        reviewing: REVIEWING_A
        approved: APPROVED_A
        rejected: REJECTED_A
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_B
        executing: WORKING_B
        submitted: SUBMITTED_B
        reviewing: REVIEWING_B
        approved: APPROVED_B
        rejected: REJECTED_B
  sub-pipelines:
    sp1:
      steps: [pair-a, pair-b]
      transitions:
        - name: advance
          from: pair-a.approved
          to: pair-b.initial
          trigger: manual
          cardinality: per-subtask
        - name: advance
          from: pair-b.approved
          to: pair-a.initial
          trigger: manual
          cardinality: per-subtask
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate transition name")
	}
	assertContains(t, err.Error(), "duplicate transition name")
	assertContains(t, err.Error(), "advance")
}

func TestLoad_DuplicateTransitionNames_AcrossSubPipelines(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_A
        executing: WORKING_A
        submitted: SUBMITTED_A
        reviewing: REVIEWING_A
        approved: APPROVED_A
        rejected: REJECTED_A
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_B
        executing: WORKING_B
        submitted: SUBMITTED_B
        reviewing: REVIEWING_B
        approved: APPROVED_B
        rejected: REJECTED_B
    pair-c:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_C
        executing: WORKING_C
        submitted: SUBMITTED_C
        reviewing: REVIEWING_C
        approved: APPROVED_C
        rejected: REJECTED_C
    pair-d:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_D
        executing: WORKING_D
        submitted: SUBMITTED_D
        reviewing: REVIEWING_D
        approved: APPROVED_D
        rejected: REJECTED_D
  sub-pipelines:
    sp1:
      steps: [pair-a, pair-b]
      transitions:
        - name: advance
          from: pair-a.approved
          to: pair-b.initial
          trigger: manual
          cardinality: per-subtask
    sp2:
      steps: [pair-c, pair-d]
      transitions:
        - name: advance
          from: pair-c.approved
          to: pair-d.initial
          trigger: manual
          cardinality: per-subtask
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate transition name across sub-pipelines")
	}
	assertContains(t, err.Error(), "duplicate transition name")
	assertContains(t, err.Error(), "advance")
}

func TestLoad_InvalidTransitionReference(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines:
    sp:
      steps: [pair-a]
      transitions:
        - name: bad-transition
          from: nonexistent-pair.approved
          to: pair-a.initial
          trigger: manual
          cardinality: per-subtask
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for invalid transition reference")
	}
	assertContains(t, err.Error(), "nonexistent-pair")
}

func TestLoad_UnknownCardinality(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp:
      steps: [pair-a, pair-b]
      transitions:
        - name: t1
          from: pair-a.approved
          to: pair-b.initial
          trigger: manual
          cardinality: many-to-many
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for unknown cardinality")
	}
	assertContains(t, err.Error(), "cardinality")
	assertContains(t, err.Error(), "many-to-many")
}

func TestLoad_UnknownTrigger(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp:
      steps: [pair-a, pair-b]
      transitions:
        - name: t1
          from: pair-a.approved
          to: pair-b.initial
          trigger: cron
          cardinality: per-subtask
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for unknown trigger")
	}
	assertContains(t, err.Error(), "trigger")
	assertContains(t, err.Error(), "cron")
}

// Blocker fix 1: role-pair cannot appear in multiple sub-pipelines.
func TestLoad_RolePairInMultipleSubPipelines(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    shared-pair:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    other-pair:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [shared-pair]
      transitions: []
    sp2:
      steps: [shared-pair, other-pair]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err != nil {
		t.Fatalf("expected no error for role-pair in multiple sub-pipelines, got: %v", err)
	}
}

// Blocker fix 2: entry-point role-pair must be a step of the referenced sub-pipeline.
func TestLoad_EntryPointRolePairNotInSubPipeline(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  entry-points:
    bad-entry: sp1.pair-b
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for entry-point referencing role-pair not in sub-pipeline")
	}
	assertContains(t, err.Error(), "pair-b")
	assertContains(t, err.Error(), "not a step of sub-pipeline")
}

func TestLoad_EntryPointNonexistentSubPipeline(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
  entry-points:
    bad-entry: nonexistent.pair-a
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for entry-point referencing nonexistent sub-pipeline")
	}
	assertContains(t, err.Error(), "nonexistent")
	assertContains(t, err.Error(), "not found")
}

func TestLoad_DoerNotInRoles(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: reviewer
      display-name: "A"
  role-pairs:
    pair-a:
      doer: unknown
      reviewer: a
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for doer not in roles")
	}
	assertContains(t, err.Error(), "doer")
	assertContains(t, err.Error(), "unknown")
}

func TestLoad_TransitionFromPairNotInSubPipelineSteps(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
    pair-c:
      doer: a
      reviewer: b
      states:
        initial: U1
        executing: U2
        submitted: U3
        reviewing: U4
        approved: U5
        rejected: U6
  sub-pipelines:
    sp1:
      steps: [pair-a, pair-b]
      transitions:
        - name: bad
          from: pair-c.approved
          to: pair-b.initial
          trigger: manual
          cardinality: per-subtask
    sp2:
      steps: [pair-c]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for transition from role-pair not in sub-pipeline steps")
	}
	assertContains(t, err.Error(), "pair-c")
	assertContains(t, err.Error(), "not a step")
}

func TestLoadFrozen_NoFile(t *testing.T) {
	_, err := LoadFrozen(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing pipeline config")
	}
}

func TestLoadFrozen_ValidFile(t *testing.T) {
	dir := t.TempDir()
	lizaDir := filepath.Join(dir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile("testdata/valid-coding-subpipeline.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrozen(dir)
	if err != nil {
		t.Fatalf("LoadFrozen failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Pipeline.RolePairs) != 3 {
		t.Errorf("expected 3 role-pairs, got %d", len(cfg.Pipeline.RolePairs))
	}
}

// --- Resolver tests ---

func loadTestConfig(t *testing.T) *PipelineConfig {
	t.Helper()
	cfg, err := Load("testdata/valid-coding-subpipeline.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	return cfg
}

func TestResolver_InitialStatus(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	got, err := r.InitialStatus("coding-pair")
	if err != nil {
		t.Fatal(err)
	}
	if got != "DRAFT_CODE" {
		t.Errorf("InitialStatus(coding-pair) = %q, want %q", got, "DRAFT_CODE")
	}
}

func TestResolver_ExecutingStatus(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	got, err := r.ExecutingStatus("code-planning-pair")
	if err != nil {
		t.Fatal(err)
	}
	if got != "CODE_PLANNING" {
		t.Errorf("ExecutingStatus(code-planning-pair) = %q, want %q", got, "CODE_PLANNING")
	}
}

func TestResolver_AllStatusMethods(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	tests := []struct {
		method   string
		rolePair string
		want     models.TaskStatus
	}{
		{"Initial", "coding-pair", "DRAFT_CODE"},
		{"Executing", "coding-pair", "IMPLEMENTING_CODE"},
		{"Submitted", "coding-pair", "CODE_TO_REVIEW"},
		{"Reviewing", "coding-pair", "REVIEWING_CODE"},
		{"Approved", "coding-pair", "CODE_APPROVED"},
		{"Rejected", "coding-pair", "CODE_REJECTED"},
		{"Initial", "code-planning-pair", "DRAFT_CODING_PLAN"},
		{"Executing", "code-planning-pair", "CODE_PLANNING"},
		{"Submitted", "code-planning-pair", "CODING_PLAN_TO_REVIEW"},
		{"Reviewing", "code-planning-pair", "REVIEWING_CODING_PLAN"},
		{"Approved", "code-planning-pair", "CODING_PLAN_APPROVED"},
		{"Rejected", "code-planning-pair", "CODING_PLAN_REJECTED"},
	}
	for _, tt := range tests {
		var got models.TaskStatus
		var err error
		switch tt.method {
		case "Initial":
			got, err = r.InitialStatus(tt.rolePair)
		case "Executing":
			got, err = r.ExecutingStatus(tt.rolePair)
		case "Submitted":
			got, err = r.SubmittedStatus(tt.rolePair)
		case "Reviewing":
			got, err = r.ReviewingStatus(tt.rolePair)
		case "Approved":
			got, err = r.ApprovedStatus(tt.rolePair)
		case "Rejected":
			got, err = r.RejectedStatus(tt.rolePair)
		}
		if err != nil {
			t.Errorf("%s(%q): unexpected error: %v", tt.method, tt.rolePair, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s(%q) = %q, want %q", tt.method, tt.rolePair, got, tt.want)
		}
	}
}

func TestResolver_StatusUnknownRolePair(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	_, err := r.InitialStatus("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown role-pair")
	}
}

func TestResolver_TransitionMap(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	tm := r.TransitionMap()

	// Check code-planning-pair transitions.
	assertTransitions(t, tm, "DRAFT_CODING_PLAN", []string{"CODE_PLANNING"})
	assertTransitions(t, tm, "CODE_PLANNING", []string{"CODING_PLAN_TO_REVIEW"})
	assertTransitions(t, tm, "CODING_PLAN_TO_REVIEW", []string{"REVIEWING_CODING_PLAN"})
	assertTransitions(t, tm, "REVIEWING_CODING_PLAN", []string{"CODING_PLAN_APPROVED", "CODING_PLAN_REJECTED"})
	assertTransitions(t, tm, "CODING_PLAN_REJECTED", []string{"DRAFT_CODING_PLAN"})
	assertTransitions(t, tm, "CODING_PLAN_APPROVED", []string{})

	// Check coding-pair transitions (base + quorum states).
	assertTransitions(t, tm, "DRAFT_CODE", []string{"IMPLEMENTING_CODE"})
	assertTransitions(t, tm, "IMPLEMENTING_CODE", []string{"CODE_TO_REVIEW"})
	assertTransitions(t, tm, "CODE_TO_REVIEW", []string{"REVIEWING_CODE"})
	assertTransitions(t, tm, "REVIEWING_CODE", []string{"CODE_APPROVED", "CODE_PARTIALLY_APPROVED", "CODE_REJECTED"})
	assertTransitions(t, tm, "CODE_REJECTED", []string{"DRAFT_CODE"})
	assertTransitions(t, tm, "CODE_APPROVED", []string{})

	// Quorum state transitions.
	assertTransitions(t, tm, "CODE_PARTIALLY_APPROVED", []string{"REVIEWING_CODE_2"})
	assertTransitions(t, tm, "REVIEWING_CODE_2", []string{"CODE_APPROVED", "CODE_REJECTED"})
}

func TestResolver_AllDeclaredStates(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	states := r.AllDeclaredStates()
	// 3 role-pairs: architecture-pair (6 states) + code-planning-pair (6 states) + coding-pair (6 + 2 quorum states) = 20.
	if len(states) != 20 {
		t.Errorf("expected 20 declared states, got %d", len(states))
	}
	expected := []models.TaskStatus{
		"DRAFT_ARCHITECTURE", "ARCHITECTING", "ARCHITECTURE_TO_REVIEW",
		"REVIEWING_ARCHITECTURE", "ARCHITECTURE_APPROVED", "ARCHITECTURE_REJECTED",
		"DRAFT_CODING_PLAN", "CODE_PLANNING", "CODING_PLAN_TO_REVIEW",
		"REVIEWING_CODING_PLAN", "CODING_PLAN_APPROVED", "CODING_PLAN_REJECTED",
		"DRAFT_CODE", "IMPLEMENTING_CODE", "CODE_TO_REVIEW",
		"REVIEWING_CODE", "CODE_APPROVED", "CODE_REJECTED",
		"CODE_PARTIALLY_APPROVED", "REVIEWING_CODE_2",
	}
	for _, e := range expected {
		if !slices.Contains(states, e) {
			t.Errorf("missing declared state %q", e)
		}
	}
}

func TestResolver_StateCategories(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	categories := r.StateCategories()
	tests := []struct {
		status string
		want   StateCategory
	}{
		{"DRAFT_CODE", StateCategoryInitial},
		{"IMPLEMENTING_CODE", StateCategoryExecuting},
		{"CODE_TO_REVIEW", StateCategorySubmitted},
		{"REVIEWING_CODE", StateCategoryReviewing},
		{"CODE_APPROVED", StateCategoryApproved},
		{"CODE_REJECTED", StateCategoryRejected},
		{"CODE_PARTIALLY_APPROVED", StateCategoryPartiallyApproved},
		{"REVIEWING_CODE_2", StateCategoryReviewing2},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got, ok := categories[models.TaskStatus(tt.status)]
			if !ok {
				t.Fatalf("StateCategories missing %s", tt.status)
			}
			if got != tt.want {
				t.Errorf("StateCategories[%s] = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestResolver_SprintTerminalStates(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	got := r.SprintTerminalStates()
	want := []models.TaskStatus{"ARCHITECTURE_APPROVED", "CODING_PLAN_APPROVED", "MERGED"}
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

func TestResolver_RolePair(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	rp, err := r.RolePair("coding-pair")
	if err != nil {
		t.Fatal(err)
	}
	if rp.Doer != "coder" {
		t.Errorf("doer = %q, want %q", rp.Doer, "coder")
	}
	if rp.Reviewer != "code-reviewer" {
		t.Errorf("reviewer = %q, want %q", rp.Reviewer, "code-reviewer")
	}
}

func TestResolver_DoerReviewerRole(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	doer, err := r.DoerRole("code-planning-pair")
	if err != nil {
		t.Fatal(err)
	}
	if doer != "code-planner" {
		t.Errorf("DoerRole = %q, want %q", doer, "code-planner")
	}
	reviewer, err := r.ReviewerRole("code-planning-pair")
	if err != nil {
		t.Fatal(err)
	}
	if reviewer != "code-plan-reviewer" {
		t.Errorf("ReviewerRole = %q, want %q", reviewer, "code-plan-reviewer")
	}
}

func TestResolver_Transition(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	tr, err := r.Transition("code-plan-to-coding")
	if err != nil {
		t.Fatal(err)
	}
	if tr.From != "code-planning-pair.approved" {
		t.Errorf("from = %q, want %q", tr.From, "code-planning-pair.approved")
	}
	if tr.To != "coding-pair.initial" {
		t.Errorf("to = %q, want %q", tr.To, "coding-pair.initial")
	}
	if tr.Trigger != "manual" {
		t.Errorf("trigger = %q, want %q", tr.Trigger, "manual")
	}
	if tr.Cardinality != "per-subtask" {
		t.Errorf("cardinality = %q, want %q", tr.Cardinality, "per-subtask")
	}
}

func TestResolver_TransitionUnknown(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	_, err := r.Transition("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown transition")
	}
}

func TestResolver_AvailableManualTransitions(t *testing.T) {
	r := NewResolver(loadTestConfig(t))

	// CODING_PLAN_APPROVED with no executed transitions — auto transition excluded.
	got := r.AvailableManualTransitions("CODING_PLAN_APPROVED", nil)
	if len(got) != 1 || got[0] != "code-plan-to-coding" {
		t.Errorf("AvailableTransitions(CODING_PLAN_APPROVED, nil) = %v, want [code-plan-to-coding] (auto-code-plan-to-coding should be excluded)", got)
	}

	// Already executed.
	got = r.AvailableManualTransitions("CODING_PLAN_APPROVED", map[string]bool{"code-plan-to-coding": true})
	if len(got) != 0 {
		t.Errorf("AvailableTransitions with executed = %v, want []", got)
	}

	// State with no transitions.
	got = r.AvailableManualTransitions("DRAFT_CODE", nil)
	if len(got) != 0 {
		t.Errorf("AvailableTransitions(DRAFT_CODE, nil) = %v, want []", got)
	}
}

func TestResolver_TransitionTargetRolePair(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	rp, err := r.TransitionTargetRolePair("code-plan-to-coding")
	if err != nil {
		t.Fatal(err)
	}
	if rp != "coding-pair" {
		t.Errorf("TransitionTargetRolePair = %q, want %q", rp, "coding-pair")
	}
}

func TestResolver_IsDeclaredState(t *testing.T) {
	r := NewResolver(loadTestConfig(t))
	if !r.IsDeclaredState("DRAFT_CODE") {
		t.Error("DRAFT_CODE should be a declared state")
	}
	if r.IsDeclaredState("NONEXISTENT") {
		t.Error("NONEXISTENT should not be a declared state")
	}
}

// --- Phase 3: review-policy validation tests ---

func TestReviewPolicyValidation(t *testing.T) {
	// Minimal valid base for review-policy tests.
	// extraStates allows adding optional quorum states to the states block.
	base := func(reviewPolicy string, extraStates ...string) string {
		statesExtra := ""
		for _, s := range extraStates {
			statesExtra += "\n" + s
		}
		return `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: reviewer
` + reviewPolicy + `
      states:
        initial: DRAFT
        executing: IMPL
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED` + statesExtra + `
  sub-pipelines:
    sp:
      steps: [coding-pair]
  entry-points:
    default: sp.coding-pair
`
	}

	quorumStates := []string{
		"        partially-approved: PARTIALLY_APPROVED",
		"        reviewing-2: REVIEWING_2",
	}

	t.Run("valid_quorum_1", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid_quorum_2_with_overrides", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 2
        significant-change:
          quorum: 2
          provider-diversity: preferred
        architecture-impact:
          quorum: 2
          provider-diversity: preferred`, quorumStates...)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects_quorum_zero", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 0`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("expected error for quorum < 1")
		}
		assertContains(t, err.Error(), "quorum")
	})

	t.Run("rejects_negative_quorum", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: -1`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("expected error for negative quorum")
		}
		assertContains(t, err.Error(), "quorum")
	})

	t.Run("rejects_override_quorum_zero", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1
        significant-change:
          quorum: 0`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("expected error for override quorum < 1")
		}
		assertContains(t, err.Error(), "quorum")
		assertContains(t, err.Error(), "significant-change")
	})

	t.Run("rejects_invalid_provider_diversity", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
          provider-diversity: required`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("expected error for invalid provider-diversity")
		}
		assertContains(t, err.Error(), "provider-diversity")
		assertContains(t, err.Error(), "required")
	})

	t.Run("valid_provider_diversity_preferred", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1
        architecture-impact:
          quorum: 2
          provider-diversity: preferred`, quorumStates...)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid_base_level_provider_diversity", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1
        provider-diversity: preferred`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("rejects_invalid_base_level_provider_diversity", func(t *testing.T) {
		yaml := base(`      review-policy:
        quorum: 1
        provider-diversity: required`)
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err == nil {
			t.Fatal("expected error for invalid base-level provider-diversity")
		}
		assertContains(t, err.Error(), "provider-diversity")
		assertContains(t, err.Error(), "required")
	})

	t.Run("valid_no_review_policy", func(t *testing.T) {
		// review-policy is optional — omitting it is valid.
		yaml := base("")
		cfg := writeTemp(t, yaml)
		_, err := Load(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// --- Phase 2: pipeline-transitions tests ---

func TestLoad_Phase2ValidConfig(t *testing.T) {
	cfg, err := Load("testdata/valid-phase2-full.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	// Verify 5 role-pairs parsed.
	if len(cfg.Pipeline.RolePairs) != 5 {
		t.Fatalf("expected 5 role-pairs, got %d", len(cfg.Pipeline.RolePairs))
	}
	for _, name := range []string{"epic-planning-pair", "us-writing-pair", "code-planning-pair", "coding-pair", "architecture-pair"} {
		if _, ok := cfg.Pipeline.RolePairs[name]; !ok {
			t.Errorf("missing role-pair %s", name)
		}
	}

	// Verify 11 roles (10 agent roles + orchestrator).
	if len(cfg.Pipeline.Roles) != 11 {
		t.Fatalf("expected 11 roles, got %d", len(cfg.Pipeline.Roles))
	}

	// Verify 3 sub-pipelines.
	if len(cfg.Pipeline.SubPipelines) != 3 {
		t.Fatalf("expected 3 sub-pipelines, got %d", len(cfg.Pipeline.SubPipelines))
	}

	// Verify pipeline-transitions parsed.
	if len(cfg.Pipeline.PipelineTransitions) != 2 {
		t.Fatalf("expected 2 pipeline-transitions, got %d", len(cfg.Pipeline.PipelineTransitions))
	}
	pt := requirePipelineTransition(t, cfg, "us-to-coding")
	if pt.From != "epic-spec-subpipeline.us-writing-pair.approved" {
		t.Errorf("pipeline-transition from = %q, want 3-part ref", pt.From)
	}
	if pt.To != "architecture-subpipeline.architecture-pair.initial" {
		t.Errorf("pipeline-transition to = %q, want 3-part ref", pt.To)
	}
	if pt.Trigger != "manual" {
		t.Errorf("pipeline-transition trigger = %q, want %q", pt.Trigger, "manual")
	}
	if pt.Cardinality != "many-to-one" {
		t.Errorf("pipeline-transition cardinality = %q, want %q", pt.Cardinality, "many-to-one")
	}

	archToCode := requirePipelineTransition(t, cfg, "architecture-to-code-plan")
	if archToCode.From != "architecture-subpipeline.architecture-pair.approved" {
		t.Errorf("architecture-to-code-plan from = %q, want 3-part ref", archToCode.From)
	}
	if archToCode.To != "coding-subpipeline.code-planning-pair.initial" {
		t.Errorf("architecture-to-code-plan to = %q, want 3-part ref", archToCode.To)
	}

	// Verify 4 entry-points.
	if len(cfg.Pipeline.EntryPoints) != 4 {
		t.Fatalf("expected 4 entry-points, got %d", len(cfg.Pipeline.EntryPoints))
	}
	if ep := cfg.Pipeline.EntryPoints["general-objective"]; ep != "epic-spec-subpipeline.epic-planning-pair" {
		t.Errorf("entry-point general-objective = %q", ep)
	}
	if ep := cfg.Pipeline.EntryPoints["functional-spec"]; ep != "architecture-subpipeline.architecture-pair" {
		t.Errorf("entry-point functional-spec = %q", ep)
	}
	if ep := cfg.Pipeline.EntryPoints["detailed-spec"]; ep != "architecture-subpipeline.architecture-pair" {
		t.Errorf("entry-point detailed-spec = %q", ep)
	}
	if ep := cfg.Pipeline.EntryPoints["technical-spec"]; ep != "coding-subpipeline.code-planning-pair" {
		t.Errorf("entry-point technical-spec = %q", ep)
	}
}

func requirePipelineTransition(t *testing.T, cfg *PipelineConfig, name string) TransitionDef {
	t.Helper()
	for _, transition := range cfg.Pipeline.PipelineTransitions {
		if transition.Name == name {
			return transition
		}
	}
	t.Fatalf("missing pipeline-transition %q", name)
	return TransitionDef{}
}

func TestParse3PartRef(t *testing.T) {
	tests := []struct {
		ref                       string
		wantSP, wantRP, wantPhase string
		wantErr                   bool
	}{
		{"epic-spec-subpipeline.us-writing-pair.approved", "epic-spec-subpipeline", "us-writing-pair", "approved", false},
		{"coding-subpipeline.code-planning-pair.initial", "coding-subpipeline", "code-planning-pair", "initial", false},
		// Invalid: only 2 parts.
		{"role-pair.approved", "", "", "", true},
		// Invalid: only 1 part.
		{"single", "", "", "", true},
		// Invalid: empty component.
		{".role-pair.approved", "", "", "", true},
		{"sp..approved", "", "", "", true},
		{"sp.rp.", "", "", "", true},
	}
	for _, tt := range tests {
		sp, rp, phase, err := parse3PartRef(tt.ref)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parse3PartRef(%q): expected error, got (%q, %q, %q)", tt.ref, sp, rp, phase)
			}
			continue
		}
		if err != nil {
			t.Errorf("parse3PartRef(%q): unexpected error: %v", tt.ref, err)
			continue
		}
		if sp != tt.wantSP || rp != tt.wantRP || phase != tt.wantPhase {
			t.Errorf("parse3PartRef(%q) = (%q, %q, %q), want (%q, %q, %q)",
				tt.ref, sp, rp, phase, tt.wantSP, tt.wantRP, tt.wantPhase)
		}
	}
}

func TestLoad_PipelineTransition_Invalid3PartRef(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  pipeline-transitions:
    - name: bad-ref
      from: pair-a.approved
      to: sp2.pair-b.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for 2-part ref in pipeline-transition from")
	}
	assertContains(t, err.Error(), "3-part")
}

func TestLoad_PipelineTransition_SameSubPipeline(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a, pair-b]
      transitions: []
  pipeline-transitions:
    - name: same-sp
      from: sp1.pair-a.approved
      to: sp1.pair-b.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for pipeline-transition within same sub-pipeline")
	}
	assertContains(t, err.Error(), "different sub-pipelines")
}

func TestLoad_PipelineTransition_NonexistentSubPipeline(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  pipeline-transitions:
    - name: bad-sp
      from: nonexistent.pair-a.approved
      to: sp2.pair-b.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent sub-pipeline in pipeline-transition")
	}
	assertContains(t, err.Error(), "nonexistent")
	assertContains(t, err.Error(), "not found")
}

func TestLoad_PipelineTransition_RolePairNotInSubPipeline(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  pipeline-transitions:
    - name: wrong-membership
      from: sp1.pair-b.approved
      to: sp2.pair-a.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for role-pair not in referenced sub-pipeline")
	}
	assertContains(t, err.Error(), "not a step")
}

func TestLoad_PipelineTransition_InvalidPhase(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  pipeline-transitions:
    - name: bad-phase
      from: sp1.pair-a.nonexistent
      to: sp2.pair-b.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for invalid phase in pipeline-transition")
	}
	assertContains(t, err.Error(), "invalid phase")
}

func TestLoad_DuplicateTransitionNames_PipelineAndSubPipeline(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
    pair-c:
      doer: a
      reviewer: b
      states:
        initial: U1
        executing: U2
        submitted: U3
        reviewing: U4
        approved: U5
        rejected: U6
  sub-pipelines:
    sp1:
      steps: [pair-a, pair-b]
      transitions:
        - name: advance
          from: pair-a.approved
          to: pair-b.initial
          trigger: manual
          cardinality: per-subtask
    sp2:
      steps: [pair-c]
      transitions: []
  pipeline-transitions:
    - name: advance
      from: sp1.pair-b.approved
      to: sp2.pair-c.initial
      trigger: manual
      cardinality: one-to-one
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate transition name across sub-pipeline and pipeline-transitions")
	}
	assertContains(t, err.Error(), "duplicate transition name")
	assertContains(t, err.Error(), "advance")
}

// --- Roles section tests ---

func TestLoad_RolesSection(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
      description: "Implements code changes"
      timeouts:
        execution: 2h
        poll-interval: 30s
        max-wait: 30m
      context-sections:
        - assigned-task
        - worktree-rules
      allowed-operations:
        - write-checkpoint
        - submit-for-review
      skills:
        - debugging
        - testing
      mandatory-docs: []
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      description: "Reviews code changes"
      allowed-operations:
        - submit-verdict
      skills:
        - code-review
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
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	pc, err := Load(cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(pc.Pipeline.Roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(pc.Pipeline.Roles))
	}

	coder, ok := pc.Pipeline.Roles["coder"]
	if !ok {
		t.Fatal("missing role coder")
	}
	if coder.Type != "doer" {
		t.Errorf("coder.Type = %q, want %q", coder.Type, "doer")
	}
	if coder.DisplayName != "Coder" {
		t.Errorf("coder.DisplayName = %q, want %q", coder.DisplayName, "Coder")
	}
	if coder.Description != "Implements code changes" {
		t.Errorf("coder.Description = %q", coder.Description)
	}
	if coder.Timeouts == nil {
		t.Fatal("coder.Timeouts is nil")
	}
	if coder.Timeouts.Execution != "2h" {
		t.Errorf("coder.Timeouts.Execution = %q, want %q", coder.Timeouts.Execution, "2h")
	}
	if coder.Timeouts.PollInterval != "30s" {
		t.Errorf("coder.Timeouts.PollInterval = %q, want %q", coder.Timeouts.PollInterval, "30s")
	}
	if coder.Timeouts.MaxWait != "30m" {
		t.Errorf("coder.Timeouts.MaxWait = %q, want %q", coder.Timeouts.MaxWait, "30m")
	}
	if len(coder.ContextSections) != 2 {
		t.Errorf("coder.ContextSections length = %d, want 2", len(coder.ContextSections))
	}
	if len(coder.AllowedOperations) != 2 {
		t.Errorf("coder.AllowedOperations length = %d, want 2", len(coder.AllowedOperations))
	}
	if len(coder.Skills) != 2 {
		t.Errorf("coder.Skills length = %d, want 2", len(coder.Skills))
	}

	reviewer := pc.Pipeline.Roles["code-reviewer"]
	if reviewer.Type != "reviewer" {
		t.Errorf("code-reviewer.Type = %q, want %q", reviewer.Type, "reviewer")
	}
	if reviewer.Timeouts != nil {
		t.Errorf("code-reviewer.Timeouts should be nil, got %+v", reviewer.Timeouts)
	}
}

func TestValidate_RoleMissingType(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    coder:
      display-name: "Coder"
      description: "Implements code changes"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: coder
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for missing type field")
	}
	assertContains(t, err.Error(), "type")
	assertContains(t, err.Error(), "required")
}

func TestValidate_RoleInvalidType(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    coder:
      type: worker
      display-name: "Coder"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: coder
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for invalid type value")
	}
	assertContains(t, err.Error(), "worker")
	assertContains(t, err.Error(), "type")
}

func TestValidate_RolePairUndefinedRole(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: reviewer
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
`
	cfg := writeTemp(t, yamlContent)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for role-pair referencing undefined role")
	}
	assertContains(t, err.Error(), "reviewer")
	assertContains(t, err.Error(), "not found in roles")
}

func TestLoad_EmbeddedPipelineRoles(t *testing.T) {
	data, err := os.ReadFile("../embedded/pipeline.yaml")
	if err != nil {
		t.Fatalf("failed to read embedded pipeline.yaml: %v", err)
	}
	cfg, err := LoadFromBytes(data)
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}

	if len(cfg.Pipeline.Roles) != 13 {
		t.Fatalf("expected 13 roles, got %d", len(cfg.Pipeline.Roles))
	}

	expectedRoles := map[string]string{
		"coder":                 "doer",
		"code-reviewer":         "reviewer",
		"orchestrator":          "orchestrator",
		"epic-planner":          "doer",
		"epic-plan-reviewer":    "reviewer",
		"us-writer":             "doer",
		"us-reviewer":           "reviewer",
		"code-planner":          "doer",
		"code-plan-reviewer":    "reviewer",
		"integration-analyst":   "doer",
		"integration-reviewer":  "reviewer",
		"architect":             "doer",
		"architecture-reviewer": "reviewer",
	}
	for name, wantType := range expectedRoles {
		role, ok := cfg.Pipeline.Roles[name]
		if !ok {
			t.Errorf("missing role %q", name)
			continue
		}
		if role.Type != wantType {
			t.Errorf("role %q: type = %q, want %q", name, role.Type, wantType)
		}
		if role.DisplayName == "" {
			t.Errorf("role %q: display-name is empty", name)
		}
	}

	// Verify orchestrator has max-instances: 1.
	orch := cfg.Pipeline.Roles["orchestrator"]
	if orch.MaxInstances != 1 {
		t.Errorf("orchestrator max-instances = %d, want 1", orch.MaxInstances)
	}
}

func TestLoad_UnknownFieldRejected(t *testing.T) {
	yaml := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    pair-a:
      doer: coder
      reviewer: reviewer
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
  sub-pipelines: {}
  entry-points: {}
  agent_roles:
    typo: "This should fail"
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for unknown field in pipeline config")
	}
	assertContains(t, err.Error(), "agent_roles")
}

// --- helpers ---

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pipeline.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("error %q does not contain %q", got, want)
	}
}

func decompositionRootYAML(from, trigger, cardinality, extraTransitions string) string {
	transitionBlock := "      transitions: []"
	if from != "" {
		transitionBlock = `
      transitions:
        - name: root-to-leaf
          from: ` + from + `
          to: leaf-pair.initial
          trigger: ` + trigger + `
          cardinality: ` + cardinality + extraTransitions
	}
	return `
pipeline:
  roles:
    doer:
      type: doer
      display-name: "Doer"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    root-pair:
      doer: doer
      reviewer: reviewer
      decomposition-root: true
      states:
        initial: ROOT_INITIAL
        executing: ROOT_EXECUTING
        submitted: ROOT_SUBMITTED
        reviewing: ROOT_REVIEWING
        approved: ROOT_APPROVED
        rejected: ROOT_REJECTED
    leaf-pair:
      doer: doer
      reviewer: reviewer
      states:
        initial: LEAF_INITIAL
        executing: LEAF_EXECUTING
        submitted: LEAF_SUBMITTED
        reviewing: LEAF_REVIEWING
        approved: LEAF_APPROVED
        rejected: LEAF_REJECTED
    extra-pair:
      doer: doer
      reviewer: reviewer
      states:
        initial: EXTRA_INITIAL
        executing: EXTRA_EXECUTING
        submitted: EXTRA_SUBMITTED
        reviewing: EXTRA_REVIEWING
        approved: EXTRA_APPROVED
        rejected: EXTRA_REJECTED
  sub-pipelines:
    main:
      steps: [root-pair, leaf-pair, extra-pair]
` + transitionBlock + `
  entry-points: {}
`
}

func decompositionRootCrossSubPipelineYAML() string {
	return `
pipeline:
  roles:
    doer:
      type: doer
      display-name: "Doer"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    root-pair:
      doer: doer
      reviewer: reviewer
      decomposition-root: true
      states:
        initial: ROOT_INITIAL
        executing: ROOT_EXECUTING
        submitted: ROOT_SUBMITTED
        reviewing: ROOT_REVIEWING
        approved: ROOT_APPROVED
        rejected: ROOT_REJECTED
    leaf-pair:
      doer: doer
      reviewer: reviewer
      states:
        initial: LEAF_INITIAL
        executing: LEAF_EXECUTING
        submitted: LEAF_SUBMITTED
        reviewing: LEAF_REVIEWING
        approved: LEAF_APPROVED
        rejected: LEAF_REJECTED
  sub-pipelines:
    root-sp:
      steps: [root-pair]
      transitions: []
    leaf-sp:
      steps: [leaf-pair]
      transitions: []
  pipeline-transitions:
    - name: root-to-leaf
      from: root-sp.root-pair.approved
      to: leaf-sp.leaf-pair.initial
      trigger: auto
      cardinality: per-subtask
  entry-points: {}
`
}

func TestResolver_PartiallyApprovedStatus(t *testing.T) {
	r := NewResolver(loadTestConfig(t))

	status, err := r.PartiallyApprovedStatus("coding-pair")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "CODE_PARTIALLY_APPROVED" {
		t.Errorf("PartiallyApprovedStatus = %q, want %q", status, "CODE_PARTIALLY_APPROVED")
	}

	// code-planning-pair has no quorum states.
	_, err = r.PartiallyApprovedStatus("code-planning-pair")
	if err == nil {
		t.Fatal("expected error for code-planning-pair (no partially-approved state)")
	}
}

func TestResolver_Reviewing2Status(t *testing.T) {
	r := NewResolver(loadTestConfig(t))

	status, err := r.Reviewing2Status("coding-pair")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "REVIEWING_CODE_2" {
		t.Errorf("Reviewing2Status = %q, want %q", status, "REVIEWING_CODE_2")
	}

	// code-planning-pair has no quorum states.
	_, err = r.Reviewing2Status("code-planning-pair")
	if err == nil {
		t.Fatal("expected error for code-planning-pair (no reviewing-2 state)")
	}
}

func TestQuorumStatesRequiredForOverrides(t *testing.T) {
	// Base quorum 1, override quorum 2, missing quorum states → rejected.
	yaml := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: reviewer
      review-policy:
        quorum: 1
        significant-change:
          quorum: 2
      states:
        initial: DRAFT
        executing: EXECUTING
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED
  sub-pipelines:
    main:
      steps:
        - coding-pair
  entry-points:
    default: main.coding-pair
`
	_, err := LoadFromBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing quorum states when override quorum > 1")
	}
	if !strings.Contains(err.Error(), "quorum states") {
		t.Errorf("error = %q, want substring 'quorum states'", err.Error())
	}
}

func TestQuorumStatesNotRequiredWhenQuorumIs1(t *testing.T) {
	// Base quorum 1, no overrides → quorum states optional.
	yaml := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: reviewer
      review-policy:
        quorum: 1
      states:
        initial: DRAFT
        executing: EXECUTING
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED
  sub-pipelines:
    main:
      steps:
        - coding-pair
  entry-points:
    default: main.coding-pair
`
	_, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_CleanState_Parses(t *testing.T) {
	yaml := `
pipeline:
  roles:
    analyst:
      type: doer
      display-name: "Analyst"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    integration-pair:
      doer: analyst
      reviewer: reviewer
      states:
        initial: DRAFT_INTEGRATION_ANALYSIS
        executing: ANALYZING_INTEGRATION
        submitted: INTEGRATION_ANALYSIS_TO_REVIEW
        reviewing: REVIEWING_INTEGRATION_ANALYSIS
        approved: INTEGRATION_ANALYSIS_APPROVED
        rejected: INTEGRATION_ANALYSIS_REJECTED
        clean: INTEGRATION_ANALYSIS_CLEAN
  sub-pipelines:
    integration:
      steps: [integration-pair]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	config, err := Load(cfg)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	rp := config.Pipeline.RolePairs["integration-pair"]
	if rp.States.Clean != "INTEGRATION_ANALYSIS_CLEAN" {
		t.Errorf("Clean = %q, want %q", rp.States.Clean, "INTEGRATION_ANALYSIS_CLEAN")
	}
}

func TestLoad_CleanState_DuplicateAcrossRolePairs(t *testing.T) {
	yaml := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_A
        executing: EXEC_A
        submitted: SUB_A
        reviewing: REV_A
        approved: APP_A
        rejected: REJ_A
        clean: SHARED_CLEAN
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_B
        executing: EXEC_B
        submitted: SUB_B
        reviewing: REV_B
        approved: APP_B
        rejected: REJ_B
        clean: SHARED_CLEAN
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for duplicate clean state name")
	}
	assertContains(t, err.Error(), "duplicate state name")
	assertContains(t, err.Error(), "SHARED_CLEAN")
}

func TestLoad_CleanState_BackwardCompat(t *testing.T) {
	// Pipeline YAML without clean field should still parse.
	cfg, err := Load("testdata/valid-coding-subpipeline.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// Existing role-pairs should have empty Clean.
	for name, rp := range cfg.Pipeline.RolePairs {
		if rp.States.Clean != "" {
			t.Errorf("role-pair %q: Clean = %q, want empty", name, rp.States.Clean)
		}
	}
}

func TestLoad_DecompositionRootAbsentBackwardCompat(t *testing.T) {
	cfg, err := Load("testdata/valid-coding-subpipeline.yaml")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	for name, rp := range cfg.Pipeline.RolePairs {
		if rp.DecompositionRoot {
			t.Errorf("role-pair %q: DecompositionRoot = true, want false", name)
		}
	}
}

func TestLoad_DecompositionRootValidTopology(t *testing.T) {
	yaml := decompositionRootYAML("root-pair.approved", "auto", "per-subtask", "")
	cfg, err := LoadFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadFromBytes failed: %v", err)
	}
	if !cfg.Pipeline.RolePairs["root-pair"].DecompositionRoot {
		t.Fatal("root-pair DecompositionRoot = false, want true")
	}
}

func TestLoad_DecompositionRootRejectsInvalidTopology(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "zero outgoing decomposition transition",
			yaml: decompositionRootYAML("", "", "", ""),
			want: "exactly one outgoing decomposition transition",
		},
		{
			name: "multiple outgoing decomposition transitions",
			yaml: decompositionRootYAML("root-pair.approved", "auto", "per-subtask", `
        - name: root-to-extra
          from: root-pair.approved
          to: extra-pair.initial
          trigger: auto
          cardinality: per-subtask`),
			want: "exactly one outgoing decomposition transition",
		},
		{
			name: "non-auto trigger",
			yaml: decompositionRootYAML("root-pair.approved", "manual", "per-subtask", ""),
			want: "trigger must be auto",
		},
		{
			name: "non-per-subtask cardinality",
			yaml: decompositionRootYAML("root-pair.approved", "auto", "one-to-one", ""),
			want: "cardinality must be per-subtask",
		},
		{
			name: "cross-subpipeline target",
			yaml: decompositionRootCrossSubPipelineYAML(),
			want: "same sub-pipeline",
		},
		{
			name: "non-approved source phase",
			yaml: decompositionRootYAML("root-pair.submitted", "auto", "per-subtask", ""),
			want: "from phase must be approved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadFromBytes([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected validation error")
			}
			assertContains(t, err.Error(), "decomposition-root")
			assertContains(t, err.Error(), tt.want)
		})
	}
}

func TestValidPhases_IncludesClean(t *testing.T) {
	if !validPhases["clean"] {
		t.Error("validPhases[\"clean\"] is false, want true")
	}
}

func TestCleanStateRejectedWithQuorumGt1(t *testing.T) {
	yaml := `
pipeline:
  roles:
    analyst:
      type: doer
      display-name: "Analyst"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    test-pair:
      doer: analyst
      reviewer: reviewer
      review-policy:
        quorum: 2
      states:
        initial: DRAFT
        executing: EXECUTING
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED
        partially-approved: PARTIALLY_APPROVED
        reviewing-2: REVIEWING_2
        clean: CLEAN_STATE
  sub-pipelines:
    main:
      steps: [test-pair]
      transitions: []
  entry-points: {}
`
	cfg := writeTemp(t, yaml)
	_, err := Load(cfg)
	if err == nil {
		t.Fatal("expected error for clean + quorum > 1")
	}
	assertContains(t, err.Error(), "clean state is not supported with quorum > 1")
}

func TestQuorumStatesRequiredForBaseQuorum(t *testing.T) {
	// Base quorum 2, missing quorum states → rejected.
	yaml := `
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: reviewer
      review-policy:
        quorum: 2
      states:
        initial: DRAFT
        executing: EXECUTING
        submitted: SUBMITTED
        reviewing: REVIEWING
        approved: APPROVED
        rejected: REJECTED
  sub-pipelines:
    main:
      steps:
        - coding-pair
  entry-points:
    default: main.coding-pair
`
	_, err := LoadFromBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected validation error for missing quorum states when base quorum > 1")
	}
	if !strings.Contains(err.Error(), "quorum states") {
		t.Errorf("error = %q, want substring 'quorum states'", err.Error())
	}
}

func assertTransitions(t *testing.T, tm map[models.TaskStatus][]models.TaskStatus, from string, wantStrs []string) {
	t.Helper()
	got := tm[models.TaskStatus(from)]
	want := make([]models.TaskStatus, len(wantStrs))
	for i, s := range wantStrs {
		want[i] = models.TaskStatus(s)
	}
	slices.Sort(got)
	slices.Sort(want)
	if len(got) != len(want) {
		t.Errorf("transitions from %s: got %v, want %v", from, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("transitions from %s: got %v, want %v", from, got, want)
			return
		}
	}
}

func TestValidate_ManyToOneCardinality(t *testing.T) {
	// Minimal two-sub-pipeline config for a pipeline-transition with many-to-one.
	validYAML := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: S1
        executing: S2
        submitted: S3
        reviewing: S4
        approved: S5
        rejected: S6
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: T1
        executing: T2
        submitted: T3
        reviewing: T4
        approved: T5
        rejected: T6
  sub-pipelines:
    sp1:
      steps: [pair-a]
      transitions: []
    sp2:
      steps: [pair-b]
      transitions: []
  pipeline-transitions:
    - name: fan-in
      from: sp1.pair-a.approved
      to: sp2.pair-b.initial
      trigger: manual
      cardinality: many-to-one
  entry-points: {}
`

	t.Run("many-to-one accepted", func(t *testing.T) {
		_, err := LoadFromBytes([]byte(validYAML))
		if err != nil {
			t.Fatalf("expected many-to-one to be accepted, got error: %v", err)
		}
	})

	t.Run("bogus cardinality rejected", func(t *testing.T) {
		bogusYAML := strings.Replace(validYAML, "cardinality: many-to-one", "cardinality: bogus", 1)
		_, err := LoadFromBytes([]byte(bogusYAML))
		if err == nil {
			t.Fatal("expected error for bogus cardinality")
		}
		assertContains(t, err.Error(), "cardinality")
		assertContains(t, err.Error(), "bogus")
	})
}

func TestTaskSlugOrName(t *testing.T) {
	t.Run("returns slug when set", func(t *testing.T) {
		td := TransitionDef{Name: "us-to-coding", TaskSlug: "architecture"}
		if got := td.TaskSlugOrName(); got != "architecture" {
			t.Errorf("TaskSlugOrName() = %q, want %q", got, "architecture")
		}
	})
	t.Run("falls back to name when slug empty", func(t *testing.T) {
		td := TransitionDef{Name: "us-to-coding"}
		if got := td.TaskSlugOrName(); got != "us-to-coding" {
			t.Errorf("TaskSlugOrName() = %q, want %q", got, "us-to-coding")
		}
	})
}

func TestLoad_InvalidTaskSlug(t *testing.T) {
	yamlContent := `
pipeline:
  roles:
    a:
      type: doer
      display-name: "A"
    b:
      type: reviewer
      display-name: "B"
  role-pairs:
    pair-a:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_A
        executing: WORKING_A
        submitted: SUBMITTED_A
        reviewing: REVIEWING_A
        approved: APPROVED_A
        rejected: REJECTED_A
    pair-b:
      doer: a
      reviewer: b
      states:
        initial: DRAFT_B
        executing: WORKING_B
        submitted: SUBMITTED_B
        reviewing: REVIEWING_B
        approved: APPROVED_B
        rejected: REJECTED_B
  sub-pipelines:
    sp:
      steps: [pair-a, pair-b]
      transitions:
        - name: a-to-b
          task-slug: "BAD SLUG"
          from: pair-a.approved
          to: pair-b.initial
          trigger: manual
          cardinality: per-subtask
  entry-points:
    default: sp.pair-a
`
	_, err := LoadFromBytes([]byte(yamlContent))
	if err == nil {
		t.Fatal("expected error for invalid task-slug")
	}
	assertContains(t, err.Error(), "task-slug")
	assertContains(t, err.Error(), "kebab-case")
}
