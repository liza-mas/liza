package referencecontract

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// selectCarrierWinners orders observations by path, then class (highest
// first), and keeps one carrier per path: the first of the highest class.
// Two observations of equal class that disagree on provenance conflict.
func selectCarrierWinners(observations []Carrier) (map[string]Carrier, error) {
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
				return nil, fmt.Errorf("conflicting carrier provenance for %q", observation.Path)
			}
			continue
		}
		winners[observation.Path] = observation
	}
	return winners, nil
}

// narrowAssignedSections replaces the span of each carrier that names an
// AssignedHeading, before duplicate detection looks at the spans.
func narrowAssignedSections(winners map[string]Carrier) error {
	for path, carrier := range winners {
		if carrier.PointerOnly {
			continue
		}
		if carrier.AssignedHeading == "" {
			continue
		}
		if carrier.SectionOnly {
			span, err := ExtractSection(carrier.Span, carrier.AssignedHeading)
			if err != nil {
				return err
			}
			carrier.Span = span
		} else {
			carrier.Span = elideAssignedPeers(carrier)
		}
		winners[path] = carrier
	}
	return nil
}

func sortedCarrierPaths(winners map[string]Carrier) []string {
	paths := make([]string, 0, len(winners))
	for path := range winners {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// writeCarrierBody renders a carrier's inlined span, or its pointer, and the
// navigation line of a section-only carrier.
func writeCarrierBody(out *strings.Builder, path string, carrier Carrier) {
	if carrier.PointerOnly {
		fmt.Fprintf(out, "CARRIER %s @ %s — inherited context; not inlined; read with git show %s if needed\n",
			strconv.Quote(path), carrier.Revision, shellQuote(carrier.Revision+":"+path))
	} else {
		fmt.Fprintf(out, "CARRIER %s @ %s\n%s", strconv.Quote(path), carrier.Revision, carrier.Span)
		if !strings.HasSuffix(carrier.Span, "\n") {
			out.WriteByte('\n')
		}
	}
	if carrier.SectionOnly {
		fmt.Fprintf(out, "OTHER SECTIONS AND REFERENCES %s @ %s — shared and sibling context; read Source References and exact headings with git show %s if needed\n",
			strconv.Quote(path), carrier.Revision, shellQuote(carrier.Revision+":"+path))
	}
}
