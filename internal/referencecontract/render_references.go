package referencecontract

import (
	"fmt"
	"strconv"
	"strings"
)

// referenceEmitter renders the direct references of the selected carriers and
// owns the once-only state shared across them.
type referenceEmitter struct {
	winners map[string]Carrier
	// emittedBy names the carrier that emits each reference in full, decided
	// before rendering so an elided carrier that sorts earlier can point at it.
	emittedBy    map[string]string
	seenRefs     map[string]bool
	seenPointers map[string]bool
	// The text slot is keyed on content, but a drifted reference owes its
	// disclosure even when another carrier pinned the same text at a current
	// revision; seenNotes tracks those disclosures separately so neither
	// emission swallows the other.
	seenNotes map[string]bool
}

func newReferenceEmitter(winners map[string]Carrier) *referenceEmitter {
	return &referenceEmitter{
		winners:      winners,
		emittedBy:    make(map[string]string),
		seenRefs:     make(map[string]bool),
		seenPointers: make(map[string]bool),
		seenNotes:    make(map[string]bool),
	}
}

// containedIn reports whether the reference's section is already inlined in
// the full carrier at its path. Identity is the rendered section, not the file
// blob: freshness admits a reference whose section is unchanged while the rest
// of its file moved (ADR-0133), so blob equality would miss real duplicates.
func (e *referenceEmitter) containedIn(ref Reference) bool {
	inlined, ok := e.winners[ref.Path]
	return ok && !inlined.PointerOnly && strings.Contains(inlined.Span, ref.Span)
}

// planEmission decides, over the sorted paths, which carrier emits each
// reference in full.
func (e *referenceEmitter) planEmission(paths []string) {
	for _, path := range paths {
		carrier := e.winners[path]
		if carrier.PointerOnly || carrier.ElideRefs {
			continue
		}
		for _, ref := range carrier.Refs {
			if carrier.InlineReferenceIDs != nil && !carrier.InlineReferenceIDs[ref.ID] {
				continue
			}
			key := refKey(ref)
			if _, done := e.emittedBy[key]; done {
				continue
			}
			if e.containedIn(ref) {
				continue
			}
			e.emittedBy[key] = path
		}
	}
}

// refView carries what each emission branch derives from one reference.
type refView struct {
	ref     Reference
	key     string
	note    string
	noteKey string
	target  string
}

func newRefView(ref Reference) refView {
	key := refKey(ref)
	return refView{
		ref:     ref,
		key:     key,
		note:    driftNote(ref),
		noteKey: key + "\x00" + ref.PinnedRevision,
		target:  strconv.Quote(ref.Path + "#" + ref.Heading),
	}
}

// writeCarrierReferences renders one carrier's references in declaration
// order, then the footer naming the ones a section-only carrier left unselected.
func (e *referenceEmitter) writeCarrierReferences(out *strings.Builder, carrier Carrier) {
	var unselectedIDs []string
	for _, ref := range carrier.Refs {
		view := newRefView(ref)
		// Compact navigation routes unselected spans through their carrier's
		// declaration. It does not consume a full-emission slot, and each
		// freshness disclosure still names the reference and its real pin.
		if carrier.PointerOnly || carrier.InlineReferenceIDs != nil && !carrier.InlineReferenceIDs[ref.ID] {
			if carrier.SectionOnly {
				unselectedIDs = append(unselectedIDs, strconv.Quote(ref.ID))
			}
			e.writeNavigationPointer(out, view)
			continue
		}
		if e.seenRefs[view.key] {
			e.writeRepeatedDisclosure(out, view)
			continue
		}
		e.writeFirstEmission(out, carrier, view)
	}
	writeUnselectedFooter(out, unselectedIDs)
}

// writeNavigationPointer discloses drift of a reference the carrier does not inline.
func (e *referenceEmitter) writeNavigationPointer(out *strings.Builder, v refView) {
	if v.note == "" || e.seenNotes[v.noteKey] {
		return
	}
	pointerNote := strings.Replace(v.note, "pinned text shown", "pinned section available via pointer", 1)
	pointerNote = strings.Replace(pointerNote, "current text shown", "current section available via pointer", 1)
	fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — not inlined; read with git show %s if needed; %s\n",
		v.target, v.ref.Revision, shellQuote(v.ref.Revision+":"+v.ref.Path), pointerNote)
	e.seenNotes[v.noteKey] = true
}

// writeRepeatedDisclosure emits, for a reference whose text is already
// emitted, only its drift disclosure, once.
func (e *referenceEmitter) writeRepeatedDisclosure(out *strings.Builder, v refView) {
	if v.note == "" || e.seenNotes[v.noteKey] {
		return
	}
	e.seenNotes[v.noteKey] = true
	var where string
	switch emitter, emitted := e.emittedBy[v.key]; {
	case e.containedIn(v.ref):
		where = withNote("inlined in this context as CARRIER "+strconv.Quote(v.ref.Path), v.note)
	case emitted:
		where = v.note + "; same text inlined in this context under CARRIER " + strconv.Quote(emitter)
	default:
		where = withNote("not inlined; read with git show "+shellQuote(v.ref.Revision+":"+v.ref.Path)+" if needed", v.note)
	}
	fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — %s\n", v.target, v.ref.Revision, where)
}

// writeFirstEmission renders a reference whose text has not been emitted yet:
// as a pointer to the carrier containing it, as an elided pointer, or in full.
func (e *referenceEmitter) writeFirstEmission(out *strings.Builder, carrier Carrier, v refView) {
	if e.containedIn(v.ref) {
		e.seenRefs[v.key], e.seenNotes[v.noteKey] = true, true
		fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — %s\n", v.target, v.ref.Revision,
			withNote("inlined in this context as CARRIER "+strconv.Quote(v.ref.Path), v.note))
		return
	}
	if carrier.ElideRefs {
		e.writeElidedReference(out, v)
		return
	}
	e.seenRefs[v.key], e.seenNotes[v.noteKey] = true, true
	if v.note != "" {
		fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — %s\n%s", v.target, v.ref.Revision, v.note, v.ref.Span)
	} else {
		fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s\n%s", v.target, v.ref.Revision, v.ref.Span)
	}
	if !strings.HasSuffix(v.ref.Span, "\n") {
		out.WriteByte('\n')
	}
}

// writeElidedReference renders a reference of an ancestor carrier as a
// one-line pointer: to the carrier inlining it in full when one does,
// otherwise to its pinned revision.
func (e *referenceEmitter) writeElidedReference(out *strings.Builder, v refView) {
	if emitter, ok := e.emittedBy[v.key]; ok {
		if !e.seenPointers[v.noteKey] {
			e.seenPointers[v.noteKey], e.seenNotes[v.noteKey] = true, true
			fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — %s\n", v.target, v.ref.Revision,
				withNote("inlined in this context under CARRIER "+strconv.Quote(emitter), v.note))
		}
		return
	}
	e.seenRefs[v.key], e.seenNotes[v.noteKey] = true, true
	fmt.Fprintf(out, "DIRECT REFERENCE %s @ %s — %s\n", v.target, v.ref.Revision,
		withNote("not inlined; read with git show "+shellQuote(v.ref.Revision+":"+v.ref.Path)+" if needed", v.note))
}

func writeUnselectedFooter(out *strings.Builder, unselectedIDs []string) {
	if len(unselectedIDs) > 0 {
		fmt.Fprintf(out, "UNSELECTED DIRECT REFERENCE IDS: %s — pointers via this carrier's Source References above\n", strings.Join(unselectedIDs, ", "))
	}
}
