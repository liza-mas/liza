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
type Carrier struct {
	Path     string
	Span     string
	Revision string
	Class    CarrierClass
	BlobOID  string
	Refs     []Reference
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
// resolved reference context. Duplicate direct references are emitted once.
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
	var out strings.Builder
	seenRefs := make(map[string]bool)
	for _, path := range paths {
		carrier := winners[path]
		fmt.Fprintf(&out, "CARRIER %s @ %s\n%s", strconv.Quote(path), carrier.Revision, carrier.Span)
		if !strings.HasSuffix(carrier.Span, "\n") {
			out.WriteByte('\n')
		}
		for _, ref := range carrier.Refs {
			key := ref.Path + "\x00" + ref.Heading + "\x00" + ref.BlobOID
			if seenRefs[key] {
				continue
			}
			seenRefs[key] = true
			fmt.Fprintf(&out, "DIRECT REFERENCE %s @ %s\n%s", strconv.Quote(ref.Path+"#"+ref.Heading), ref.Revision, ref.Span)
			if !strings.HasSuffix(ref.Span, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(out.String(), "\n"), nil
}
