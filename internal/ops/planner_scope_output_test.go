package ops

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestArchitectureOutputRequiresStrictScopeAnchor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, fragment, errorText string
		legacy, missing, absent   bool
	}{
		{name: "missing ref", missing: true, errorText: "output[0].arch_ref is required"},
		{name: "bare strict", errorText: "output[0].arch_ref requires an exact Scope heading"},
		{name: "exact strict", fragment: "#Task One"},
		{name: "slug strict", fragment: "#task-one", errorText: "fragment must be the exact heading"},
		{name: "new legacy output still needs anchor", legacy: true, errorText: "output[0].arch_ref requires an exact Scope heading"},
		{name: "legacy anchored output remains readable", legacy: true, fragment: "#Task One"},
		{name: "slug legacy output refused", legacy: true, fragment: "#task-one", errorText: "fragment must be the exact heading"},
		{name: "missing legacy heading refused", legacy: true, fragment: "#Missing Scope", errorText: "fragment must be the exact heading"},
		{name: "missing artifact refused", absent: true, fragment: "#Task One", errorText: "output[0].arch_ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, taskID, commit, _, _ := setupPlanningAcceptanceSubmission(t, "", tc.legacy)
			ref := "specs/plans/child.md" + tc.fragment
			if tc.missing {
				ref = ""
			}
			if tc.absent {
				ref = "specs/plans/absent.md" + tc.fragment
			}
			task := &models.Task{ID: taskID, Type: models.TaskTypeArchitecture,
				Output: []models.OutputEntry{{ArchRef: ref}}}
			err := validateOutputRefFragments(root, task, commit)
			if tc.errorText == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("error = %v, want %q", err, tc.errorText)
			}
		})
	}
}

func TestArchitectureValidatedOutputCannotChangeBeforePublication(t *testing.T) {
	t.Parallel()
	expected := models.Task{ID: "architect-1", Type: models.TaskTypeArchitecture,
		Output: []models.OutputEntry{{ArchRef: "specs/architecture.md#Scope 1"}}}
	current := expected
	current.Output = []models.OutputEntry{{ArchRef: "specs/architecture.md"}}
	if err := checkPlanningOutputSnapshot(&expected, &current); err == nil {
		t.Fatal("a validated anchored assignment was replaced by a bare ref before publication")
	}
	if err := checkPlanningOutputSnapshot(&expected, &expected); err != nil {
		t.Fatalf("unchanged validated architecture allocation refused: %v", err)
	}
}
