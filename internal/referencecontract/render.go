package referencecontract

import (
	"fmt"
	"sort"
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
// ElideRefs marks a carrier inherited from further up the lineage than the
// task's assigned artifacts: its declared references are rendered as one-line
// pointers rather than spans. The zero value renders every reference in full,
// so a caller that does not classify carriers gets the complete context.
//
// AssignedHeading is the section of this carrier the task was assigned, when
// its ref declared one. It narrows the span by eliding that section's peers,
// never the context they share. Empty renders the span whole.
type Carrier struct {
	Path            string
	Span            string
	Revision        string
	Class           CarrierClass
	BlobOID         string
	Refs            []Reference
	ElideRefs       bool
	AssignedHeading string
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
// A carrier naming an AssignedHeading renders that section and everything it
// shares with the rest of the file, and one pointer line per peer section.
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
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].Path != observations[j].Path {
			return observations[i].Path < observations[j].Path
		}
		return observations[i].Class > observations[j].Class
	})
	winners := make(map[string]Carrier)
	for _, observation := range observations {
		if existing, ok := winners[observation.Path]; ok {
			if existing.Class == observation.Class && (existing.BlobOID != observation.BlobOID || existing.Span != observation.Span) {
				return "", fmt.Errorf("conflicting carrier provenance for %q", observation.Path)
			}
			continue
		}
		winners[observation.Path] = observation
	}
	for path, carrier := range winners {
		if carrier.AssignedHeading == "" {
			continue
		}
		carrier.Span = elideAssignedPeers(carrier)
		winners[path] = carrier
	}
	paths := make([]string, 0, len(winners))
	for path := range winners {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	// Identity is the rendered section, not the file blob: freshness admits a
	// reference whose section is unchanged while the rest of its file moved
	// (ADR-0133), so blob equality would miss real duplicates.
	containedIn := func(ref Reference) bool {
		inlined, ok := winners[ref.Path]
		return ok && strings.Contains(inlined.Span, ref.Span)
	}
	// Which carrier emits each reference in full, decided before rendering so
	// an elided carrier that sorts earlier can point at it.
	emittedBy := make(map[string]string)
	for _, path := range paths {
		carrier := winners[path]
		if carrier.ElideRefs {
			continue
		}
		for _, ref := range carrier.Refs {
			key := refKey(ref)
			if _, done := emittedBy[key]; done {
				continue
			}
			if containedIn(ref) {
				continue
			}
			emittedBy[key] = path
		}
	}
	var out strings.Builder
	seenRefs := make(map[string]bool)
	seenPointers := make(map[string]bool)
	// The text slot is keyed on content, but a drifted reference owes its
	// disclosure even when another carrier pinned the same text at a current
	// revision; seenNotes tracks those disclosures separately so neither
	// emission swallows the other.
	seenNotes := make(map[string]bool)
	for _, path := range paths {
		carrier := winners[path]
		fmt.Fprintf(&out, "CARRIER %s @ %s\n%s", strconv.Quote(path), carrier.Revision, carrier.Span)
		if !strings.HasSuffix(carrier.Span, "\n") {
			out.WriteByte('\n')
		}
		for _, ref := range carrier.Refs {
			key := refKey(ref)
			note := driftNote(ref)
			noteKey := key + "\x00" + ref.PinnedRevision
			target := strconv.Quote(ref.Path + "#" + ref.Heading)
			if seenRefs[key] {
				if note != "" && !seenNotes[noteKey] {
					seenNotes[noteKey] = true
					var where string
					switch emitter, emitted := emittedBy[key]; {
					case containedIn(ref):
						where = withNote("inlined in this context as CARRIER "+strconv.Quote(ref.Path), note)
					case emitted:
						where = note + "; same text inlined in this context under CARRIER " + strconv.Quote(emitter)
					default:
						where = withNote("not inlined; read with git show "+shellQuote(ref.Revision+":"+ref.Path)+" if needed", note)
					}
					fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — %s\n", target, ref.Revision, where)
				}
				continue
			}
			if containedIn(ref) {
				seenRefs[key], seenNotes[noteKey] = true, true
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — %s\n", target, ref.Revision,
					withNote("inlined in this context as CARRIER "+strconv.Quote(ref.Path), note))
				continue
			}
			if carrier.ElideRefs {
				if emitter, ok := emittedBy[key]; ok {
					if !seenPointers[noteKey] {
						seenPointers[noteKey], seenNotes[noteKey] = true, true
						fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — %s\n", target, ref.Revision,
							withNote("inlined in this context under CARRIER "+strconv.Quote(emitter), note))
					}
					continue
				}
				seenRefs[key], seenNotes[noteKey] = true, true
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — %s\n", target, ref.Revision,
					withNote("not inlined; read with git show "+shellQuote(ref.Revision+":"+ref.Path)+" if needed", note))
				continue
			}
			seenRefs[key], seenNotes[noteKey] = true, true
			if note != "" {
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — %s\n%s", target, ref.Revision, note, ref.Span)
			} else {
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s\n%s", target, ref.Revision, ref.Span)
			}
			if !strings.HasSuffix(ref.Span, "\n") {
				out.WriteByte('\n')
			}
		}
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
