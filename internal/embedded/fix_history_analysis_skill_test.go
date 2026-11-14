package embedded

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const fixHistorySkillDir = "skills/fix-history-analysis"

var fixHistorySkillFiles = []string{
	"SKILL.md",
	"references/full-mode.md",
	"references/light-mode.md",
}

// fixHistoryFragments pins one fragment per rule of the skill's method, with
// the file that must carry it.
var fixHistoryFragments = []struct {
	rule, file, fragment string
}{
	{"invocation: modes", "SKILL.md", "**mode**: `light` or `full`."},
	{"invocation: repositories", "SKILL.md", "ordered upstream first, each optionally labelled as a fork of an earlier one. Default: the current repository alone."},
	{"invocation: window", "SKILL.md", "`SINCE` and `UNTIL` git dates, both required, passed to the record query unchanged."},
	{"read-only", "SKILL.md", "Write no file, commit or goal: drafts and proposals go in the returned report."},
	{"read-only: light mode stays local", "SKILL.md", "Light mode never leaves its listed inputs; full mode may also read a linked ticket for a thin message."},
	{"evidence", "SKILL.md", "Every item cites SHAs and the command or `file:line` it came from."},
	{"evidence: verified marks and hand counts", "SKILL.md", "Mark each claim verified or not, and counts done by hand as such."},
	{"evidence: chronology", "SKILL.md", "Check chronology (committer dates) before any causal claim."},
	{"census", "SKILL.md", "The report opens with each repository's `HEAD` SHA, the window, and the exact commands whose output gave each count, so the counts reproduce."},
	{"populations: separate", "SKILL.md", "Count and classify separately, dropping none:"},
	{"populations: product", "SKILL.md", "product fixes;"},
	{"populations: in-run", "SKILL.md", "in-run corrections: the fix and the code it repairs entered the main line through the same merge"},
	{"populations: proof", "SKILL.md", "test or proof fixes: the diff touches only tests or proof fixtures."},
	{"young code: hunks", "SKILL.md", "`git show -M -U0` hunk (`-a,b`: lines a..a+b-1; `-a,0` with a >= 1: line a, which the insertion follows)"},
	{"young code: blame on first parent", "SKILL.md", "`git blame --porcelain` on F's first parent"},
	{"young code: newest line, 30 days", "SKILL.md", "when its newest blamed line's committer time is at most 30 days (30 x 86400 s) before F's."},
	{"young code: not measurable", "SKILL.md", "is *not measurable*: count it apart and exclude it from both terms."},
	{"young code: reporting", "SKILL.md", "Report `young/measurable` overall and per population, with the not-measurable count."},
	{"thin messages", "SKILL.md", "**Thin messages** lower confidence and send you to the diff."},
	{"thin messages: which source was read", "SKILL.md", "Say which was read, or that a ticket was not readable."},
	{"report headings", "SKILL.md", "Each section is a Markdown heading carrying the section's name"},

	{"full: gather period query", "references/full-mode.md", "Run the Period query: fix commits and their record fields. A record, when present, is the starting point."},
	{"full: gather reverts", "references/full-mode.md", "`git log` subjects of type `revert` or git's `Revert \"...\"`."},
	{"full: gather messages and diffs", "references/full-mode.md", "Read each commit's full message and diff."},
	{"full: placement same SHA", "references/full-mode.md", "a commit reachable in two repositories with one SHA counts once."},
	{"full: placement patch-id", "references/full-mode.md", "Two SHAs with equal `git patch-id --stable`, or a `(cherry picked from commit ...)` line, are one fix, attributed to the upstream-most repository."},
	{"full: delegation", "references/full-mode.md", "A read-only subagent classifies each batch per generic-subagent and returns the digest."},
	{"full: unavailable without delegation", "references/full-mode.md", "A session that cannot delegate stops before classifying and reports full mode unavailable; it never substitutes an inline pass."},
	{"full: digest entry per fix", "references/full-mode.md", "One digest entry per fix, with fields:"},
	{"full: digest repository, SHA, population", "references/full-mode.md", "repository, SHA, population;"},
	{"full: digest message versus diff", "references/full-mode.md", "message versus diff: `consistent` or `misleading`, with the diff evidence;"},
	{"full: digest untested condition", "references/full-mode.md", "untested condition: one of upgrade, second instance, other platform or provider, inherited environment, existing user files, slow or failing dependency, or a derived one;"},
	{"full: causes never preset", "references/full-mode.md", "structural cause: derived from the evidence, never from a preset list;"},
	{"full: digest lineage", "references/full-mode.md", "lineage: `repairs <sha>` with its age, `shape seen at <sha>`, or none;"},
	{"full: digest evidence and confidence", "references/full-mode.md", "evidence, confidence with its reason, verified mark."},
	{"full: two independent classifiers per batch", "references/full-mode.md", "Classify every batch twice, with two independent subagents given the same batch and no classification, neither seeing the other's digest."},
	{"full: field-by-field comparison", "references/full-mode.md", "Compare the two digests field by field."},
	{"full: source re-read per divergence", "references/full-mode.md", "For each divergence re-read the source (message, diff, blame) and report it *resolved*, with the evidence, or *unresolved*, with both readings."},
	{"full: reconciliation result mark", "references/full-mode.md", "The report states the result as *reconciled* when no divergence is unresolved, else *reconciled, N unresolved*."},
	{"full: coverage vocabulary", "references/full-mode.md", "check each cause against the invariant file and the guardrails file, in the record's classification vocabulary: gap, unenforced, not consulted, none needed."},
	{"full: process cause, repairs of fixes", "references/full-mode.md", "fixes repairing recent fixes;"},
	{"full: process cause, same shape", "references/full-mode.md", "the same shape twice;"},
	{"full: process cause, narrow triggers", "references/full-mode.md", "triggers drawn too narrowly;"},
	{"full: process cause, concepts shared early", "references/full-mode.md", "concepts shared before being defined."},
	{"full: report sections", "references/full-mode.md", "Census; Populations; Weaknesses (principle altitude, evidence and confidence per cause); Coverage; Process findings; Direction (young-code share); Draft goal (one per repository)."},
	{"full: coverage-first draft", "references/full-mode.md", "states that a past fix establishes a concern, not a current defect, and that an invariant already enforced is discharged with proof, not new code."},
	{"full: upstream draft", "references/full-mode.md", "A fix attributed upstream is drafted upstream, with how its change reaches there: a run upstream, or commits cherry-picked from the fork."},

	{"light: collects itself", "references/light-mode.md", "The skill collects its inputs itself, with git and file reads only, and reads nothing else."},
	{"light: input records", "references/light-mode.md", "The period's records: the Period query with the window."},
	{"light: input invariant and guardrails files", "references/light-mode.md", "The invariant file (`INVARIANTS.md`) and the guardrails file (`GUARDRAILS.md`) at each repository root, read as found."},
	{"light: invariant IDs", "references/light-mode.md", "An invariant's ID is the first backticked token of its Invariant cell."},
	{"light: trigger text", "references/light-mode.md", "Trigger text is the `**Triggers:**` line of the invariant-compliance rule, or the rule's prose where the project wrote it so."},
	{"light: input lessons", "references/light-mode.md", "files the guardrails file indexes as lessons, else a `lessons/` directory at the root."},
	{"light: input fix show and blame", "references/light-mode.md", "Each listed fix's `git show` and blame on its parent"},
	{"light: input later commits", "references/light-mode.md", "`git log` over the period for later commits whose message contains a *scheduled* record's owner reference."},
	{"light: unreadable records", "references/light-mode.md", "Unreadable: a listed fix whose record fields are empty, hold a value outside the record's closed sets, or hold two values in a single-valued field. List them by SHA and exclude them from every count below."},
	{"light: widened triggers rule", "references/light-mode.md", "clusters of two or more *not-consulted* records on one concept area (the same invariant ID, or code boundaries in the same parent directory) that no trigger text names."},
	{"light: open dispositions", "references/light-mode.md", "owner `human` means awaiting the human)."},
	{"light: accepted voided by recurrence", "references/light-mode.md", "as an acceptance a recurrence voids."},
	{"light: fixed-twice window", "references/light-mode.md", "those within 14 days marked"},
	{"light: uncited invariants", "references/light-mode.md", "file IDs minus concepts cited in the period, for retain, merge or retire."},
	{"light: rows without ID not computable", "references/light-mode.md", "Rows without an ID are counted apart as not computable, with the fix bodies that name them."},
	{"light: no invariant file", "references/light-mode.md", "With no invariant file, items 4 and 7 say *not computable*, never empty."},
	{"light: none-needed share", "references/light-mode.md", "the *none-needed* share of readable records (a high share is a finding)"},
	{"light: proposes, human decides", "references/light-mode.md", "The skill proposes; the human decides."},
}

