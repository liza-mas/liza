// Package rolebudget measures, per role variant, the bytes an agent receives
// before it acts: its rendered prompt and the mandatory skill/reference files
// its role tells it to read. The goal that introduced it caps instruction
// growth at 5% per role variant in each column; the committed baseline is the
// anchor that cap is measured against.
//
// The role context renders through the same fixture as promptbench so the two
// instruments share one shape, with the inlined reference context blanked:
// this instrument measures instruction text, promptbench measures payload.
// The decomposition-root variant is produced by the same function the
// compositor uses (agent.TaskContextSections), not a copy of its rule; the
// orchestrator is measured per wake trigger, where its instructions live.
package rolebudget

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/prompts/promptbench"
)

// Report is the committed baseline artifact.
type Report struct {
	// Gate is false while the release is being assembled (the test reports
	// only) and true once the ceiling is asserted against this baseline.
	Gate     bool          `json:"gate"`
	Variants []RoleVariant `json:"variants"`
}

// RoleVariant is one row: a role, rendered in its specialized or
// decomposition-root form.
type RoleVariant struct {
	Variant string `json:"variant"`
	Role    string `json:"role"`
	// RenderedBytes is the base prompt plus the variant's instruction
	// sections; BasePromptBytes is the shared part of it, for transparency.
	RenderedBytes      int      `json:"rendered_bytes"`
	BasePromptBytes    int      `json:"base_prompt_bytes"`
	MandatoryReadBytes int      `json:"mandatory_read_bytes"`
	Reads              []string `json:"reads"`
}

// Ceiling is the allowed growth per column per variant, as a fraction.
const Ceiling = 0.05

// Measure renders every role in the embedded pipeline and sums its mandatory
// reads from the repository masters under repoRoot.
func Measure(repoRoot string) (Report, error) {
	config, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		return Report{}, err
	}
	resolver := pipeline.NewResolver(config)
	shape := promptbench.CalibratedShape()

	roles := make([]string, 0, len(config.Pipeline.Roles))
	for name := range config.Pipeline.Roles {
		roles = append(roles, name)
	}
	sort.Strings(roles)

	var report Report
	for _, role := range roles {
		roleType, err := resolver.RoleType(role)
		if err != nil {
			return Report{}, err
		}
		reads, readBytes, err := mandatoryReads(repoRoot, resolver, role)
		if err != nil {
			return Report{}, err
		}
		base, err := prompts.BuildBasePrompt(prompts.BasePromptConfig{
			Role: role, AgentID: role + "-1", TaskID: "fixture-cp-1", SpecsDir: "specs",
			ProjectRoot: "/generated", StatePath: "/generated/state.yaml",
			GoalDesc: "generated goal", GoalSpecRef: "specs/generated/goal.md",
		})
		if err != nil {
			return Report{}, fmt.Errorf("base prompt for %s: %w", role, err)
		}
		if roleType == "orchestrator" {
			// The orchestrator's instructions live in its wake templates,
			// selected by trigger; each is a variant. The dashboard is run
			// data and is not rendered here.
			for _, trigger := range prompts.WakeTriggers {
				rendered, err := prompts.RenderWakeInstructions(trigger, role+"-1")
				if err != nil {
					return Report{}, fmt.Errorf("render %s wake %s: %w", role, trigger, err)
				}
				report.Variants = append(report.Variants, RoleVariant{
					Variant:            role + " (wake " + trigger + ")",
					Role:               role,
					RenderedBytes:      len(base) + len(rendered),
					BasePromptBytes:    len(base),
					MandatoryReadBytes: readBytes,
					Reads:              reads,
				})
			}
			continue
		}
		for _, pair := range variantPairs(config, role) {
			rendered, err := renderVariant(resolver, shape, role, roleType, pair)
			if err != nil {
				return Report{}, fmt.Errorf("render %s via %s: %w", role, pair.name, err)
			}
			report.Variants = append(report.Variants, RoleVariant{
				Variant:            pair.variant(role),
				Role:               role,
				RenderedBytes:      len(base) + len(rendered),
				BasePromptBytes:    len(base),
				MandatoryReadBytes: readBytes,
				Reads:              reads,
			})
		}
	}
	return report, nil
}

// rolePair is the pair a role is rendered through; the empty pair renders a
// role that belongs to no pair (the orchestrator).
type rolePair struct {
	name string
	root bool
}

func (p rolePair) variant(role string) string {
	if p.root {
		return role + " (decomposition root)"
	}
	return role
}

// variantPairs returns one specialized pair and, when the role is a doer or
// reviewer of a decomposition-root pair, one root pair. Pairs of the same
// kind render identically, so one of each suffices.
func variantPairs(config *pipeline.PipelineConfig, role string) []rolePair {
	var specialized, root *rolePair
	names := make([]string, 0, len(config.Pipeline.RolePairs))
	for name := range config.Pipeline.RolePairs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		def := config.Pipeline.RolePairs[name]
		if def.Doer != role && def.Reviewer != role {
			continue
		}
		pair := rolePair{name: name, root: def.DecompositionRoot}
		switch {
		case def.DecompositionRoot && root == nil:
			root = &pair
		case !def.DecompositionRoot && specialized == nil:
			specialized = &pair
		}
	}
	if specialized == nil {
		specialized = &rolePair{}
	}
	pairs := []rolePair{*specialized}
	if root != nil {
		pairs = append(pairs, *root)
	}
	return pairs
}

