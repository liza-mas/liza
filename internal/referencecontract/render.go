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
type Carrier struct {
	Path      string
	Span      string
	Revision  string
	Class     CarrierClass
	BlobOID   string
	Refs      []Reference
	ElideRefs bool
}

// Reference is one resolved direct reference: a section of a pinned file.
type Reference struct {
	Path     string
	Heading  string
	Revision string
	BlobOID  string
	Span     string
}

// RenderCarriers reconciles observations of the same path and renders the
// resolved reference context.
//
// Duplicate direct references are emitted once. A direct reference whose
// target is already inlined in full as a carrier at the same path and blob
// OID is also emitted once — as a one-line pointer to that carrier rather
// than a second copy of the section. At an equal OID the section is a
// substring of the carrier by construction, and containment is checked on
// the bytes anyway; if either fails, the reference is emitted in full. A
// reference pinned at a different blob than the inlined carrier carries
// different text and is never elided.
//
// References declared by a carrier with ElideRefs set are pointers: to the
// carrier that inlines the same reference in full when one does, otherwise
// to the pinned revision, which the agent can read with git show. A pointer
// never consumes the once-only slot of a full emission, so a reference shared
// by an elided and a full carrier is still rendered in full exactly once.
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
	paths := make([]string, 0, len(winners))
	for path := range winners {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	containedIn := func(ref Reference) (string, bool) {
		inlined, ok := winners[ref.Path]
		if ok && inlined.BlobOID == ref.BlobOID && strings.Contains(inlined.Span, ref.Span) {
			return inlined.Path, true
		}
		return "", false
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
			if _, contained := containedIn(ref); contained {
				continue
			}
			emittedBy[key] = path
		}
	}
	var out strings.Builder
	seenRefs := make(map[string]bool)
	seenPointers := make(map[string]bool)
	for _, path := range paths {
		carrier := winners[path]
		fmt.Fprintf(&out, "CARRIER %s @ %s\n%s", strconv.Quote(path), carrier.Revision, carrier.Span)
		if !strings.HasSuffix(carrier.Span, "\n") {
			out.WriteByte('\n')
		}
		for _, ref := range carrier.Refs {
			key := refKey(ref)
			if seenRefs[key] {
				continue
			}
			target := strconv.Quote(ref.Path + "#" + ref.Heading)
			if inlinedPath, contained := containedIn(ref); contained {
				seenRefs[key] = true
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — inlined in this context as CARRIER %s\n",
					target, ref.Revision, strconv.Quote(inlinedPath))
				continue
			}
			if carrier.ElideRefs {
				if emitter, ok := emittedBy[key]; ok {
					if !seenPointers[key] {
						seenPointers[key] = true
						fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — inlined in this context under CARRIER %s\n",
							target, ref.Revision, strconv.Quote(emitter))
					}
					continue
				}
				seenRefs[key] = true
				fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s — not inlined; read with git show %s:%s if needed\n",
					target, ref.Revision, ref.Revision, ref.Path)
				continue
			}
			seenRefs[key] = true
			fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s\n%s", target, ref.Revision, ref.Span)
			if !strings.HasSuffix(ref.Span, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

func refKey(ref Reference) string {
	return ref.Path + "\x00" + ref.Heading + "\x00" + ref.BlobOID
}