// fixHistoryLightSections are the nine light report sections in order; Scope 6
// cites these names.
var fixHistoryLightSections = []string{
	"Period and census",
	"Widened triggers",
	"Gaps",
	"Unenforced invariants",
	"Open dispositions",
	"Fixed-twice intervals",
	"Uncited invariants",
	"Lesson candidates",
	"Shares",
}

func readFixHistorySkillFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), filepath.FromSlash(fixHistorySkillDir), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func normalizedFixHistorySkillFile(t *testing.T, rel string) string {
	t.Helper()
	return strings.Join(strings.Fields(readFixHistorySkillFile(t, rel)), " ")
}

func TestFixHistoryAnalysisPinsMethod(t *testing.T) {
	content := map[string]string{}
	for _, file := range fixHistorySkillFiles {
		content[file] = normalizedFixHistorySkillFile(t, file)
	}
	for _, c := range fixHistoryFragments {
		body, ok := content[c.file]
		if !ok {
			t.Fatalf("fragment %q names unknown file %s", c.rule, c.file)
		}
		if !strings.Contains(body, c.fragment) {
			t.Errorf("%s: missing rule %q: %q", c.file, c.rule, c.fragment)
		}
	}
}

func TestFixHistoryAnalysisLightSectionsInOrder(t *testing.T) {
	body := normalizedFixHistorySkillFile(t, "references/light-mode.md")
	pos := 0
	for i, name := range fixHistoryLightSections {
		item := strconv.Itoa(i+1) + ". **" + name + "**:"
		idx := strings.Index(body[pos:], item)
		if idx < 0 {
			t.Fatalf("light section %d %q missing or out of order (expected %q after offset %d)", i+1, name, item, pos)
		}
		pos += idx + len(item)
	}
}