func renderVariant(resolver *pipeline.Resolver, shape promptbench.Shape, role, roleType string, pair rolePair) (string, error) {
	sections, err := resolver.ContextSections(role)
	if err != nil {
		return "", err
	}
	data := promptbench.FixtureRoleContext(shape, role, role+"-1", roleType)
	// The budget measures instruction text. The inlined reference context is
	// run payload — promptbench's subject, not this one's — and at ~265 KB it
	// would make a 5% ceiling meaningless for instruction growth.
	data.ResolvedReferenceContext = ""
	task := &models.Task{ID: data.TaskID, RolePair: pair.name}
	sections, err = agent.TaskContextSections(sections, task, data, resolver)
	if err != nil {
		return "", err
	}
	return prompts.BuildRoleContext(role, sections, data)
}

// sharedReferenceLink matches a relative link from a skill into the shared
// references directory, with or without a heading anchor, which the skill
// tells its reader to open.
var sharedReferenceLink = regexp.MustCompile(`\]\((?:\.\./)+(?:skills/)?shared/references/([A-Za-z0-9._-]+\.md)(?:#[^)]*)?\)`)

// mandatoryReads lists the repository files a role must read before acting:
// its skills, its mandatory docs, and the shared references those skills link.
// Each file counts once.
func mandatoryReads(repoRoot string, resolver *pipeline.Resolver, role string) ([]string, int, error) {
	skills, err := resolver.Skills(role)
	if err != nil {
		return nil, 0, err
	}
	docs, err := resolver.MandatoryDocs(role)
	if err != nil {
		return nil, 0, err
	}
	seen := map[string]bool{}
	var files []string
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			files = append(files, rel)
		}
	}
	for _, skill := range skills {
		add(filepath.ToSlash(filepath.Join("skills", skill, "SKILL.md")))
	}
	for _, doc := range docs {
		add(filepath.ToSlash(doc))
	}
	// Shared references linked from a skill are part of that skill's read.
	for i := 0; i < len(files); i++ {
		content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(files[i])))
		if err != nil {
			return nil, 0, fmt.Errorf("mandatory read %s for %s: %w", files[i], role, err)
		}
		for _, m := range sharedReferenceLink.FindAllStringSubmatch(string(content), -1) {
			add("skills/shared/references/" + m[1])
		}
	}
	sort.Strings(files)
	total := 0
	for _, rel := range files {
		info, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			return nil, 0, err
		}
		total += int(info.Size())
	}
	return files, total, nil
}

// Delta is one variant's movement against the baseline.
type Delta struct {
	Variant           string
	RenderedBefore    int
	RenderedAfter     int
	MandatoryBefore   int
	MandatoryAfter    int
	RenderedGrowth    float64
	MandatoryGrowth   float64
	ExceedsCeiling    bool
	MissingInBaseline bool
	MissingInCurrent  bool
}

// Compare pairs the current report with the baseline by variant name.
func Compare(baseline, current Report) []Delta {
	before := map[string]RoleVariant{}
	for _, v := range baseline.Variants {
		before[v.Variant] = v
	}
	deltas := make([]Delta, 0, len(current.Variants))
	for _, v := range current.Variants {
		d := Delta{Variant: v.Variant, RenderedAfter: v.RenderedBytes, MandatoryAfter: v.MandatoryReadBytes}
		b, ok := before[v.Variant]
		if !ok {
			d.MissingInBaseline = true
			deltas = append(deltas, d)
			continue
		}
		d.RenderedBefore, d.MandatoryBefore = b.RenderedBytes, b.MandatoryReadBytes
		d.RenderedGrowth = growth(b.RenderedBytes, v.RenderedBytes)
		d.MandatoryGrowth = growth(b.MandatoryReadBytes, v.MandatoryReadBytes)
		d.ExceedsCeiling = d.RenderedGrowth > Ceiling || d.MandatoryGrowth > Ceiling
		deltas = append(deltas, d)
	}
	// A variant that was in the baseline but is not measured any more — a
	// dropped wake trigger or role — must not vanish from the table silently.
	measured := map[string]bool{}
	for _, v := range current.Variants {
		measured[v.Variant] = true
	}
	for _, v := range baseline.Variants {
		if !measured[v.Variant] {
			deltas = append(deltas, Delta{Variant: v.Variant, RenderedBefore: v.RenderedBytes, MandatoryBefore: v.MandatoryReadBytes, MissingInCurrent: true})
		}
	}
	return deltas
}

func growth(before, after int) float64 {
	if before == 0 {
		return 0
	}
	return float64(after-before) / float64(before)
}

// Table renders the deltas as the report the goal asks for: bytes and deltas
// per variant, never averaged.
func Table(deltas []Delta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-44s %10s %10s %8s   %10s %10s %8s\n", "variant", "prompt", "->", "delta", "reads", "->", "delta")
	for _, d := range deltas {
		if d.MissingInBaseline {
			fmt.Fprintf(&b, "%-44s %10d %10s %8s   %10d %10s %8s  (not in baseline)\n", d.Variant, d.RenderedAfter, "", "", d.MandatoryAfter, "", "")
			continue
		}
		if d.MissingInCurrent {
			fmt.Fprintf(&b, "%-44s %10d %10s %8s   %10d %10s %8s  (no longer measured)\n", d.Variant, d.RenderedBefore, "", "", d.MandatoryBefore, "", "")
			continue
		}
		flag := ""
		if d.ExceedsCeiling {
			flag = "  OVER"
		}
		fmt.Fprintf(&b, "%-44s %10d %10d %+7.1f%%   %10d %10d %+7.1f%%%s\n",
			d.Variant, d.RenderedBefore, d.RenderedAfter, 100*d.RenderedGrowth,
			d.MandatoryBefore, d.MandatoryAfter, 100*d.MandatoryGrowth, flag)
	}
	return b.String()
}

// JSON renders the report for the committed baseline.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
