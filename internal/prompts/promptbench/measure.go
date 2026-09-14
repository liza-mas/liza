package promptbench

import (
	"bufio"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// SiteMeasurement is one row of the dependency-render enumeration.
//
// Rows that measure zero are reported as zero, never omitted. A site that
// renders nothing in a given fixture or run is a finding — it means a
// reduction targeting it cannot be validated by that input — and an omitted
// row is indistinguishable from a site nobody looked at.
type SiteMeasurement struct {
	Site        string `json:"site"`
	Source      string `json:"source"`
	Occurrences int    `json:"occurrences"`
	Bytes       int    `json:"bytes"`
	LongestRun  int    `json:"longest_run_bytes"`
	Note        string `json:"note,omitempty"`
}

// Report is the benchmark's output: total rendered bytes, the carrier block,
// and every dependency render site.
type Report struct {
	TotalBytes     int               `json:"total_bytes"`
	CarrierBlock   SectionMeasure    `json:"carrier_block"`
	Sections       []SectionMeasure  `json:"sections"`
	DependsOnSites []SiteMeasurement `json:"depends_on_sites"`
	DependsOnTotal int               `json:"depends_on_total_bytes"`
	DependsOnShare float64           `json:"depends_on_share_of_total"`
	CarrierShare   float64           `json:"carrier_share_of_total"`
	// DuplicateReferences is the byte weight of DIRECT REFERENCE blocks whose
	// span is already present verbatim in a CARRIER inlined at the same path
	// in the same prompt. ElidedReferences counts references rendered as a
	// one-line pointer to such a carrier instead of a second copy.
	DuplicateReferences ReferenceMeasure `json:"duplicate_references"`
	CarrierHeadings     int              `json:"carrier_headings"`
	CarrierSections     int              `json:"carrier_sections"`
	MeanSectionBytes    int              `json:"mean_carrier_section_bytes"`
}

// ReferenceMeasure reports direct-reference duplication against inlined
// carriers.
type ReferenceMeasure struct {
	DuplicatedBlocks int     `json:"duplicated_blocks"`
	DuplicatedBytes  int     `json:"duplicated_bytes"`
	ElidedPointers   int     `json:"elided_pointers"`
	ShareOfTotal     float64 `json:"share_of_total"`
}

// SectionMeasure is the byte weight of one rendered `=== NAME ===` section.
type SectionMeasure struct {
	Name  string `json:"name"`
	Bytes int    `json:"bytes"`
}

var (
	sectionHeader = regexp.MustCompile(`^=== (.+) ===$`)
	mdHeading     = regexp.MustCompile(`^#{1,6} `)

	// site2: descendant dependency lines, two-space indented inside the
	// IntegrationDescendants range (branch_integration_context.tmpl:22-23).
	site2Line = regexp.MustCompile(`^  Dependencies: `)
	// site4: active-task digest entries (builder.go:385-386).
	site4Run = regexp.MustCompile(`depends_on=[^ ]+`)
)

// MeasureRendered walks a rendered prompt and reports section weights and
// per-site dependency volume.
//
// site3 is delimited by its rendered banner rather than a field match: the
// SIBLING CONSISTENCY RULE block renders task IDs, not a DependsOn field, so
// no field-level pattern finds it. Matching on the literal string
// "Dependencies:" alone finds prose inside inlined carrier documents and none
// of the real sites — that mistake is why this function measures per row.
func MeasureRendered(rendered string) Report {
	r := Report{TotalBytes: len(rendered)}

	var (
		currentSection string
		sectionBytes   = map[string]int{}
		order          []string

		inCarrierBlock bool
		inSite3        bool

		site2Occ, site2Bytes, site2Longest int
		site3Occ, site3Bytes, site3Longest int
	)

	scan := bufio.NewScanner(strings.NewReader(rendered))
	scan.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scan.Scan() {
		line := scan.Text()
		n := len(line) + 1

		if m := sectionHeader.FindStringSubmatch(line); m != nil {
			currentSection = m[1]
			if _, seen := sectionBytes[currentSection]; !seen {
				order = append(order, currentSection)
			}
			inCarrierBlock = currentSection == "RESOLVED REFERENCE CONTEXT"
			inSite3 = false
			sectionBytes[currentSection] += n
			continue
		}
		if currentSection != "" {
			sectionBytes[currentSection] += n
		}

		if inCarrierBlock && mdHeading.MatchString(line) {
			r.CarrierHeadings++
		}

		if site2Line.MatchString(line) {
			site2Occ++
			site2Bytes += n
			site2Longest = max(site2Longest, n)
		}

		switch {
		case strings.HasPrefix(line, "SIBLING CONSISTENCY RULE:"):
			inSite3 = true
		case inSite3 && strings.HasPrefix(line, "- "):
			site3Occ++
			site3Bytes += n
			site3Longest = max(site3Longest, n)
		case inSite3 && !strings.HasPrefix(line, "This task depends"):
			inSite3 = false
		}
	}

	r.DuplicateReferences = measureDuplicateReferences(rendered)
	if r.TotalBytes > 0 {
		r.DuplicateReferences.ShareOfTotal = float64(r.DuplicateReferences.DuplicatedBytes) / float64(r.TotalBytes)
	}

	site4Occ, site4Bytes, site4Longest := 0, 0, 0
	for _, m := range site4Run.FindAllString(rendered, -1) {
		site4Occ++
		site4Bytes += len(m)
		site4Longest = max(site4Longest, len(m))
	}

	r.DependsOnSites = []SiteMeasurement{
		{
			Site: "site2_descendant_dependencies", Source: "blocks/branch_integration_context.tmpl:22-23",
			Occurrences: site2Occ, Bytes: site2Bytes, LongestRun: site2Longest,
			Note: "uncapped; O(descendants x deps)",
		},
		{
			Site: "site3_sibling_consistency_rule", Source: "blocks/collective_plan_scoping.tmpl:55-61",
			Occurrences: site3Occ, Bytes: site3Bytes, LongestRun: site3Longest,
			Note: "uncapped; role-pair filtered, so a fixture without same-role-pair phase deps measures zero",
		},
		{
			Site: "site4_active_task_digest", Source: "builder.go:385-386",
			Occurrences: site4Occ, Bytes: site4Bytes, LongestRun: site4Longest,
			Note: "task count capped at 12, dependency count uncapped; where per-subtask fan-out lands",
		},
		{
			Site: "site5_task_graph_digest", Source: "blocks/collective_plan_scoping.tmpl:78",
			Note: "bounded by maxTaskGraphChildDeps within maxTaskGraphEntries=16; not separately extracted",
		},
	}
	for _, s := range r.DependsOnSites {
		r.DependsOnTotal += s.Bytes
	}

	for _, name := range order {
		m := SectionMeasure{Name: name, Bytes: sectionBytes[name]}
		if name == "RESOLVED REFERENCE CONTEXT" {
			r.CarrierBlock = m
		}
		r.Sections = append(r.Sections, m)
	}
	sort.Slice(r.Sections, func(i, j int) bool { return r.Sections[i].Bytes > r.Sections[j].Bytes })

	r.CarrierSections = r.CarrierHeadings
	if r.CarrierHeadings > 0 {
		r.MeanSectionBytes = r.CarrierBlock.Bytes / r.CarrierHeadings
	}
	if r.TotalBytes > 0 {
		r.CarrierShare = float64(r.CarrierBlock.Bytes) / float64(r.TotalBytes)
		r.DependsOnShare = float64(r.DependsOnTotal) / float64(r.TotalBytes)
	}
	return r
}

// JSON renders the report for the committed baseline artifact.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

var carrierOrRefHeader = regexp.MustCompile(`(?m)^(CARRIER "([^"]*)" @ \S+|DIRECT REFERENCE "([^"#]*)#[^"]*" @ \S+)( — inlined above as CARRIER "[^"]*")?$`)

// measureDuplicateReferences finds DIRECT REFERENCE blocks whose span is a
// verbatim substring of a CARRIER inlined at the same path in the same
// prompt. Path equality alone is not enough — a reference pinned at a
// different blob than the inlined carrier carries different text and is not a
// duplicate — so containment is checked on the bytes.
func measureDuplicateReferences(rendered string) ReferenceMeasure {
	var m ReferenceMeasure
	locs := carrierOrRefHeader.FindAllStringSubmatchIndex(rendered, -1)
	if len(locs) == 0 {
		return m
	}
	type block struct {
		isCarrier bool
		path      string
		elided    bool
		start     int // header start
		body      string
		size      int // header + body bytes
	}
	blocks := make([]block, 0, len(locs))
	for i, loc := range locs {
		end := len(rendered)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		// Stop a block at the next section banner so trailing sections do
		// not count as the last reference's body.
		if next := strings.Index(rendered[loc[1]:end], "\n=== "); next >= 0 {
			end = loc[1] + next + 1
		}
		b := block{start: loc[0], size: end - loc[0]}
		if loc[4] >= 0 {
			b.isCarrier, b.path = true, rendered[loc[4]:loc[5]]
		} else {
			b.path = rendered[loc[6]:loc[7]]
		}
		b.elided = loc[8] >= 0
		b.body = strings.TrimSpace(rendered[loc[1]:end])
		blocks = append(blocks, b)
	}
	carriers := map[string]string{}
	for _, b := range blocks {
		if b.isCarrier {
			carriers[b.path] = b.body
		}
	}
	for _, b := range blocks {
		if b.isCarrier {
			continue
		}
		if b.elided {
			m.ElidedPointers++
			continue
		}
		if inlined, ok := carriers[b.path]; ok && b.body != "" && strings.Contains(inlined, b.body) {
			m.DuplicatedBlocks++
			m.DuplicatedBytes += b.size
		}
	}
	return m
}
