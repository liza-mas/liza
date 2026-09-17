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

	"github.com/liza-mas/liza/internal/referencecontract"
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

	// DuplicateRefs is the number of direct references one carrier declares
	// into sections of another carrier that is already inlined in full. The
	// measured run carries ~2.6 such references per prompt, totalling 11.7%
	// of the corpus; they are the target of the reduction, so the fixture
	// must contain them or the reduction is unmeasurable.
	DuplicateRefs int
	// StaleRefs is the number of direct references whose pinned blob differs
	// from the inlined carrier's. These must never be elided.
	StaleRefs int

	// AncestorCarriers are scalar-class carriers inherited from further up
	// the lineage (the epic behind an architecture plan, the architecture
	// plan behind a code plan). Each declares AncestorRefs direct references
	// into documents that are not themselves inlined, each of about
	// AncestorRefBytes. The measured run renders every such reference in
	// full although only the assigned carrier's references are the task's
	// read set (roles.md, "Reference-First Planning Artifact Review").
	AncestorCarriers     int
	AncestorCarrierBytes int
	AncestorRefs         int
	AncestorRefBytes     int

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
//	DuplicateRefs       9501128 bytes / 424 prompts / ~1800 bytes per section ~= 12
//	AncestorCarriers    589 carriers / 286 prompts       ~= 2.06, rounded to 2
//	AncestorCarrierBytes 37647 mean span bytes
//	AncestorRefs        7354 refs / 589 carriers         ~= 12.5, rounded to 13
//	AncestorRefBytes    5261 mean bytes per reference
//	PhaseDependencies   539 lines / 123 prompts          ~= 4
//	ActiveTasks         renderer cap
//	DepsPerActiveTask   290 mean bytes / ~24 bytes per id ~= 12
func CalibratedShape() Shape {
	return Shape{
		Carriers:           2,
		CarrierBytes:       88870,
		HeadingsPerCarrier: 50,
		HeadingDepthMix:    [4]int{6, 46, 48, 1},

		DuplicateRefs: 12,
		StaleRefs:     1,

		AncestorCarriers:     2,
		AncestorCarrierBytes: 37647,
		AncestorRefs:         13,
		AncestorRefBytes:     5261,

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

// carrierSections generates one synthetic carrier body with calibrated
// section structure and returns it alongside the (heading, section-text)
// pairs, so a reference into it can be built from a real section.
func carrierSections(shape Shape, seed string) (string, []carrierSection) {
	return sectionedBody(shape, shape.CarrierBytes, seed)
}

// sectionedBody generates a body of about totalBytes at the calibrated
// section granularity (CarrierBytes / HeadingsPerCarrier per section).
func sectionedBody(shape Shape, totalBytes int, seed string) (string, []carrierSection) {
	sectionBytes := max(shape.CarrierBytes/max(shape.HeadingsPerCarrier, 1), 1)
	headings := max(totalBytes/sectionBytes, 1)
	depths := expandDepthMix(shape.HeadingDepthMix, headings)

	var b strings.Builder
	var sections []carrierSection
	for i, depth := range depths {
		heading := fmt.Sprintf("Section %d", i)
		body := filler(sectionBytes, fmt.Sprintf("%s-s%d", seed, i))
		text := fmt.Sprintf("\n%s %s\n\n%s", strings.Repeat("#", depth), heading, body)
		b.WriteString(text)
		sections = append(sections, carrierSection{heading: heading, text: text})
	}
	return b.String(), sections
}

type carrierSection struct {
	heading string
	text    string
}

// GenerateCarriers builds the calibrated carrier set as the compositor sees
// it: full carriers, plus direct references that some carriers declare into
// sections of others. Rendering goes through referencecontract.RenderCarriers
// so that the fixture measures the real compositor and not a hand-written
// imitation of its output.
func GenerateCarriers(shape Shape) []referencecontract.Carrier {
	carriers := make([]referencecontract.Carrier, 0, shape.Carriers)
	sections := make([][]carrierSection, shape.Carriers)
	for i := range shape.Carriers {
		span, secs := carrierSections(shape, fmt.Sprintf("c%d", i))
		sections[i] = secs
		carriers = append(carriers, referencecontract.Carrier{
			Path:     fmt.Sprintf("specs/generated/fixture-carrier-%d.md", i),
			Span:     span,
			Revision: "fixture-head",
			Class:    referencecontract.CarrierParent,
			BlobOID:  fmt.Sprintf("%040x", i+1),
		})
	}
	if shape.Carriers < 2 {
		return carriers
	}

	// The last carrier references sections of the first. Every such
	// reference duplicates text already inlined in full above it.
	target := 0
	source := shape.Carriers - 1
	for r := range shape.DuplicateRefs {
		sec := sections[target][r%len(sections[target])]
		carriers[source].Refs = append(carriers[source].Refs, referencecontract.Reference{
			Path:     carriers[target].Path,
			Heading:  sec.heading,
			Revision: carriers[target].Revision,
			BlobOID:  carriers[target].BlobOID,
			Span:     sec.text,
		})
	}
	// A stale reference: same path, different pinned blob. Its content is
	// not what is inlined, so it must be emitted in full.
	for r := range shape.StaleRefs {
		sec := sections[target][(shape.DuplicateRefs+r)%len(sections[target])]
		carriers[source].Refs = append(carriers[source].Refs, referencecontract.Reference{
			Path:     carriers[target].Path,
			Heading:  sec.heading,
			Revision: "fixture-stale",
			BlobOID:  fmt.Sprintf("%040x", 0xdead),
			Span:     strings.Replace(sec.text, "generated filler", "earlier revision", 1),
		})
	}
	carriers = append(carriers, generateAncestorCarriers(shape)...)
	return carriers
}

// generateAncestorCarriers builds the inherited scalar carriers and their
// references into documents that are not inlined anywhere in the prompt.
func generateAncestorCarriers(shape Shape) []referencecontract.Carrier {
	carriers := make([]referencecontract.Carrier, 0, shape.AncestorCarriers)
	for i := range shape.AncestorCarriers {
		span, _ := sectionedBody(shape, shape.AncestorCarrierBytes, fmt.Sprintf("a%d", i))
		carrier := referencecontract.Carrier{
			Path:     fmt.Sprintf("specs/generated/fixture-ancestor-%d.md", i),
			Span:     span,
			Revision: "fixture-head",
			Class:    referencecontract.CarrierScalar,
			BlobOID:  fmt.Sprintf("%040x", 0xa000+i),
		}
		for r := range shape.AncestorRefs {
			heading := fmt.Sprintf("External %d", r)
			carrier.Refs = append(carrier.Refs, referencecontract.Reference{
				Path:     fmt.Sprintf("specs/generated/fixture-external-%d-%d.md", i, r),
				Heading:  heading,
				Revision: "fixture-head",
				BlobOID:  fmt.Sprintf("%040x", 0xe000+i*shape.AncestorRefs+r),
				Span:     fmt.Sprintf("\n## %s\n\n%s", heading, filler(shape.AncestorRefBytes, fmt.Sprintf("a%d-e%d", i, r))),
			})
		}
		carriers = append(carriers, carrier)
	}
	return carriers
}

// GenerateResolvedReferenceContext renders the carrier block as the
// compositor would present it: the standing preamble followed by the
// reconciled carriers and their references.
func GenerateResolvedReferenceContext(shape Shape) string {
	rendered, err := referencecontract.RenderCarriers(GenerateCarriers(shape))
	if err != nil {
		panic(fmt.Sprintf("fixture carriers do not reconcile: %v", err))
	}
	return `The compositor resolved and freshness-checked the strict carrier content and its
declared direct references before launch. Treat this bounded context as inherited
authority. Do not re-read or replace its pinned source paths with worktree content.
Read outside it only for evidence owned by your stage or an unresolved dependency
that the assigned context identifies.

` + rendered
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
