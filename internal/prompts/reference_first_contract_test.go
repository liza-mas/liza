package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/pipeline"
)

func TestReferenceFirstProducerReviewerPairs(t *testing.T) {
	t.Parallel()

	producerFiles := []string{
		"../../skills/epic-writing/SKILL.md",
		"../../skills/epic-writing/references/epic-format.md",
		"../../skills/user-story-writing/SKILL.md",
		"../../skills/user-story-writing/references/user-story-format.md",
		"../../skills/architecture-planning/SKILL.md",
		"../../skills/architecture-planning/references/arch-plan-format.md",
	}
	for _, path := range producerFiles {
		content := readContractFixture(t, path)
		if !strings.Contains(content, "Source References") {
			t.Errorf("%s does not require the strict Source References carrier", path)
		}
	}

	reviewContract := readContractFixture(t, "../../skills/spec-review/SKILL.md")
	for _, reject := range []string{
		"inherited requirement, constraint, threshold, or interface has no direct reference",
		"unchanged inherited prose", "full-parent read", "conflicts with its inherited owner",
		"second detailed specification", "character-identical",
		"correction accumulates unchanged or resolved history", "mapped anchor span omits that obligation",
	} {
		if !strings.Contains(reviewContract, reject) {
			t.Errorf("spec-review missing rejection class %q", reject)
		}
	}

	pipelineBytes, err := os.ReadFile(filepath.Clean("../embedded/pipeline.yaml"))
	if err != nil {
		t.Fatalf("read embedded pipeline: %v", err)
	}
	pipelineConfig, err := pipeline.LoadFromBytes(pipelineBytes)
	if err != nil {
		t.Fatalf("parse embedded pipeline: %v", err)
	}
	resolver := pipeline.NewResolver(pipelineConfig)
	for _, role := range []string{"epic-plan-reviewer", "us-reviewer", "code-plan-reviewer", "architecture-reviewer"} {
		skills, err := resolver.Skills(role)
		if err != nil {
			t.Errorf("pipeline role %q: %v", role, err)
			continue
		}
		found := false
		for _, skill := range skills {
			found = found || skill == "spec-review"
		}
		if !found {
			t.Errorf("pipeline role %q does not load spec-review", role)
		}
	}
}

func TestReferenceFirstPromptsRemoveSupersededParityMandates(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"templates/blocks/implementation_phase.tmpl",
		"templates/blocks/review_instructions.tmpl",
	} {
		content := readContractFixture(t, path)
		for _, forbidden := range []string{
			"Extract each field verbatim from the plan file",
			"must be identical, not paraphrased or extended",
			"character-identical to the plan",
		} {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s retains superseded parity mandate %q", path, forbidden)
			}
		}
	}
}

func readContractFixture(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
