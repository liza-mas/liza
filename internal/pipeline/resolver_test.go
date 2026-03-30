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
	if tr.To != "coding-subpipeline.code-planning-pair.initial" {
		t.Errorf("to = %q, want 3-part ref", tr.To)
	}
	if tr.Trigger != "manual" {
		t.Errorf("trigger = %q, want %q", tr.Trigger, "manual")
	}
	if tr.Cardinality != "one-to-one" {
		t.Errorf("cardinality = %q, want %q", tr.Cardinality, "one-to-one")
	}
}

func TestResolver_Transition_SubPipelineStillWorks(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// Sub-pipeline transitions should still be found.
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

func TestResolver_AvailableTransitions_PipelineTransition(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	// US_APPROVED should have "us-to-coding" available.
	got := r.AvailableTransitions("US_APPROVED", nil)
	want := []string{"us-to-coding"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(US_APPROVED, nil) = %v, want %v", got, want)
	}

	// Already executed — should return empty.
	got = r.AvailableTransitions("US_APPROVED", map[string]bool{"us-to-coding": true})
	if len(got) != 0 {
		t.Errorf("AvailableTransitions with executed = %v, want []", got)
	}

	// EPIC_PLAN_APPROVED should have "epic-to-us" available.
	got = r.AvailableTransitions("EPIC_PLAN_APPROVED", nil)
	want = []string{"epic-to-us"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(EPIC_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	// CODING_PLAN_APPROVED should have "code-plan-to-coding" available.
	got = r.AvailableTransitions("CODING_PLAN_APPROVED", nil)
	want = []string{"code-plan-to-coding"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("AvailableTransitions(CODING_PLAN_APPROVED, nil) = %v, want %v", got, want)
	}

	// DRAFT_CODE should have no transitions.
	got = r.AvailableTransitions("DRAFT_CODE", nil)
	if len(got) != 0 {
		t.Errorf("AvailableTransitions(DRAFT_CODE, nil) = %v, want []", got)
	}
}

func TestResolver_SprintTerminalStates_Phase2(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))
	got := r.SprintTerminalStates()

	// Pipeline-transition sources: us-writing-pair.approved (US_APPROVED)
	// Sub-pipeline transition sources: epic-planning-pair.approved (EPIC_PLAN_APPROVED),
	//   code-planning-pair.approved (CODING_PLAN_APPROVED)
	// Plus MERGED always included.
	want := []models.TaskStatus{
		"CODING_PLAN_APPROVED",
		"EPIC_PLAN_APPROVED",
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

	want := []string{"code-planning-pair", "coding-pair", "epic-planning-pair", "us-writing-pair"}
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

	// Sub-pipeline transition sources: epic-planning-pair, code-planning-pair
	// Pipeline-transition sources: us-writing-pair
	want := map[string]bool{
		"epic-planning-pair": true,
		"code-planning-pair": true,
		"us-writing-pair":    true,
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

func TestResolver_IsTransitionSourcePair_Phase2(t *testing.T) {
	r := NewResolver(loadPhase2Config(t))

	sources := []string{"epic-planning-pair", "code-planning-pair", "us-writing-pair"}
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
	want := []string{"code-planner", "coder", "epic-planner", "us-writer"}
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
	want := []string{"code-plan-reviewer", "code-reviewer", "epic-plan-reviewer", "us-reviewer"}
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
	want := []string{"write-checkpoint", "submit-for-review", "mark-blocked", "handoff", "set-task-output", "await-verdict"}
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
	if rp != "code-planning-pair" {
		t.Errorf("TransitionTargetRolePair = %q, want %q", rp, "code-planning-pair")
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
		"integration-fix", "prior-rejection", "prior-attempt", "doer-state-transitions", "doer-tools",
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
        submitted: CODE_READY_FOR_REVIEW
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