func TestFixHistoryAnalysisCitesRecordQuery(t *testing.T) {
	for _, file := range fixHistorySkillFiles {
		if strings.Contains(readFixHistorySkillFile(t, file), "%(trailers:") {
			t.Errorf("%s copies the record query format string; cite the Period query instead", file)
		}
	}
	skill := normalizedFixHistorySkillFile(t, "SKILL.md")
	want := "Both run the Period query of the [prevention record](../shared/references/prevention-record.md) as written there."
	if !strings.Contains(skill, want) {
		t.Errorf("SKILL.md must cite the Period query of the prevention record: %q", want)
	}
	record, err := os.ReadFile(filepath.Join(findRepoRoot(t), "skills", "shared", "references", "prevention-record.md"))
	if err != nil {
		t.Fatalf("read prevention record: %v", err)
	}
	if !strings.Contains(string(record), "\n### Period query\n") {
		t.Error("prevention-record.md has no '### Period query' heading")
	}
}

var fixHistoryLinkPattern = regexp.MustCompile(`\]\(([^)#\s]+)(?:#[^)\s]*)?\)`)

func TestFixHistoryAnalysisLinksResolve(t *testing.T) {
	skill := readFixHistorySkillFile(t, "SKILL.md")
	for _, rel := range []string{
		"references/full-mode.md",
		"references/light-mode.md",
		"../shared/references/prevention-record.md",
		"../generic-subagent/SKILL.md",
	} {
		if !strings.Contains(skill, "]("+rel) {
			t.Errorf("SKILL.md does not link %s", rel)
		}
	}
	root := filepath.Join(findRepoRoot(t), filepath.FromSlash(fixHistorySkillDir))
	for _, file := range fixHistorySkillFiles {
		dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(file)))
		for _, m := range fixHistoryLinkPattern.FindAllStringSubmatch(readFixHistorySkillFile(t, file), -1) {
			if strings.Contains(m[1], "://") {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m[1]))); err != nil {
				t.Errorf("%s links %s, which does not resolve", file, m[1])
			}
		}
	}
}

func TestFixHistoryAnalysisBrandNeutral(t *testing.T) {
	for _, file := range fixHistorySkillFiles {
		lower := strings.ToLower(readFixHistorySkillFile(t, file))
		for _, banned := range []string{"liza", "omni", "§"} {
			if strings.Contains(lower, banned) {
				t.Errorf("%s contains %q", file, banned)
			}
		}
	}
}

func TestFixHistoryAnalysisListedInSkillTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), "docs", "liza-hardened-mas.md"))
	if err != nil {
		t.Fatalf("read skill table doc: %v", err)
	}
	if !strings.Contains(string(raw), "\n| **fix-history-analysis** | ") {
		t.Error("docs/liza-hardened-mas.md skill table has no fix-history-analysis row")
	}
}
