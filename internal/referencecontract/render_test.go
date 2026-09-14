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

	// A true pre-reduction render: identical spans, but the references are
	// pinned at a blob that does not match the inlined carrier, so elision
	// cannot fire. The header line carries the revision, not the OID, so the
	// output is byte-identical to what the pre-reduction code emitted.
	unelided := make([]Carrier, len(carriers))
	copy(unelided, carriers)
	unelided[1].Refs = make([]Reference, len(refs))
	for i, r := range refs {
		r.BlobOID = "not-the-inlined-blob"
		unelided[1].Refs[i] = r
	}
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
