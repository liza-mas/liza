package prompts

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMandatoryDocsRoleContext(t *testing.T) {
	projectRoot := t.TempDir()
	absoluteDoc := filepath.Join(t.TempDir(), "external-protocol.md")
	relativeDoc := filepath.Join("specs", "quality-protocol.md")
	for _, rootCase := range []struct {
		name     string
		worktree string
		wantRoot string
	}{
		{name: "project", wantRoot: projectRoot},
		{name: "worktree", worktree: filepath.Join(projectRoot, ".worktrees", "task"), wantRoot: filepath.Join(projectRoot, ".worktrees", "task")},
	} {
		for _, sectionCase := range []struct {
			name     string
			sections []string
		}{
			{name: "omitted", sections: []string{"skills-affinity"}},
			{name: "trailing", sections: []string{"skills-affinity", "mandatory-docs"}},
			{name: "repeated", sections: []string{"mandatory-docs", "skills-affinity", "mandatory-docs"}},
			{name: "no sections"},
		} {
			t.Run(rootCase.name+"/"+sectionCase.name, func(t *testing.T) {
				docs := []string{relativeDoc, absoluteDoc}
				data := RoleContextData{RoleType: "doer", ProjectRoot: projectRoot, Worktree: rootCase.worktree, MandatoryDocs: slices.Clone(docs), Skills: []string{"testing"}}
				sectionsBefore := slices.Clone(sectionCase.sections)
				context, err := BuildRoleContext("custom-doer", sectionCase.sections, &data)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(strings.TrimSpace(context), "=== MANDATORY DOCUMENTS ===") {
					t.Errorf("mandatory documents must precede role instructions: %s", context)
				}
				if strings.Count(context, "=== MANDATORY DOCUMENTS ===") != 1 {
					t.Errorf("expected exactly one document block: %s", context)
				}
				for _, want := range []string{
					"Read every listed file in full", "every session before role work",
					"cannot be read, stop", "path and error", "Never skip",
					"- " + filepath.Join(rootCase.wantRoot, relativeDoc), "- " + absoluteDoc,
				} {
					if !strings.Contains(context, want) {
						t.Errorf("missing required document guidance %q: %s", want, context)
					}
				}
				if !slices.Equal(data.MandatoryDocs, docs) || !slices.Equal(sectionCase.sections, sectionsBefore) {
					t.Fatal("rendering mutated caller document paths or section order")
				}
			})
		}
	}
}

func TestMandatoryDocsOnlyNonemptyBlock(t *testing.T) {
	data := RoleContextData{ProjectRoot: t.TempDir(), MandatoryDocs: []string{"protocol.md"}}
	context, err := BuildRoleContext("custom-doer", []string{"skills-affinity"}, &data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(context, "MANDATORY DOCUMENTS") || !strings.Contains(context, filepath.Join(data.ProjectRoot, "protocol.md")) {
		t.Fatalf("mandatory documents disappeared when other sections were empty: %q", context)
	}
}

func TestMandatoryDocsAfterMissingReviewBoundary(t *testing.T) {
	data := RoleContextData{RoleType: "reviewer", ProjectRoot: t.TempDir(), MandatoryDocs: []string{"protocol.md"}}
	context, err := BuildRoleContext("custom-reviewer", nil, &data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.TrimSpace(context), "REVIEW BOUNDARY MISSING: Stop;") {
		t.Fatalf("missing review boundary must remain first: %s", context)
	}
	if strings.Index(context, "=== MANDATORY DOCUMENTS ===") <= strings.Index(context, "REVIEW BOUNDARY MISSING:") {
		t.Fatalf("mandatory documents must follow the review stop: %s", context)
	}
	if data.ReviewCommit != "" {
		t.Fatal("rendering mutated caller review boundary")
	}
}

func TestMandatoryDocsEmpty(t *testing.T) {
	for _, docs := range [][]string{nil, {}} {
		data := RoleContextData{RoleType: "doer", MandatoryDocs: docs, Skills: []string{"testing"}}
		context, err := BuildRoleContext("custom-doer", []string{"mandatory-docs", "skills-affinity"}, &data)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(context, "MANDATORY DOCUMENTS") || strings.Contains(context, "Read every listed") {
			t.Errorf("empty list must omit document instructions: %s", context)
		}
		if !strings.Contains(context, "SKILLS AFFINITY") {
			t.Errorf("empty list lost other sections: %s", context)
		}
	}
}
