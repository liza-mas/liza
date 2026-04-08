package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/pipeline"
)

// --- extractOutputEntries tests ---

func TestExtractOutputEntries_WithDependsOn(t *testing.T) {
	raw := []any{
		map[string]any{
			"desc":       "Setup DB",
			"done_when":  "DB ready",
			"scope":      "db",
			"spec_ref":   "specs/db.md",
			"depends_on": []any{},
		},
		map[string]any{
			"desc":       "Build API",
			"done_when":  "API works",
			"scope":      "api",
			"spec_ref":   "specs/api.md",
			"depends_on": []any{"0"},
		},
		map[string]any{
			"desc":       "Build UI",
			"done_when":  "UI works",
			"scope":      "ui",
			"spec_ref":   "specs/ui.md",
			"depends_on": []any{"0", "1"},
		},
	}

	entries, err := extractOutputEntries(raw)
	if err != nil {
		t.Fatalf("extractOutputEntries() error: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}

	// Entry 0: empty depends_on
	if len(entries[0].DependsOn) != 0 {
		t.Errorf("entries[0].DependsOn = %v, want empty", entries[0].DependsOn)
	}

	// Entry 1: depends on "0"
	if len(entries[1].DependsOn) != 1 || entries[1].DependsOn[0] != "0" {
		t.Errorf("entries[1].DependsOn = %v, want [\"0\"]", entries[1].DependsOn)
	}

	// Entry 2: depends on "0" and "1"
	if len(entries[2].DependsOn) != 2 || entries[2].DependsOn[0] != "0" || entries[2].DependsOn[1] != "1" {
		t.Errorf("entries[2].DependsOn = %v, want [\"0\", \"1\"]", entries[2].DependsOn)
	}
}

func TestExtractOutputEntries_NoDependsOn(t *testing.T) {
	raw := []any{
		map[string]any{
			"desc":      "Task",
			"done_when": "Done",
			"scope":     "s",
		},
	}

	entries, err := extractOutputEntries(raw)
	if err != nil {
		t.Fatalf("extractOutputEntries() error: %v", err)
	}

	if len(entries[0].DependsOn) != 0 {
		t.Errorf("entries[0].DependsOn = %v, want nil/empty", entries[0].DependsOn)
	}
}

// testHelpersPipelineYAML defines a minimal pipeline with both standard and
// custom roles to verify resolver-based authorization in authorizeClaimRelease.
var testHelpersPipelineYAML = []byte(`
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
      allowed-operations:
        - submit-for-review
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      allowed-operations:
        - submit-verdict
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      allowed-operations:
        - add-tasks
    data-engineer:
      type: doer
      display-name: "Data Engineer"
      allowed-operations:
        - submit-for-review
    security-reviewer:
      type: reviewer
      display-name: "Security Reviewer"
      allowed-operations:
        - submit-verdict
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: READY
        executing: CODING
        submitted: CODE_SUBMITTED
        reviewing: CODE_REVIEWING
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    coding:
      steps:
        - coding-pair
      transitions: []
  entry-points:
    default: coding.coding-pair
`)

func testHelpersResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(testHelpersPipelineYAML)
	if err != nil {
		t.Fatalf("testHelpersResolver: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

func TestAuthorizeClaimRelease(t *testing.T) {
	resolver := testHelpersResolver(t)

	t.Run("standard doer can release doer claim", func(t *testing.T) {
		err := authorizeClaimRelease("coder-1", "doer", resolver)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("standard doer cannot release reviewer claim", func(t *testing.T) {
		err := authorizeClaimRelease("coder-1", "reviewer", resolver)
		if err == nil {
			t.Fatal("expected error for doer releasing reviewer claim")
		}
		if !strings.Contains(err.Error(), "can only release doer claims") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("standard reviewer can release reviewer claim", func(t *testing.T) {
		err := authorizeClaimRelease("code-reviewer-1", "reviewer", resolver)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("standard reviewer cannot release doer claim", func(t *testing.T) {
		err := authorizeClaimRelease("code-reviewer-1", "doer", resolver)
		if err == nil {
			t.Fatal("expected error for reviewer releasing doer claim")
		}
		if !strings.Contains(err.Error(), "can only release reviewer claims") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("orchestrator can release any claim type", func(t *testing.T) {
		for _, claimRole := range []string{"doer", "reviewer", "both"} {
			err := authorizeClaimRelease("orchestrator-1", claimRole, resolver)
			if err != nil {
				t.Errorf("orchestrator should be allowed to release %q claim, got: %v", claimRole, err)
			}
		}
	})

	t.Run("custom YAML doer role passes doer claim release", func(t *testing.T) {
		err := authorizeClaimRelease("data-engineer-1", "doer", resolver)
		if err != nil {
			t.Errorf("custom doer role should authorize doer claim release: %v", err)
		}
	})

	t.Run("custom YAML doer role cannot release reviewer claim", func(t *testing.T) {
		err := authorizeClaimRelease("data-engineer-1", "reviewer", resolver)
		if err == nil {
			t.Fatal("expected error for custom doer releasing reviewer claim")
		}
	})

	t.Run("custom YAML reviewer role passes reviewer claim release", func(t *testing.T) {
		err := authorizeClaimRelease("security-reviewer-1", "reviewer", resolver)
		if err != nil {
			t.Errorf("custom reviewer role should authorize reviewer claim release: %v", err)
		}
	})

	t.Run("custom YAML reviewer role cannot release doer claim", func(t *testing.T) {
		err := authorizeClaimRelease("security-reviewer-1", "doer", resolver)
		if err == nil {
			t.Fatal("expected error for custom reviewer releasing doer claim")
		}
	})

	t.Run("nil resolver rejects all", func(t *testing.T) {
		err := authorizeClaimRelease("coder-1", "doer", nil)
		if err == nil {
			t.Fatal("expected error when resolver is nil")
		}
		if !strings.Contains(err.Error(), "resolver") {
			t.Errorf("expected resolver-related error, got: %v", err)
		}
	})

	t.Run("invalid agent ID format rejected", func(t *testing.T) {
		err := authorizeClaimRelease("invalid", "doer", resolver)
		if err == nil {
			t.Fatal("expected error for invalid agent ID")
		}
	})

	t.Run("unknown role rejected", func(t *testing.T) {
		// Role extracted from agent ID but not in pipeline YAML
		err := authorizeClaimRelease("phantom-1", "doer", resolver)
		if err == nil {
			t.Fatal("expected error for unknown role")
		}
	})
}

func TestExtractOutputEntries_WithPlanRef(t *testing.T) {
	raw := []any{
		map[string]any{
			"desc":      "Task A",
			"done_when": "A works",
			"scope":     "a",
			"spec_ref":  "specs/a.md",
			"plan_ref":  "specs/plans/plan.md",
		},
		map[string]any{
			"desc":      "Task B",
			"done_when": "B works",
			"scope":     "b",
			"spec_ref":  "specs/b.md",
			// plan_ref intentionally omitted
		},
	}

	entries, err := extractOutputEntries(raw)
	if err != nil {
		t.Fatalf("extractOutputEntries() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].PlanRef != "specs/plans/plan.md" {
		t.Errorf("entries[0].PlanRef = %q, want %q", entries[0].PlanRef, "specs/plans/plan.md")
	}
	if entries[1].PlanRef != "" {
		t.Errorf("entries[1].PlanRef = %q, want empty", entries[1].PlanRef)
	}
}

func TestExtractOutputEntries_WithArchRef(t *testing.T) {
	raw := []any{
		map[string]any{
			"desc":      "Task A",
			"done_when": "A works",
			"scope":     "a",
			"spec_ref":  "specs/a.md",
			"arch_ref":  "specs/arch-plan/feature.md",
		},
		map[string]any{
			"desc":      "Task B",
			"done_when": "B works",
			"scope":     "b",
			"spec_ref":  "specs/b.md",
			// arch_ref intentionally omitted
		},
	}

	entries, err := extractOutputEntries(raw)
	if err != nil {
		t.Fatalf("extractOutputEntries() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].ArchRef != "specs/arch-plan/feature.md" {
		t.Errorf("entries[0].ArchRef = %q, want %q", entries[0].ArchRef, "specs/arch-plan/feature.md")
	}
	if entries[1].ArchRef != "" {
		t.Errorf("entries[1].ArchRef = %q, want empty", entries[1].ArchRef)
	}
}

func TestExtractOutputEntries_RejectsStringArray(t *testing.T) {
	raw := []any{
		`{"desc": "Setup DB", "done_when": "DB ready", "scope": "db"}`,
		`{"desc": "Build API", "done_when": "API works", "scope": "api"}`,
	}

	_, err := extractOutputEntries(raw)
	if err == nil {
		t.Fatal("expected error for string array input")
	}

	var shapeErr *InputShapeError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected InputShapeError, got %T: %v", err, err)
	}
	if !strings.Contains(shapeErr.Message, "must be an object") {
		t.Errorf("message = %q, want substring %q", shapeErr.Message, "must be an object")
	}
}

func TestExtractTaskInputs_RejectsStringArray(t *testing.T) {
	raw := []any{
		`{"id": "t1", "desc": "Do something", "spec": "specs/v.md", "done": "Done", "scope": "s"}`,
	}

	_, err := extractTaskInputs(raw)
	if err == nil {
		t.Fatal("expected error for string array input")
	}

	var shapeErr *InputShapeError
	if !errors.As(err, &shapeErr) {
		t.Fatalf("expected InputShapeError, got %T: %v", err, err)
	}
	if !strings.Contains(shapeErr.Message, "must be an object") {
		t.Errorf("message = %q, want substring %q", shapeErr.Message, "must be an object")
	}
}

func TestExtractTaskInputs_WithPlanRef(t *testing.T) {
	raw := []any{
		map[string]any{
			"id":        "task-1",
			"desc":      "Do something",
			"spec":      "specs/vision.md",
			"done":      "Done",
			"scope":     "scope",
			"plan_ref":  "specs/plans/plan.md",
			"role_pair": "coding-pair",
		},
	}

	tasks, err := extractTaskInputs(raw)
	if err != nil {
		t.Fatalf("extractTaskInputs() error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].PlanRef != "specs/plans/plan.md" {
		t.Errorf("tasks[0].PlanRef = %q, want %q", tasks[0].PlanRef, "specs/plans/plan.md")
	}
}
