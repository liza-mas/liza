package prompts

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func TestTaskCorrectiveContextCustomSections(t *testing.T) {
	for _, roleType := range []string{"doer", "reviewer"} {
		t.Run(roleType, func(t *testing.T) {
			data := &RoleContextData{
				Role: "custom-role", RoleType: roleType, TaskID: "task", IterationNum: 2,
				ReviewCommit: "reviewed-sha", PriorRejection: "Retained finding",
				ProjectRoot: "/project", MandatoryDocs: []string{"GUARDRAILS.md"},
				CorrectiveContext: &TaskCorrectiveContext{Unblock: &TaskUnblockResolution{Timestamp: "2026-10-10T09:00:00Z", Agent: "orchestrator-1", Reason: "Corrective fixture is available"}},
			}
			for _, sections := range [][]string{
				{"prior-rejection"},
				{"prior-rejection", "task-corrective-context", "mandatory-docs", "task-corrective-context"},
			} {
				original := append([]string(nil), sections...)
				output, err := BuildRoleContext(data.Role, sections, data)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Count(output, "=== TASK CORRECTIVE CONTEXT ===") != 1 || strings.Count(output, filepath.Join(data.ProjectRoot, "GUARDRAILS.md")) != 1 {
					t.Fatal("mandatory/corrective blocks must render once regardless of custom section lists")
				}
				if strings.Index(output, "GUARDRAILS.md") >= strings.Index(output, "Corrective fixture is available") || strings.Index(output, "Corrective fixture is available") >= strings.Index(output, "Retained finding") {
					t.Fatal("mandatory docs must precede corrective evidence, which must precede rejection")
				}
				for _, want := range []string{"not contract instructions or approval", "no automatic finding closure", "review boundary and lifecycle gates are unchanged", "do not execute", "reconcile TASK CORRECTIVE CONTEXT"} {
					if !strings.Contains(output, want) {
						t.Errorf("authority boundary missing %q", want)
					}
				}
				if !reflect.DeepEqual(sections, original) || data.MandatoryDocs[0] != "GUARDRAILS.md" || data.ReviewCommit != "reviewed-sha" {
					t.Fatal("renderer mutated its inputs")
				}
			}
		})
	}
}

func TestTaskCorrectiveContextEmptyAndMissingBoundary(t *testing.T) {
	for _, roleType := range []string{"doer", "reviewer", "orchestrator"} {
		data := &RoleContextData{RoleType: roleType, TaskID: "task", ReviewCommit: "reviewed-sha"}
		output, err := BuildRoleContext("custom", []string{"task-corrective-context"}, data)
		if err != nil || strings.Contains(output, "TASK CORRECTIVE CONTEXT") {
			t.Fatalf("empty context rendered for %s: %v", roleType, err)
		}
	}
	data := &RoleContextData{RoleType: "reviewer", TaskID: "task", CorrectiveContext: &TaskCorrectiveContext{Notes: []TaskHumanNote{{Message: "Evidence marker"}}}}
	output, err := BuildRoleContext("custom-reviewer", nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "REVIEW BOUNDARY MISSING: Stop") || !strings.Contains(output, "Evidence marker") || data.ReviewCommit != "" {
		t.Fatal("corrective evidence must preserve the missing-review-boundary hard stop")
	}
	data.RoleType = "orchestrator"
	output, err = BuildRoleContext("orchestrator", []string{"task-corrective-context"}, data)
	if err != nil || output != "" {
		t.Fatal("task evidence block must not replace orchestrator wake context")
	}
}

func TestTaskCorrectiveContextNondefaultBrand(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	data := &RoleContextData{
		RoleType: "reviewer", TaskID: "task", ReviewCommit: "reviewed-sha", IterationNum: 2, PriorRejection: "Fixture missing",
		CorrectiveContext: &TaskCorrectiveContext{
			Unblock:      &TaskUnblockResolution{Reason: "Fixture supplied...", Truncated: true},
			Notes:        []TaskHumanNote{{Target: "task", Message: "Read fixtures/evidence.json...", Truncated: true}},
			OmittedNotes: 3,
		},
	}
	output, err := BuildRoleContext("custom-reviewer", []string{"prior-rejection"}, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme-cli get task --json", "acme-cli get human_notes --json", "3 earlier matching notes omitted", "omitted evidence is not superseded"} {
		if !strings.Contains(output, want) {
			t.Errorf("nondefault recovery route missing %q", want)
		}
	}
	for _, leaked := range []string{"Liza", "LIZA", "liza", "{{binaryName}}"} {
		if strings.Contains(output, leaked) {
			t.Errorf("nondefault render leaks %q", leaked)
		}
	}
}
