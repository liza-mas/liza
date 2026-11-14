package embedded

import (
	"strings"
	"testing"
)

// Pinned text tests for FR6: both retrospective protocols name the
// fix-history-analysis light mode as the review's input and the human's
// decisions by report section.

const (
	recurringReviewPairingFile = "contracts/PAIRING_MODE.md"
	recurringReviewSprintFile  = "specs/protocols/sprint-governance.md"
	recurringReviewWakeFile    = "internal/prompts/templates/wake_sprint_complete.tmpl"
	recurringReviewSkillRef    = "skills/fix-history-analysis/references/light-mode.md"
	recurringReviewLightRun    = "internal/historyfixture/testdata/recorded-runs/light-run.md"
	recurringReviewHeading     = "## Retrospective Protocol"
)

// recurringReviewDecisionSections are the report sections the human decides
// (Period and census is data, not a decision).
var recurringReviewDecisionSections = []string{
	"Widened triggers",
	"Gaps",
	"Unenforced invariants",
	"Open dispositions",
	"Fixed-twice intervals",
	"Uncited invariants",
	"Lesson candidates",
	"Shares",
}

var recurringReviewDecisionWords = []string{
	"principle-level", "done with proof", "scheduled", "accepted", "recalibrat",
	"retain", "merge", "retire", "capture", "none-needed", "young-code", "not gated",
}

var recurringReviewInputMarkers = []string{"fix-history-analysis", "light mode", "period query"}

func recurringReviewNormalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// recurringReviewSection returns the lines from the exact heading to the first
// later line for which isEnd holds, or to the end of the file.
func recurringReviewSection(t *testing.T, file string, isEnd func(string) bool) string {
	t.Helper()
	lines := strings.Split(string(readRepoFile(t, file)), "\n")
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") != recurringReviewHeading {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if isEnd(strings.TrimRight(next, " \t\r")) {
				break
			}
			body = append(body, next)
		}
		return strings.Join(body, "\n")
	}
	t.Fatalf("%s: heading %q not found", file, recurringReviewHeading)
	return ""
}

func recurringReviewPairingSection(t *testing.T) string {
	return recurringReviewSection(t, recurringReviewPairingFile, func(l string) bool { return l == "---" })
}

func recurringReviewSprintSection(t *testing.T) string {
	return recurringReviewSection(t, recurringReviewSprintFile, func(l string) bool { return strings.HasPrefix(l, "## ") })
}

type recurringReviewScope struct {
	file string
	body string
}

func recurringReviewScopes(t *testing.T) []recurringReviewScope {
	return []recurringReviewScope{
		{recurringReviewPairingFile, recurringReviewNormalize(recurringReviewPairingSection(t))},
		{recurringReviewSprintFile, recurringReviewNormalize(recurringReviewSprintSection(t))},
	}
}

func recurringReviewRequire(t *testing.T, scopes []recurringReviewScope, fragments []string) {
	t.Helper()
	for _, s := range scopes {
		for _, f := range fragments {
			if !strings.Contains(s.body, strings.ToLower(f)) {
				t.Errorf("%s § Retrospective Protocol: missing %q", s.file, f)
			}
		}
	}
}

func TestRecurringReviewNamesLightModeInput(t *testing.T) {
	recurringReviewRequire(t, recurringReviewScopes(t), recurringReviewInputMarkers)
}

func TestRecurringReviewNamesDecisions(t *testing.T) {
	scopes := recurringReviewScopes(t)
	recurringReviewRequire(t, scopes, recurringReviewDecisionSections)
	recurringReviewRequire(t, scopes, recurringReviewDecisionWords)
}

func TestRecurringReviewHumanRunsAtCheckpoint(t *testing.T) {
	sprint := recurringReviewNormalize(recurringReviewSprintSection(t))
	for _, f := range []string{"human", "checkpoint review"} {
		if !strings.Contains(sprint, f) {
			t.Errorf("%s § Retrospective Protocol: missing %q", recurringReviewSprintFile, f)
		}
	}

	doc := string(readRepoFile(t, recurringReviewSprintFile))
	_, rest, ok := strings.Cut(doc, "### Checkpoint Review Checklist")
	if !ok {
		t.Fatalf("%s: checklist heading not found", recurringReviewSprintFile)
	}
	checklist, _, _ := strings.Cut(rest, "\n---")
	if !strings.Contains(checklist, "fix-history-analysis") {
		t.Errorf("%s: Checkpoint Review Checklist lacks fix-history-analysis", recurringReviewSprintFile)
	}

	if strings.Contains(string(readRepoFile(t, recurringReviewWakeFile)), "fix-history-analysis") {
		t.Errorf("%s must not name fix-history-analysis (the wake stays unchanged)", recurringReviewWakeFile)
	}
}

func TestRecurringReviewRecordsDecisionsAsText(t *testing.T) {
	section := recurringReviewSprintSection(t)
	_, afterOpen, ok := strings.Cut(section, "### Retrospective Output")
	if !ok {
		t.Fatalf("%s: Retrospective Output heading not found", recurringReviewSprintFile)
	}
	// Skip the preserved YAML example: text after its closing fence.
	_, afterFence, ok := strings.Cut(afterOpen, "```yaml")
	if !ok {
		t.Fatalf("%s: YAML example not found", recurringReviewSprintFile)
	}
	_, after, ok := strings.Cut(afterFence, "\n```")
	if !ok {
		t.Fatalf("%s: YAML example is not closed", recurringReviewSprintFile)
	}
	if !strings.Contains(after, "retrospective: |") {
		t.Errorf("%s: Output text after the YAML example lacks the block scalar snippet", recurringReviewSprintFile)
	}
	if strings.Contains(strings.ToLower(after), "actions") {
		t.Errorf("%s: Output text after the YAML example must not name actions", recurringReviewSprintFile)
	}
}

func TestRecurringReviewMatchesSkillSections(t *testing.T) {
	skill := recurringReviewNormalize(string(readRepoFile(t, recurringReviewSkillRef)))
	for _, name := range recurringReviewDecisionSections {
		if !strings.Contains(skill, strings.ToLower(name)) {
			t.Errorf("%s lacks cited section %q", recurringReviewSkillRef, name)
		}
	}
}

func TestRecurringReviewKeepsExistingRetrospective(t *testing.T) {
	section := recurringReviewPairingSection(t)
	for _, f := range []string{"(L)ight / (H)eavy / (S)kip", "**Light", "**Heavy"} {
		if !strings.Contains(section, f) {
			t.Errorf("%s § Retrospective Protocol: lost %q", recurringReviewPairingFile, f)
		}
	}
	for _, raw := range []string{"Liza", "LIZA", "liza", "Omni"} {
		if strings.Contains(section, raw) {
			t.Errorf("%s § Retrospective Protocol: raw brand literal %q", recurringReviewPairingFile, raw)
		}
	}
}

func TestRecurringReviewDryRunListsCitedSections(t *testing.T) {
	run := recurringReviewNormalize(string(readRepoFile(t, recurringReviewLightRun)))
	names := append([]string{"Period and census"}, recurringReviewDecisionSections...)
	for _, name := range names {
		if !strings.Contains(run, strings.ToLower(name)) {
			t.Errorf("%s lacks cited section %q", recurringReviewLightRun, name)
		}
	}
}
