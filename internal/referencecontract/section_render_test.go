package referencecontract

import (
	"strings"
	"testing"
)

func TestRenderExplicitSectionReadSet(t *testing.T) {
	t.Parallel()
	shared := "## Shared\nREQUIRED SHARED CONTRACT\n"
	selected := Reference{ID: "shared", Path: "master.md", Heading: "Shared", Revision: "pin", Span: shared}
	unselected := Reference{ID: "other", Path: "story.md", Heading: "Other", Revision: "pin", Span: "# Other\nUNASSIGNED STORY BULK\n"}
	carrier := Carrier{Path: "master.md", Revision: "head", Span: "# Master\n" + shared + "## Decomposition\n### Scope 1\nASSIGNED LOCAL CONTRACT\n### Scope 2\nSIBLING BULK\n",
		AssignedHeading: "Scope 1", SectionOnly: true, InlineReferenceIDs: map[string]bool{"shared": true}, Refs: []Reference{selected, unselected}}
	context, err := RenderCarriers([]Carrier{carrier, {Path: "story.md", Revision: "head", Span: unselected.Span, PointerOnly: true, ElideRefs: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"ASSIGNED LOCAL CONTRACT", "REQUIRED SHARED CONTRACT", `DIRECT REFERENCE "master.md#Shared" @ pin`, `UNSELECTED DIRECT REFERENCE IDS: "other"`, "git show 'head:master.md'", "git show 'head:story.md'"} {
		if !strings.Contains(context, required) {
			t.Errorf("missing %q in %s", required, context)
		}
	}
	for _, omitted := range []string{"SIBLING BULK", "UNASSIGNED STORY BULK"} {
		if strings.Contains(context, omitted) {
			t.Errorf("inlined unassigned %q", omitted)
		}
	}
}

func TestPointerCarrierDoesNotContainSelectedReference(t *testing.T) {
	t.Parallel()
	span := "# Requirement\nREQUIRED FACT\n"
	ref := Reference{ID: "required", Path: "ancestor.md", Heading: "Requirement", Revision: "pin", Span: span}
	carriers := []Carrier{
		{Path: "ancestor.md", Revision: "head", Span: span, PointerOnly: true, ElideRefs: true, Refs: []Reference{ref}},
		{Path: "assigned.md", Revision: "head", Span: "### Scope\nLOCAL\n", AssignedHeading: "Scope", SectionOnly: true, InlineReferenceIDs: map[string]bool{"required": true}, Refs: []Reference{ref}},
	}
	context, err := RenderCarriers(carriers)
	if err != nil || strings.Count(context, "REQUIRED FACT") != 1 {
		t.Fatalf("selected ancestor section must render once: error=%v\n%s", err, context)
	}
}

func TestCompactReferencesRetainIndividualDrift(t *testing.T) {
	t.Parallel()
	ref := Reference{ID: "drifted", Path: "other.md", Heading: "Other", Revision: "current", PinnedRevision: "old", Span: "# Other\nDRIFTED BULK\n"}
	context, err := RenderCarriers([]Carrier{{Path: "assigned.md", Revision: "head", Span: "### Scope\nLOCAL\n", AssignedHeading: "Scope", SectionOnly: true, InlineReferenceIDs: map[string]bool{}, Refs: []Reference{ref}}})
	if err != nil || strings.Contains(context, "DRIFTED BULK") || !strings.Contains(context, `DIRECT REFERENCE "other.md#Other" @ current`) || !strings.Contains(context, "changed since its pinned revision old") || !strings.Contains(context, "git diff 'old' 'current' -- 'other.md'") {
		t.Fatalf("individual drift disclosure lost: error=%v\n%s", err, context)
	}
	if !strings.Contains(context, "current section available via pointer") || strings.Contains(context, "current text shown") {
		t.Fatalf("pointer claimed absent text was inlined: %s", context)
	}
	ref.Revision, ref.Unresolved = "old", "path deleted"
	context, err = RenderCarriers([]Carrier{{Path: "assigned.md", Revision: "head", Span: "### Scope\nLOCAL\n", AssignedHeading: "Scope", SectionOnly: true, InlineReferenceIDs: map[string]bool{}, Refs: []Reference{ref}}})
	if err != nil || !strings.Contains(context, "pinned section available via pointer") || strings.Contains(context, "pinned text shown") || !strings.Contains(context, "git show 'old:other.md'") {
		t.Fatalf("unresolved pin navigation was not disclosed accurately: error=%v\n%s", err, context)
	}
}
