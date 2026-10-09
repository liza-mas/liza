package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
)

func TestValidationNotesBothRoleBlocksWithoutCanonicalCommands(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	for _, tc := range []struct{ role, section string }{{"coder", "assigned-task"}, {"code-reviewer", "review-task"}} {
		data := &RoleContextData{Role: tc.role, TaskID: "child", ValidationNotes: []models.ValidationNote{{ParentTask: "plan", OutputIndex: 2, Message: "Confirm expected hooks ran."}}}
		output, err := BuildRoleContext(tc.role, []string{tc.section}, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"ADVISORY VALIDATION NOTES", "plan output[2]: Confirm expected hooks ran.", "do not change canonical acceptance", "cannot substitute for missing producers, runtime_inputs or human provisioning"} {
			if !strings.Contains(output, want) {
				t.Errorf("%s missing %q", tc.role, want)
			}
		}
		for _, leaked := range []string{"Liza", "LIZA", "liza", "{{binaryName}}"} {
			if strings.Contains(output, leaked) {
				t.Errorf("nondefault notes render leaks %q", leaked)
			}
		}
		data.ValidationNotes = nil
		output, err = BuildRoleContext(tc.role, []string{tc.section}, data)
		if err != nil || strings.Contains(output, "ADVISORY VALIDATION NOTES") {
			t.Fatalf("legacy rendering: %s %v", output, err)
		}
	}
}
