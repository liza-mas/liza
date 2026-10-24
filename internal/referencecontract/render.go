package referencecontract

import (
	"fmt"
	"strconv"
	"strings"
)

// CarrierClass ranks how a carrier entered the resolved context. When the same
// path is observed through more than one route, the higher class wins.
type CarrierClass uint8

const (
	CarrierScalar CarrierClass = iota
	CarrierParent
	CarrierReview
)

// Carrier is one strict carrier resolved for a prompt: its pinned span and
// the direct references it declares.
//
// ElideRefs marks ancestor or decomposition-root second-hop context:
// its declared references are rendered as one-line
// pointers rather than spans. The zero value renders every reference in full,
// so a caller that does not classify carriers gets the complete context.
//
// AssignedHeading is the section of this carrier the task was assigned, when
// its ref declared one. It narrows the span by eliding that section's peers,
// retaining shared context by default. SectionOnly retains only the assigned
// section; Empty renders the span whole.
type Carrier struct {
	Path            string
	Span            string
	Revision        string
	Class           CarrierClass
	BlobOID         string
	Refs            []Reference
	ElideRefs       bool
	AssignedHeading string
	// Explicit section read sets opt in; zero values retain legacy rendering.
	SectionOnly        bool
	PointerOnly        bool
	InlineReferenceIDs map[string]bool
}

// Reference is one resolved direct reference: a section of a pinned file.
// BlobOID records the blob the section was read from; it is provenance for
// callers and test fixtures, not an identity used by rendering.
//
// PinnedRevision is set only for a reference whose section drifted under a
// pin that cannot move (ADR-0133): Revision then names where Span was read
// from — integration HEAD, or the pin itself when Unresolved gives why the
// section no longer resolves at HEAD.
type Reference struct {
	ID             string
	Path           string
	Heading        string
	Revision       string
	BlobOID        string
	Span           string
	PinnedRevision string
	Unresolved     string
}

// RenderCarriers reconciles observations of the same path and renders the
// resolved reference context.
//
// Duplicate direct references are emitted once, identified by the text they
// render rather than by the blob they were pinned at. A direct reference whose
// section is already present in a carrier inlined in full at the same path is
// also emitted once — as a one-line pointer to that carrier rather than a
// second copy of the section. Containment is checked on the bytes, so a
// reference pinned at an older revision of a file edited elsewhere still
// elides; if the section is not present, the reference is emitted in full.
//
// By default, a carrier naming an AssignedHeading renders that section and
// shared context, with one pointer per peer. SectionOnly instead renders only
// that section and its selected references; PointerOnly renders a carrier pointer.
// Narrowing happens before duplicate detection, so a reference whose text
// survived only inside an elided peer is emitted in full rather than pointing
// at a section no longer present.
//
// References declared by a carrier with ElideRefs set are pointers: to the
// carrier that inlines the same reference in full when one does, otherwise
// to the pinned revision, which the agent can read with git show. A pointer
// never consumes the once-only slot of a full emission, so a reference shared
// by an elided and a full carrier is still rendered in full exactly once.
//
// A drifted reference (PinnedRevision set) appends its disclosure to whichever
// line renders it. When its text is already emitted for another carrier, it
// still gets one pointer line carrying the disclosure; an undrifted duplicate
// of an emitted drifted reference adds nothing.
func RenderCarriers(observations []Carrier) (string, error) {
	if len(observations) == 0 {
		return "", nil
	}
	winners, err := selectCarrierWinners(observations)
	if err != nil {
		return "", err
	}
	if err := narrowAssignedSections(winners); err != nil {
		return "", err
	}
	paths := sortedCarrierPaths(winners)
	emitter := newReferenceEmitter(winners)
	emitter.planEmission(paths)
	var out strings.Builder
	for _, path := range paths {
		carrier := winners[path]
		writeCarrierBody(&out, path, carrier)
		emitter.writeCarrierReferences(&out, carrier)
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

// driftNote discloses a reference whose section drifted under its pin, or ""
// when it did not. A resolvable section shows the current text and names the
// command that compares it with the pinned one; an unresolvable section shows
// the pinned text and says why nothing current could be read.
func driftNote(ref Reference) string {
	if ref.PinnedRevision == "" {
		return ""
	}
	if ref.Unresolved != "" {
		return fmt.Sprintf("section no longer resolves at integration HEAD (%s); pinned text shown", ref.Unresolved)
	}
	return fmt.Sprintf("section changed since its pinned revision %s; current text shown; compare: git diff %s %s -- %s",
		ref.PinnedRevision, shellQuote(ref.PinnedRevision), shellQuote(ref.Revision), shellQuote(ref.Path))
}

func withNote(pointer, note string) string {
	if note == "" {
		return pointer
	}
	return pointer + "; " + note
}

// elideAssignedPeers replaces each peer of the carrier's assigned section with
// a pointer naming where to read it. The pointer carries the carrier's own
// revision, so the agent reads the same content the peer's owner was given.
func elideAssignedPeers(carrier Carrier) string {
	peers := peerSections(carrier.Span, carrier.AssignedHeading)
	if len(peers) == 0 {
		return carrier.Span
	}
	var out strings.Builder
	cursor := 0
	for _, peer := range peers {
		out.WriteString(carrier.Span[cursor:peer.start])
		fmt.Fprintf(&out, "SECTION %s @ %s — peer of your assigned section; not inlined; read with git show %s if needed\n",
			strconv.Quote(carrier.Path+"#"+peer.heading), carrier.Revision,
			shellQuote(carrier.Revision+":"+carrier.Path))
		cursor = peer.end
	}
	out.WriteString(carrier.Span[cursor:])
	return out.String()
}

// shellQuote renders a Git object argument the agent can paste verbatim.
// POSIX single quotes, because a path is only checked for separators, control
// characters and a .md extension: "$(cmd).md" and "`cmd`.md" are accepted
// names, and inside double quotes a pasted command would run them. Single
// quotes expand nothing, so every accepted name stays literal; an embedded
// apostrophe closes and reopens the quoting around an escaped one.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// refKey identifies a reference by what it renders. Two carriers citing the
// same section pinned at different revisions emit it once; a genuinely
// different section text under the same heading still emits separately.
func refKey(ref Reference) string {
	return ref.Path + "\x00" + ref.Heading + "\x00" + ref.Span
}
