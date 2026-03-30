package pipeline

import (
	"slices"
	"testing"
)

// frozenConfigWithout loads the embedded reference and removes the named
// operation from every role, simulating a frozen config from an older version.
func frozenConfigWithout(t *testing.T, op string) *PipelineConfig {
	t.Helper()
	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}
	for name, role := range ref.Pipeline.Roles {
		role.AllowedOperations = slices.DeleteFunc(role.AllowedOperations, func(s string) bool {
			return s == op
		})
		ref.Pipeline.Roles[name] = role
	}
	return ref
}

func TestMigrateOperations_AddsNewOperation(t *testing.T) {
	frozen := frozenConfigWithout(t, "await-verdict")

	// Verify precondition: coder role lacks await-verdict.
	coder := frozen.Pipeline.Roles["coder"]
	if slices.Contains(coder.AllowedOperations, "await-verdict") {
		t.Fatal("precondition: frozen coder should not have await-verdict")
	}
	originalOps := make([]string, len(coder.AllowedOperations))
	copy(originalOps, coder.AllowedOperations)

	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}

	changed := MigrateOperations(frozen, ref)
	if !changed {
		t.Error("MigrateOperations returned false, want true")
	}

	coder = frozen.Pipeline.Roles["coder"]
	if !slices.Contains(coder.AllowedOperations, "await-verdict") {
		t.Error("await-verdict not added to frozen coder role")
	}

	// All original operations must still be present.
	for _, op := range originalOps {
		if !slices.Contains(coder.AllowedOperations, op) {
			t.Errorf("original operation %q was removed", op)
		}
	}
}

func TestMigrateOperations_NoopWhenPresent(t *testing.T) {
	// Load reference as "frozen" — already has await-verdict in every role.
	frozen, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}
	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}

	changed := MigrateOperations(frozen, ref)
	if changed {
		t.Error("MigrateOperations returned true, want false (no-op)")
	}
}

func TestMigrateOperations_AddsAwaitResubmission(t *testing.T) {
	frozen := frozenConfigWithout(t, "await-resubmission")

	// Verify precondition: code-reviewer role lacks await-resubmission.
	cr := frozen.Pipeline.Roles["code-reviewer"]
	if slices.Contains(cr.AllowedOperations, "await-resubmission") {
		t.Fatal("precondition: frozen code-reviewer should not have await-resubmission")
	}
	originalOps := make([]string, len(cr.AllowedOperations))
	copy(originalOps, cr.AllowedOperations)

	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}

	changed := MigrateOperations(frozen, ref)
	if !changed {
		t.Error("MigrateOperations returned false, want true")
	}

	cr = frozen.Pipeline.Roles["code-reviewer"]
	if !slices.Contains(cr.AllowedOperations, "await-resubmission") {
		t.Error("await-resubmission not added to frozen code-reviewer role")
	}

	// All original operations must still be present.
	for _, op := range originalOps {
		if !slices.Contains(cr.AllowedOperations, op) {
			t.Errorf("original operation %q was removed", op)
		}
	}
}

func TestMigrateOperations_AllReviewerRoles_AwaitResubmission(t *testing.T) {
	frozen := frozenConfigWithout(t, "await-resubmission")
	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}

	reviewerRoles := []string{"code-reviewer", "code-plan-reviewer", "epic-plan-reviewer", "us-reviewer"}

	// Verify precondition: none of the reviewer roles have await-resubmission.
	for _, role := range reviewerRoles {
		r := frozen.Pipeline.Roles[role]
		if slices.Contains(r.AllowedOperations, "await-resubmission") {
			t.Fatalf("precondition: frozen %s should not have await-resubmission", role)
		}
	}

	changed := MigrateOperations(frozen, ref)
	if !changed {
		t.Fatal("MigrateOperations returned false, want true")
	}

	for _, role := range reviewerRoles {
		r := frozen.Pipeline.Roles[role]
		if !slices.Contains(r.AllowedOperations, "await-resubmission") {
			t.Errorf("await-resubmission not added to frozen %s role", role)
		}
	}
}

func TestMigrateOperations_AllDoerRoles(t *testing.T) {
	frozen := frozenConfigWithout(t, "await-verdict")
	ref, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference: %v", err)
	}

	doerRoles := []string{"coder", "code-planner", "epic-planner", "us-writer"}

	// Verify precondition: none of the doer roles have await-verdict.
	for _, role := range doerRoles {
		r := frozen.Pipeline.Roles[role]
		if slices.Contains(r.AllowedOperations, "await-verdict") {
			t.Fatalf("precondition: frozen %s should not have await-verdict", role)
		}
	}

	changed := MigrateOperations(frozen, ref)
	if !changed {
		t.Fatal("MigrateOperations returned false, want true")
	}

	for _, role := range doerRoles {
		r := frozen.Pipeline.Roles[role]
		if !slices.Contains(r.AllowedOperations, "await-verdict") {
			t.Errorf("await-verdict not added to frozen %s role", role)
		}
	}
}
