package referencecontract

import (
	"strings"
	"testing"
)

const (
	renderCarrierBody = "# Doc\n\n## Contract\ncontract text\n\n## Other\nother text\n"
	renderSection     = "## Contract\ncontract text\n"
)

// A direct reference into a carrier already inlined at the same blob is
// rendered as a pointer, not a second copy of the section.
func TestRenderCarriersElidesReferenceInlinedAsCarrier(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/a.md", Span: renderCarrierBody, Revision: "head", Class: CarrierParent, BlobOID: "blob-a"},
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b",
			Refs: []Reference{{Path: "specs/a.md", Heading: "Contract", Revision: "head", BlobOID: "blob-a", Span: renderSection}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}

	if strings.Count(out, "contract text") != 1 {
		t.Errorf("section text appears %d times, want exactly once (in the carrier):\n%s",
			strings.Count(out, "contract text"), out)
	}
	pointer := `DIRECT REFERENCE "specs/a.md#Contract" @ head — inlined in this context as CARRIER "specs/a.md"`
	if !strings.Contains(out, pointer) {
		t.Errorf("elided reference lost its pointer line; the reference must stay discoverable:\n%s", out)
	}
}

// A reference pinned at a different blob than the inlined carrier carries
// different text and must be emitted in full. Path equality alone is not a
// duplicate.
func TestRenderCarriersKeepsStaleReferenceInFull(t *testing.T) {
	t.Parallel()

	staleSection := "## Contract\nearlier contract text\n"
	out, err := RenderCarriers([]Carrier{
		{Path: "specs/a.md", Span: renderCarrierBody, Revision: "head", Class: CarrierParent, BlobOID: "blob-a"},
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b",
			Refs: []Reference{{Path: "specs/a.md", Heading: "Contract", Revision: "older", BlobOID: "blob-a-old", Span: staleSection}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}

	if !strings.Contains(out, "earlier contract text") {
		t.Errorf("stale reference was elided; its text differs from the inlined carrier and must be kept:\n%s", out)
	}
	if strings.Contains(out, "inlined in this context as CARRIER") {
		t.Errorf("stale reference rendered as a pointer to a carrier with different content:\n%s", out)
	}
}

// Equal OID but a span that is somehow not contained falls through to a full
// emit. This should not happen — at an equal blob the section is a substring
// by construction — but the guard fails safe rather than trusting the OID.
func TestRenderCarriersFallsThroughWhenNotContained(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/a.md", Span: renderCarrierBody, Revision: "head", Class: CarrierParent, BlobOID: "blob-a"},
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b",
			Refs: []Reference{{Path: "specs/a.md", Heading: "Contract", Revision: "head", BlobOID: "blob-a", Span: "## Contract\nnot actually in the carrier\n"}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(out, "not actually in the carrier") {
		t.Errorf("non-contained reference was elided on OID alone:\n%s", out)
	}
}

// A reference into a path that is not inlined as a carrier is unaffected.
func TestRenderCarriersEmitsExternalReferenceInFull(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b",
			Refs: []Reference{{Path: "specs/elsewhere.md", Heading: "Contract", Revision: "head", BlobOID: "blob-e", Span: renderSection}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(out, "contract text") {
		t.Errorf("reference to a non-inlined path was dropped:\n%s", out)
	}
}

// Byte delta on a shape like the measured run: several references into one
// inlined carrier. The number matters less than that it is measured here
// rather than asserted.
func TestRenderCarriersReductionIsMeasured(t *testing.T) {
	t.Parallel()

	var body strings.Builder
	body.WriteString("# Doc\n")
	var refs []Reference
	for i := range 12 {
		section := strings.Repeat("x", 1700)
		text := "\n## S" + string(rune('a'+i)) + "\n" + section + "\n"
		body.WriteString(text)
		refs = append(refs, Reference{Path: "specs/a.md", Heading: "S" + string(rune('a'+i)), Revision: "head", BlobOID: "blob-a", Span: text})
	}
	carriers := []Carrier{
		{Path: "specs/a.md", Span: body.String(), Revision: "head", Class: CarrierParent, BlobOID: "blob-a"},
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b", Refs: refs},
	}

	after, err := RenderCarriers(carriers)
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}

	// A true pre-reduction render: the inlined carrier's body is replaced by
	// filler of exactly the same length, so no reference span is contained in
	// it and elision cannot fire, while both renders stay byte-comparable.
	unelided := make([]Carrier, len(carriers))
	copy(unelided, carriers)
	unelided[0].Span = strings.Repeat("y", len(body.String())-1) + "\n"
	before, err := RenderCarriers(unelided)
	if err != nil {
		t.Fatalf("RenderCarriers (unelided): %v", err)
	}

	saved := len(before) - len(after)
	t.Logf("saved %d bytes of %d (%.1f%%) on 12 duplicated references", saved, len(before), 100*float64(saved)/float64(len(before)))

	// Each elision replaces "header + section body" with one pointer line.
	var want int
	for _, r := range refs {
		full := len(`DIRECT REFERENCE "specs/a.md#`+r.Heading+`" @ head`+"\n") + len(r.Span)
		pointer := len(`DIRECT REFERENCE "specs/a.md#` + r.Heading + `" @ head — inlined in this context as CARRIER "specs/a.md"` + "\n")
		want += full - pointer
	}
	if saved != want {
		t.Errorf("saved %d bytes, want exactly %d", saved, want)
	}
}

// An elided carrier's reference to a document inlined nowhere is a pointer
// to its pinned revision; the span is not rendered.
func TestRenderCarriersElidedReferenceIsPointerToRevision(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/epic.md", Span: "epic body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-e", ElideRefs: true,
			Refs: []Reference{{Path: "specs/goal.md", Heading: "Scope", Revision: "pinned", BlobOID: "blob-g", Span: "## Scope\nGOAL-SCOPE-TEXT\n"}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if strings.Contains(out, "GOAL-SCOPE-TEXT") {
		t.Fatalf("elided reference span rendered:\n%s", out)
	}
	want := `DIRECT REFERENCE "specs/goal.md#Scope" @ pinned — not inlined; read with git show 'pinned:specs/goal.md' if needed`
	if !strings.Contains(out, want) {
		t.Fatalf("missing pointer %q in:\n%s", want, out)
	}
}

// A reference declared by both an elided and a full carrier is rendered in
// full exactly once, by the full carrier, even when the elided carrier sorts
// first — a pointer must not consume the once-only slot.
func TestRenderCarriersSharedReferenceStaysFullOnce(t *testing.T) {
	t.Parallel()

	shared := Reference{Path: "specs/goal.md", Heading: "Scope", Revision: "pinned", BlobOID: "blob-g", Span: "## Scope\nGOAL-SCOPE-TEXT\n"}
	out, err := RenderCarriers([]Carrier{
		{Path: "specs/a-epic.md", Span: "epic body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-e", ElideRefs: true, Refs: []Reference{shared}},
		{Path: "specs/b-epic.md", Span: "second epic body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-e2", ElideRefs: true, Refs: []Reference{shared}},
		{Path: "specs/plan.md", Span: "plan body\n", Revision: "review", Class: CarrierParent, BlobOID: "blob-p", Refs: []Reference{shared}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if count := strings.Count(out, "GOAL-SCOPE-TEXT"); count != 1 {
		t.Fatalf("shared reference rendered %d times, want once:\n%s", count, out)
	}
	pointer := `DIRECT REFERENCE "specs/goal.md#Scope" @ pinned — inlined in this context under CARRIER "specs/plan.md"`
	if count := strings.Count(out, pointer); count != 1 {
		t.Fatalf("pointer rendered %d times, want once:\n%s", count, out)
	}
	if strings.Index(out, pointer) > strings.Index(out, "GOAL-SCOPE-TEXT") {
		t.Fatalf("pointer should precede the full emission it names:\n%s", out)
	}
}

// The dedup key includes the blob: the same path#heading pinned at different
// blobs by an elided and a full carrier renders as a pointer and a span.
func TestRenderCarriersDifferentBlobsAreDistinctReferences(t *testing.T) {
	t.Parallel()

	older := Reference{Path: "specs/goal.md", Heading: "Scope", Revision: "older", BlobOID: "blob-old", Span: "## Scope\nOLD-SCOPE-TEXT\n"}
	newer := Reference{Path: "specs/goal.md", Heading: "Scope", Revision: "newer", BlobOID: "blob-new", Span: "## Scope\nNEW-SCOPE-TEXT\n"}
	out, err := RenderCarriers([]Carrier{
		{Path: "specs/epic.md", Span: "epic body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-e", ElideRefs: true, Refs: []Reference{older}},
		{Path: "specs/plan.md", Span: "plan body\n", Revision: "review", Class: CarrierParent, BlobOID: "blob-p", Refs: []Reference{newer}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if strings.Contains(out, "OLD-SCOPE-TEXT") {
		t.Fatalf("elided older revision rendered in full:\n%s", out)
	}
	if !strings.Contains(out, `"specs/goal.md#Scope" @ older — not inlined; read with git show 'older:specs/goal.md'`) {
		t.Fatalf("older revision should be a revision pointer:\n%s", out)
	}
	if strings.Count(out, "NEW-SCOPE-TEXT") != 1 {
		t.Fatalf("newer revision should render in full once:\n%s", out)
	}
}

// Same-path observations collapse to the highest class, and the winner's
// ElideRefs rules: a scalar observation marked elided loses to the parent
// observation of the same path, whose references render in full.
func TestRenderCarriersWinnerDecidesElision(t *testing.T) {
	t.Parallel()

	ref := Reference{Path: "specs/goal.md", Heading: "Scope", Revision: "pinned", BlobOID: "blob-g", Span: "## Scope\nGOAL-SCOPE-TEXT\n"}
	out, err := RenderCarriers([]Carrier{
		{Path: "specs/plan.md", Span: "plan at head\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-p", ElideRefs: true, Refs: []Reference{ref}},
		{Path: "specs/plan.md", Span: "plan at review\n", Revision: "review", Class: CarrierParent, BlobOID: "blob-p", Refs: []Reference{ref}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if strings.Count(out, "GOAL-SCOPE-TEXT") != 1 || strings.Contains(out, "not inlined") {
		t.Fatalf("parent winner's reference should render in full:\n%s", out)
	}
}

// The saving from eliding ancestor references is exactly header+span minus
// the pointer line, per reference, with everything else byte-identical.
func TestRenderCarriersAncestorElisionIsMeasured(t *testing.T) {
	t.Parallel()

	var refs []Reference
	for i := range 13 {
		heading := "S" + string(rune('a'+i))
		refs = append(refs, Reference{Path: "specs/goal.md", Heading: heading, Revision: "pinned", BlobOID: "blob-g",
			Span: "## " + heading + "\n" + strings.Repeat("x", 5200) + "\n"})
	}
	full := []Carrier{
		{Path: "specs/epic.md", Span: "epic body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-e", Refs: refs},
		{Path: "specs/plan.md", Span: "plan body\n", Revision: "review", Class: CarrierParent, BlobOID: "blob-p"},
	}
	before, err := RenderCarriers(full)
	if err != nil {
		t.Fatalf("RenderCarriers (full): %v", err)
	}
	elided := make([]Carrier, len(full))
	copy(elided, full)
	elided[0].ElideRefs = true
	after, err := RenderCarriers(elided)
	if err != nil {
		t.Fatalf("RenderCarriers (elided): %v", err)
	}

	var want int
	for _, r := range refs {
		header := len(`DIRECT REFERENCE "specs/goal.md#`+r.Heading+`" @ pinned`) + 1
		pointer := len(`DIRECT REFERENCE "specs/goal.md#`+r.Heading+`" @ pinned — not inlined; read with git show 'pinned:specs/goal.md' if needed`) + 1
		want += header + len(r.Span) - pointer
	}
	if saved := len(before) - len(after); saved != want {
		t.Errorf("saved %d bytes, want exactly %d", saved, want)
	}
	if !strings.HasSuffix(after, "CARRIER \"specs/plan.md\" @ review\nplan body") {
		t.Errorf("non-elided tail changed:\n%s", after)
	}
}

// A plan carrier: shared analysis and design sections, then one section per
// task. Task 2 is the assigned one; Task 1 and Task 3 are its peers.
const renderPlanBody = `# Code Plan

## Root Cause Analysis

### a. Evidence
evidence text

## Design

### Payload parity (Tasks 2-3)
design text the assigned task needs

## Tasks

### Task 1: first
first body

### Task 2: second
second body

### Task 3: third
third body

## Out of Scope
scope text
`

// The whole plan arrives through a merged parent's reviewed range, but the
// task was pointed at one section of it. Its peers become pointers; the
// analysis and design sections they share stay inlined, because a task's
// design context lives outside its own section.
func TestRenderCarriersElidesPeersOfAssignedSection(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/plan.md", Span: renderPlanBody, Revision: "head", Class: CarrierParent,
			BlobOID: "blob-plan", AssignedHeading: "Task 2: second"},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}

	for _, kept := range []string{"second body", "evidence text", "design text the assigned task needs", "scope text"} {
		if !strings.Contains(out, kept) {
			t.Errorf("elision dropped %q, which is not a peer of the assigned section:\n%s", kept, out)
		}
	}
	for _, elided := range []string{"first body", "third body"} {
		if strings.Contains(out, elided) {
			t.Errorf("peer section %q was still inlined:\n%s", elided, out)
		}
	}
	pointer := `SECTION "specs/plan.md#Task 1: first" @ head — peer of your assigned section; not inlined; read with git show 'head:specs/plan.md' if needed`
	if !strings.Contains(out, pointer) {
		t.Errorf("elided peer lost its pointer line; the section must stay discoverable:\n%s", out)
	}
}

// Without an assigned heading there is nothing to narrow against, so the
// carrier renders exactly as it did before assignment was plumbed through.
func TestRenderCarriersKeepsWholeSpanWithoutAssignedHeading(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/plan.md", Span: renderPlanBody, Revision: "head", Class: CarrierParent, BlobOID: "blob-plan"},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(out, "first body") || !strings.Contains(out, "third body") {
		t.Errorf("carrier was narrowed without an assigned heading:\n%s", out)
	}
}

// Narrowing on a heading that does not resolve uniquely would drop sections
// the task needs, so an unresolvable assignment renders the carrier whole.
func TestRenderCarriersKeepsWholeSpanWhenAssignedHeadingMissing(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/plan.md", Span: renderPlanBody, Revision: "head", Class: CarrierParent,
			BlobOID: "blob-plan", AssignedHeading: "Task 9: renamed away"},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(out, "first body") || !strings.Contains(out, "third body") {
		t.Errorf("carrier was narrowed against a heading it does not contain:\n%s", out)
	}
}

// Duplicate detection runs on the narrowed span. A reference whose text
// survived only inside an elided peer is no longer present in this context,
// so it must be emitted in full rather than pointing at a missing section.
func TestRenderCarriersEmitsReferenceLostToElidedPeer(t *testing.T) {
	t.Parallel()

	peerSpan := "### Task 3: third\nthird body\n"
	out, err := RenderCarriers([]Carrier{
		{Path: "specs/plan.md", Span: renderPlanBody, Revision: "head", Class: CarrierParent,
			BlobOID: "blob-plan", AssignedHeading: "Task 2: second"},
		{Path: "specs/other.md", Span: "other body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-other",
			Refs: []Reference{{Path: "specs/plan.md", Heading: "Task 3: third", Revision: "head", BlobOID: "blob-plan", Span: peerSpan}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(out, "third body") {
		t.Errorf("reference into an elided peer was dropped instead of emitted in full:\n%s", out)
	}
	if strings.Contains(out, `DIRECT REFERENCE "specs/plan.md#Task 3: third" @ head — inlined in this context`) {
		t.Errorf("reference points at a section this context no longer inlines:\n%s", out)
	}
}

// A carrier path is checked for separators, control characters and a .md
// extension — not for shell metacharacters. Both pointer lines name a Git
// object the agent is told to read, so an accepted name like "$(id).md" must
// arrive at the shell as text rather than as a command to run.
func TestRenderCarriersQuotesGeneratedGitCommands(t *testing.T) {
	t.Parallel()

	hostile := "specs/$(id)`whoami`it's.md"
	out, err := RenderCarriers([]Carrier{
		{Path: hostile, Span: renderPlanBody, Revision: "head", Class: CarrierParent,
			BlobOID: "blob-plan", AssignedHeading: "Task 2: second"},
		{Path: "specs/other.md", Span: "other body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-other",
			ElideRefs: true,
			Refs:      []Reference{{Path: hostile, Heading: "Absent", Revision: "pinned", BlobOID: "blob-x", Span: "## Absent\nabsent\n"}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}

	for _, want := range []string{
		`git show 'head:specs/$(id)` + "`whoami`" + `it'\''s.md' if needed`,
		`git show 'pinned:specs/$(id)` + "`whoami`" + `it'\''s.md' if needed`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated command is not single-quoted; want %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `git show "`) {
		t.Errorf("a git show argument is double-quoted, leaving $() and backticks active:\n%s", out)
	}
}

// A reference with no drift renders exactly as before drift disclosure
// existed: the note is additive and never touches an undrifted emission.
func TestRenderCarriersUndriftedOutputIsUnchanged(t *testing.T) {
	t.Parallel()

	out, err := RenderCarriers([]Carrier{
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b",
			Refs: []Reference{{Path: "specs/src.md", Heading: "Contract", Revision: "pinned", BlobOID: "blob-s", Span: renderSection}}},
		{Path: "specs/c.md", Span: "c body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-c", ElideRefs: true,
			Refs: []Reference{{Path: "specs/goal.md", Heading: "Scope", Revision: "pinned", BlobOID: "blob-g", Span: "## Scope\nscope\n"}}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	want := "CARRIER \"specs/b.md\" @ head\nb body\n" +
		"DIRECT REFERENCE \"specs/src.md#Contract\" @ pinned\n## Contract\ncontract text\n" +
		"CARRIER \"specs/c.md\" @ head\nc body\n" +
		"DIRECT REFERENCE \"specs/goal.md#Scope\" @ pinned — not inlined; read with git show 'pinned:specs/goal.md' if needed"
	if out != want {
		t.Fatalf("undrifted render changed:\n got: %q\nwant: %q", out, want)
	}
}

// A drifted reference carries its disclosure in every form it renders: in
// full, as an elided pointer, and as a pointer to an inlined carrier; and an
// unresolvable section says the pinned text is shown.
func TestRenderCarriersDisclosesDriftedReference(t *testing.T) {
	t.Parallel()

	drifted := Reference{Path: "specs/src.md", Heading: "Contract", Revision: "head", BlobOID: "blob-new",
		Span: "## Contract\nextended contract text\n", PinnedRevision: "pinned"}
	note := "section changed since its pinned revision pinned; current text shown; compare: git diff 'pinned' 'head' -- 'specs/src.md'"

	full, err := RenderCarriers([]Carrier{
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b", Refs: []Reference{drifted}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if want := "DIRECT REFERENCE \"specs/src.md#Contract\" @ head — " + note + "\n## Contract\nextended contract text"; !strings.Contains(full, want) {
		t.Errorf("full form lacks the drift disclosure; want %q in:\n%s", want, full)
	}

	elided, err := RenderCarriers([]Carrier{
		{Path: "specs/c.md", Span: "c body\n", Revision: "head", Class: CarrierScalar, BlobOID: "blob-c", ElideRefs: true, Refs: []Reference{drifted}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(elided, "not inlined; read with git show 'head:specs/src.md' if needed; "+note) {
		t.Errorf("elided pointer lacks the drift disclosure:\n%s", elided)
	}

	inlined, err := RenderCarriers([]Carrier{
		{Path: "specs/src.md", Span: "# Src\n\n## Contract\nextended contract text\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-new"},
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b", Refs: []Reference{drifted}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if !strings.Contains(inlined, "inlined in this context as CARRIER \"specs/src.md\"; "+note) {
		t.Errorf("inlined-carrier pointer lacks the drift disclosure:\n%s", inlined)
	}

	unresolved := Reference{Path: "specs/src.md", Heading: "Contract", Revision: "pinned", BlobOID: "blob-old",
		Span: "## Contract\ncontract text\n", PinnedRevision: "pinned", Unresolved: "heading not found"}
	gone, err := RenderCarriers([]Carrier{
		{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b", Refs: []Reference{unresolved}},
	})
	if err != nil {
		t.Fatalf("RenderCarriers: %v", err)
	}
	if want := "DIRECT REFERENCE \"specs/src.md#Contract\" @ pinned — section no longer resolves at integration HEAD (heading not found); pinned text shown\n## Contract\ncontract text"; !strings.Contains(gone, want) {
		t.Errorf("unresolvable form lacks its disclosure; want %q in:\n%s", want, gone)
	}
}

// Two carriers cite one section: one pin is current, the other was left
// behind and now resolves to the same text. The text renders once whichever
// sorts first, and the drift note renders exactly once — never swallowed by
// the current-pin emission, never duplicated onto it.
func TestRenderCarriersKeepsDriftNoteWhenSameSectionIsCurrentElsewhere(t *testing.T) {
	t.Parallel()

	current := Reference{Path: "specs/src.md", Heading: "Contract", Revision: "head", BlobOID: "blob-new", Span: "## Contract\nSHARED-TEXT\n"}
	drifted := current
	drifted.PinnedRevision = "pinned"
	for _, order := range []struct {
		name           string
		first, second  Reference
		pointerEmitter string
	}{
		{name: "current pin sorts first", first: current, second: drifted, pointerEmitter: "specs/a.md"},
		{name: "drifted pin sorts first", first: drifted, second: current},
	} {
		out, err := RenderCarriers([]Carrier{
			{Path: "specs/a.md", Span: "a body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-a", Refs: []Reference{order.first}},
			{Path: "specs/b.md", Span: "b body\n", Revision: "head", Class: CarrierParent, BlobOID: "blob-b", Refs: []Reference{order.second}},
		})
		if err != nil {
			t.Fatalf("%s: RenderCarriers: %v", order.name, err)
		}
		if count := strings.Count(out, "SHARED-TEXT"); count != 1 {
			t.Errorf("%s: section text rendered %d times, want once:\n%s", order.name, count, out)
		}
		if count := strings.Count(out, "section changed since its pinned revision pinned"); count != 1 {
			t.Errorf("%s: drift note rendered %d times, want once:\n%s", order.name, count, out)
		}
		if order.pointerEmitter != "" {
			want := "; same text inlined in this context under CARRIER \"" + order.pointerEmitter + "\""
			if !strings.Contains(out, want) {
				t.Errorf("%s: drifted duplicate should point at the emitter; want %q in:\n%s", order.name, want, out)
			}
		} else if count := strings.Count(out, "DIRECT REFERENCE \"specs/src.md#Contract\""); count != 1 {
			t.Errorf("%s: %d reference lines for the shared section, want exactly the drifted emission:\n%s", order.name, count, out)
		}
	}
}
