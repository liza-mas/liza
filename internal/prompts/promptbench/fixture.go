// Package promptbench measures rendered prompt payload by section, so that a
// payload reduction can be shown to have reduced something.
//
// The fixture is synthetic. It reproduces the proportions recorded in
// testdata/run-calibration.json — carrier count and size, carrier heading
// structure, and per-site dependency-render volume — using generated filler
// text. No content from the measured run is reproduced here; see the
// calibration record for why.
//
// Carrier *internal structure* is calibrated, not only carrier size. A fixture
// whose carriers are structureless filler can measure reference-by-path but
// cannot measure section-scoped inclusion, and the fixture is committed before
// the reduction is chosen.
package promptbench

import (
	"fmt"
	"strings"
)

// Shape is the calibrated fixture geometry. Field values come from
// testdata/run-calibration.json; see CalibratedShape.
type Shape struct {
	// Carriers inlined into one rendered prompt.
	Carriers int
	// CarrierBytes is the target size of each generated carrier.
	CarrierBytes int
	// HeadingsPerCarrier controls section granularity — the property that
	// makes section-scoped inclusion measurable.
	HeadingsPerCarrier int
	// HeadingDepthMix is the relative frequency of h1..h4, in that order.
	HeadingDepthMix [4]int

	// PhaseDependencies drives site 3 (SIBLING CONSISTENCY RULE). These must
	// share the current task's role pair or collectPhaseDependencyTasks
	// filters them out and the site silently measures zero.
	PhaseDependencies int

	// Descendants and DepsPerDescendant drive site 2, which is
	// O(descendants x deps) and absent from the measured run.
	Descendants       int
	DepsPerDescendant int

	// ActiveTasks and DepsPerActiveTask drive site 4 via the orchestrator
	// dashboard. Task count is capped at 12 by the renderer; dependency
	// count is not.
	ActiveTasks       int
	DepsPerActiveTask int
}

// CalibratedShape returns the per-prompt geometry derived from the calibration
// record. Per-prompt values are the corpus figures divided by prompt count, so
// one rendered fixture prompt approximates one average measured prompt.
//
// Derivations, so a reader can check them against testdata/run-calibration.json:
//
//	Carriers            767 occurrences / 424 prompts    ~= 1.8, rounded to 2
//	CarrierBytes        68163964 bytes / 767 carriers    ~= 88870
//	HeadingsPerCarrier  38067 headings / 767 carriers    ~= 50
//	HeadingDepthMix     2218:17496:18134:219             ~= 6:46:48:1 (percent)
//	PhaseDependencies   539 lines / 123 prompts          ~= 4
//	ActiveTasks         renderer cap
//	DepsPerActiveTask   290 mean bytes / ~24 bytes per id ~= 12
func CalibratedShape() Shape {
	return Shape{
		Carriers:           2,
		CarrierBytes:       88870,
		HeadingsPerCarrier: 50,
		HeadingDepthMix:    [4]int{6, 46, 48, 1},

		PhaseDependencies: 4,

		// Site 2 is zero in the measured run. The fixture still populates it
		// so the harness reports a real number rather than a structural zero:
		// a reduction that only helps when this section is present must still
		// be measurable. The calibration record is the authority on what the
		// measured run contained; this is the harness's coverage, not a claim
		// about the run.
		Descendants:       8,
		DepsPerDescendant: 14,

		ActiveTasks:       12,
		DepsPerActiveTask: 12,
	}
}

// filler returns deterministic prose of approximately n bytes. Deterministic
// so that a baseline captured today is comparable to a run tomorrow.
func filler(n int, seed string) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(n + 32)
	i := 0
	for b.Len() < n {
		// Vary line length a little so wrapping and line-oriented measurement
		// do not see an unnaturally uniform shape.
		fmt.Fprintf(&b, "%s line %d: generated filler standing in for specification prose.\n", seed, i)
		i++
	}
	return b.String()[:n]
}

// GenerateCarrier renders one synthetic carrier with calibrated section
// structure: headings at the calibrated depth mix, with the byte budget spread
// across sections.
func GenerateCarrier(path, blobOID string, shape Shape) string {
	var b strings.Builder

	fmt.Fprintf(&b, "CARRIER %q @ %s\n", path, blobOID)

	headings := max(shape.HeadingsPerCarrier, 1)
	// Budget excludes the heading lines themselves.
	sectionBytes := shape.CarrierBytes / headings

	depths := expandDepthMix(shape.HeadingDepthMix, headings)
	for i, depth := range depths {
		fmt.Fprintf(&b, "\n%s Section %d\n\n", strings.Repeat("#", depth), i)
		b.WriteString(filler(sectionBytes, fmt.Sprintf("s%d", i)))
	}
	return b.String()
}

// expandDepthMix turns relative heading-depth frequencies into a concrete
// sequence of that many headings, laid out so depths interleave rather than
// appearing in blocks.
func expandDepthMix(mix [4]int, count int) []int {
	total := 0
	for _, m := range mix {
		total += m
	}
	if total == 0 {
		return repeatInt(2, count)
	}

	var out []int
	for depth, weight := range mix {
		n := weight * count / total
		for range n {
			out = append(out, depth+1)
		}
	}
	// Rounding can leave the sequence short; pad with the modal depth.
	modal := 1
	for depth, weight := range mix {
		if weight > mix[modal-1] {
			modal = depth + 1
		}
	}
	for len(out) < count {
		out = append(out, modal)
	}
	out = out[:count]

	interleave(out)
	return out
}

// interleave reorders in place so equal depths are not adjacent in long runs,
// approximating a real document's alternation between levels.
func interleave(depths []int) {
	sorted := make([]int, len(depths))
	copy(sorted, depths)
	for i := range depths {
		// Stride through the sorted slice by a step coprime-ish to its length.
		depths[i] = sorted[(i*7)%len(sorted)]
	}
}

func repeatInt(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// GenerateResolvedReferenceContext renders the carrier block as the compositor
// would present it: the standing preamble followed by the carriers.
func GenerateResolvedReferenceContext(shape Shape) string {
	var b strings.Builder
	b.WriteString(`The compositor resolved and freshness-checked the strict carrier content and its
declared direct references before launch. Treat this bounded context as inherited
authority. Do not re-read or replace its pinned source paths with worktree content.
Read outside it only for evidence owned by your stage or an unresolved dependency
that the assigned context identifies.
`)
	for i := range shape.Carriers {
		b.WriteString("\n")
		b.WriteString(GenerateCarrier(
			fmt.Sprintf("specs/generated/fixture-carrier-%d.md", i),
			fmt.Sprintf("%040x", i+1),
			shape,
		))
	}
	return b.String()
}
