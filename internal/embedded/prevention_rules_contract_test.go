package embedded

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/gitbash"
	"github.com/liza-mas/liza/internal/preventionrecord/recordfixture"
)

const (
	preventionRecordDoc = "skills/shared/references/prevention-record.md"
	// fixedTwicePrefix is the blocked_reason literal that the multi-agent
	// annex row and the blocked-wake case must both carry.
	fixedTwicePrefix = "`fixed-twice:`"
	// The check's window starts at the epoch and ends now (CORE Rule 2).
	fixedTwiceSince = "SINCE=1970-01-01"
	fixedTwiceUntil = "UNTIL=now"
)

func preventionRulesRead(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// preventionRulesBetween returns text from the first start to the next end
// after it (end excluded); an empty end means the rest of the text.
func preventionRulesBetween(t *testing.T, name, text, start, end string) string {
	t.Helper()
	i := strings.Index(text, start)
	if i < 0 {
		t.Fatalf("%s: missing anchor %q", name, start)
	}
	rest := text[i:]
	if end == "" {
		return rest
	}
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		t.Fatalf("%s: missing end anchor %q after %q", name, end, start)
	}
	return rest[:len(start)+j]
}

// preventionRulesParagraph returns the blank-line-delimited paragraph holding anchor.
func preventionRulesParagraph(t *testing.T, name, text, anchor string) string {
	t.Helper()
	i := strings.Index(text, anchor)
	if i < 0 {
		t.Fatalf("%s: missing anchor %q", name, anchor)
	}
	start := strings.LastIndex(text[:i], "\n\n") + 2
	if start < 2 {
		start = 0
	}
	end := strings.Index(text[i:], "\n\n")
	if end < 0 {
		return text[start:]
	}
	return text[start : i+end]
}

// preventionRulesLine returns the single line holding anchor.
func preventionRulesLine(t *testing.T, name, text, anchor string) string {
	t.Helper()
	var found []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, anchor) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s: %d lines hold %q, want exactly one", name, len(found), anchor)
	}
	return found[0]
}

func preventionRulesRequire(t *testing.T, name, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("%s: missing marker %q", name, fragment)
		}
	}
}

func preventionRulesRequireFold(t *testing.T, name, text string, fragments ...string) {
	t.Helper()
	lowered := strings.ToLower(text)
	for _, fragment := range fragments {
		if !strings.Contains(lowered, strings.ToLower(fragment)) {
			t.Errorf("%s: missing marker %q", name, fragment)
		}
	}
}

func preventionRulesForbid(t *testing.T, name, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if strings.Contains(text, fragment) {
			t.Errorf("%s: forbidden text %q", name, fragment)
		}
	}
}

func preventionRulesCore(t *testing.T) string {
	return preventionRulesRead(t, "contracts/CORE.md")
}

func preventionRulesRule(t *testing.T, number, next string) string {
	t.Helper()
	core := preventionRulesCore(t)
	return preventionRulesBetween(t, "CORE Rule "+number, core, "### Rule "+number+":", "### Rule "+next+":")
}

func preventionRulesFixedTwiceParagraph(t *testing.T) string {
	t.Helper()
	return preventionRulesParagraph(t, "CORE Rule 2", preventionRulesRule(t, "2", "3"), "**Fixed-twice check")
}

func preventionRulesStopTriggerRow(t *testing.T) string {
	t.Helper()
	table := preventionRulesBetween(t, "CORE Stop Triggers", preventionRulesCore(t), "**Stop Triggers:**", "\n---")
	return preventionRulesLine(t, "CORE Stop Triggers", strings.ToLower(table), "fixed-twice stop trigger")
}

func preventionRulesMultiAgentRows(t *testing.T) (answer, trigger string) {
	t.Helper()
	section := preventionRulesBetween(t, "MULTI_AGENT_MODE overrides", preventionRulesRead(t, "contracts/MULTI_AGENT_MODE.md"),
		"## CORE Rule Overrides", "\n---")
	return preventionRulesLine(t, "MULTI_AGENT_MODE overrides", section, "**Rule 11 prevention answer**"),
		preventionRulesLine(t, "MULTI_AGENT_MODE overrides", section, "**Fixed-twice stop trigger**")
}

func preventionRulesWakeCase(t *testing.T) string {
	t.Helper()
	wake := preventionRulesRead(t, "internal/prompts/templates/wake_blocked_tasks.tmpl")
	step2 := preventionRulesBetween(t, "wake_blocked_tasks step 2", wake, "2. Determine if blocker is:", "\n3. ")
	return preventionRulesLine(t, "wake_blocked_tasks step 2", step2, fixedTwicePrefix)
}

func TestPreventionRulesBugFixDoD(t *testing.T) {
	t.Parallel()
	dod := preventionRulesRule(t, "3", "4")
	item := preventionRulesLine(t, "CORE Rule 3", dod, "Bug fix")
	if !strings.HasPrefix(item, "- [ ] ") {
		t.Errorf("CORE Rule 3: bug-fix entry is not a checklist item: %q", item)
	}
	preventionRulesRequire(t, "CORE Rule 3 bug-fix item", item, "prevention answer", "Rule 11")

	rca := preventionRulesRule(t, "11", "12")
	preventionRulesRequire(t, "CORE Rule 11", rca, "**Prevention answer", preventionRecordDoc, "Rule 2", "→ prevention answer")
	preventionRulesForbid(t, "CORE Rule 11", rca, "propose countermeasures")
	if _, err := os.Stat(filepath.Join(findRepoRoot(t), filepath.FromSlash(preventionRecordDoc))); err != nil {
		t.Errorf("record document %s does not resolve: %v", preventionRecordDoc, err)
	}
}

func TestPreventionRulesDebuggingSkill(t *testing.T) {
	t.Parallel()
	skill := preventionRulesRead(t, "skills/debugging/SKILL.md")
	section := func(start, end string) string {
		return preventionRulesBetween(t, "debugging "+start, skill, start, end)
	}
	preventionRulesRequire(t, "Quick Wins", section("# Quick Wins", "\n# Fast Path"), "prevention answer", "sibling search")
	fast := section("**Format:**", "The above verification")
	preventionRulesRequire(t, "Fast Path format", fast, "Prevention:")
	gate := section("Root Cause Gate:", "Proceed to Resolution")
	preventionRulesRequire(t, "Root Cause Gate", gate, "Fix history:", "Rule 2")
	resolution := section("## Resolution", "**Post-Resolution Reflection:**")
	preventionRulesRequire(t, "Resolution", resolution, "sibling", "disposition")
	reflection := section("**Post-Resolution Reflection:**", "**Stop-on-Repeat:**")
	preventionRulesRequire(t, "Post-Resolution Reflection", reflection, "Prevention answer")
}

func TestPreventionRulesFixedTwiceCheck(t *testing.T) {
	t.Parallel()
	core := preventionRulesCore(t)
	check := preventionRulesFixedTwiceParagraph(t)
	preventionRulesRequire(t, "CORE Rule 2 fixed-twice check", check,
		"**Fixed-twice check", "14 days", "same shell call", "Fixed-Twice-Design", "per held concept",
		fixedTwiceSince, fixedTwiceUntil, preventionRecordDoc, "Fixed-Twice-Design,valueonly")
	if n := strings.Count(core, "14 days"); n != 1 {
		t.Errorf("CORE holds %d occurrences of \"14 days\", want 1 (the Rule 2 check)", n)
	}

	row := preventionRulesStopTriggerRow(t)
	preventionRulesRequire(t, "CORE stop-trigger row", row, "fixed-twice stop trigger", "design before code")
}

func TestPreventionRulesModeBehavior(t *testing.T) {
	t.Parallel()
	_, trigger := preventionRulesMultiAgentRows(t)
	preventionRulesRequire(t, "MULTI_AGENT_MODE fixed-twice row", trigger, "write-checkpoint", fixedTwicePrefix, "architect")

	wake := preventionRulesWakeCase(t)
	preventionRulesRequire(t, "wake_blocked_tasks fixed-twice case", wake, fixedTwicePrefix, "architecture-pair", "--awaits", "reviewed design", "only the pairs")

	pairing := preventionRulesBetween(t, "PAIRING_MODE extensions", preventionRulesRead(t, "contracts/PAIRING_MODE.md"),
		"## CORE Rule Extensions", "\n---")
	entry := preventionRulesParagraph(t, "PAIRING_MODE extensions", pairing, "**Rule 11 prevention answer")
	preventionRulesRequire(t, "PAIRING_MODE entry", entry, "Software Architecture Protocol", "waive", "Fixed-Twice-Design")

	subagent := preventionRulesBetween(t, "SUBAGENT_MODE read-write", preventionRulesRead(t, "contracts/SUBAGENT_MODE.md"),
		"### READ-WRITE Subagents", "\n---")
	bullet := preventionRulesLine(t, "SUBAGENT_MODE read-write", subagent, "prevention answer")
	preventionRulesRequireFold(t, "SUBAGENT_MODE bullet", bullet, "caller", "fixed-twice stop trigger")
}

func TestPreventionRulesDispositionsStand(t *testing.T) {
	t.Parallel()
	answer, trigger := preventionRulesMultiAgentRows(t)
	preventionRulesRequire(t, "MULTI_AGENT_MODE Rule 11 row", answer,
		"only the human", "`scheduled`", "`human`", "`removed`", "`accepted`")
	if !strings.Contains(answer, "Never `accepted`") {
		t.Errorf("MULTI_AGENT_MODE Rule 11 row: missing marker %q", "Never `accepted`")
	}

	// No CORE or annex site ties a disposition to the fixed-twice check.
	for name, text := range map[string]string{
		"CORE Rule 2 paragraph":   preventionRulesFixedTwiceParagraph(t),
		"CORE stop-trigger row":   preventionRulesStopTriggerRow(t),
		"MULTI_AGENT fixed-twice": trigger,
		"wake fixed-twice case":   preventionRulesWakeCase(t),
	} {
		preventionRulesForbid(t, name, text, "`removed`", "`scheduled`", "`accepted`")
	}
}

func TestPreventionRulesScopeBoundary(t *testing.T) {
	t.Parallel()
	scope := preventionRulesRule(t, "6", "7")
	line := preventionRulesParagraph(t, "CORE Rule 6", scope, "**Refactoring Discipline:**")
	preventionRulesRequireFold(t, "CORE Rule 6 Refactoring Discipline", line, "fixed-twice stop trigger", "removing the shape")
}

func TestPreventionRulesReferenceSurfaces(t *testing.T) {
	t.Parallel()
	failureMap := preventionRulesRead(t, "contracts/CONTRACT_FAILURE_MODE_MAP.md")
	process := preventionRulesBetween(t, "failure map", failureMap, "## Process & Recovery Failure Modes", "\n---")
	preventionRulesRequire(t, "failure map Process & Recovery", process, "| REC-7 |", "| REC-8 |")
	summary := preventionRulesBetween(t, "failure map", failureMap, "## Coverage Summary", "\n---")
	preventionRulesRequire(t, "failure map Coverage Summary", summary,
		"| Process & Recovery | 8 | 8 | 0 | 0 |", "| **Total** | **57** | **57** | **0** | **0** |")

	usage := preventionRulesRead(t, "support-docs/USAGE_PAIRING.md")
	done := preventionRulesBetween(t, "USAGE_PAIRING", usage, "### The agent validates before claiming done", "### How to tell")
	preventionRulesRequire(t, "USAGE_PAIRING done section", done, "what made the bug possible")
	drift := preventionRulesBetween(t, "USAGE_PAIRING", usage, "### The agent catches its own drift", "### The agent degrades")
	preventionRulesRequire(t, "USAGE_PAIRING drift section", drift, "fixed twice")
}

// fixedTwiceFixture is a temporary repository whose commits carry fixed
// dates, an explicit identity and a built environment (G1.6): nothing is
// inherited from the host but the executables' location, and diagnostics
// name git subcommands and git's own output, never an environment.
type fixedTwiceFixture struct {
	t       *testing.T
	dir     string
	home    string
	shell   string
	concept string // the document's concept query
	marker  string // CORE Rule 2's marker one-liner
}

func newFixedTwiceFixture(t *testing.T) *fixedTwiceFixture {
	t.Helper()
	shell, err := gitbash.Resolve()
	if err != nil {
		t.Fatalf("no shell to run the check: %v", err)
	}
	f := &fixedTwiceFixture{
		t: t, dir: t.TempDir(), home: t.TempDir(), shell: shell,
		concept: fixedTwiceConceptQuery(t),
		marker:  fixedTwiceMarkerOneLiner(t),
	}
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"config", "commit.gpgsign", "false"},
	} {
		f.git(nil, args...)
	}
	return f
}

// fixedTwiceConceptQuery extracts the one-line block under "### Concept query".
func fixedTwiceConceptQuery(t *testing.T) string {
	t.Helper()
	doc := preventionRulesRead(t, preventionRecordDoc)
	section := preventionRulesBetween(t, "record document", doc, "### Concept query", "")
	body := preventionRulesBetween(t, "record document", section, "```sh\n", "\n```")
	lines := strings.Split(strings.TrimPrefix(body, "```sh\n"), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("record document: concept query is %d lines, want one command", len(lines))
	}
	return lines[0]
}

// fixedTwiceMarkerOneLiner extracts the marker list command from CORE's Rule 2 check.
func fixedTwiceMarkerOneLiner(t *testing.T) string {
	t.Helper()
	var found []string
	for i, span := range strings.Split(preventionRulesFixedTwiceParagraph(t), "`") {
		if i%2 == 1 && strings.HasPrefix(span, "git log ") && strings.Contains(span, "Fixed-Twice-Design") {
			found = append(found, span)
		}
	}
	if len(found) != 1 {
		t.Fatalf("CORE Rule 2: %d code spans are the marker list command, want exactly one", len(found))
	}
	return found[0]
}

func (f *fixedTwiceFixture) env(extra ...string) []string {
	return append(recordfixture.Environ(f.home), extra...)
}

func (f *fixedTwiceFixture) git(stdin *strings.Reader, args ...string) string {
	f.t.Helper()
	return f.gitEnv(f.env(), stdin, args...)
}

func (f *fixedTwiceFixture) gitEnv(env []string, stdin *strings.Reader, args ...string) string {
	f.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", f.dir}, args...)...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		f.t.Fatalf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String())
}

// commit adds an empty commit on the given day (day 1 is 2026-03-01) whose
// final trailer block is trailers, and returns its full SHA.
func (f *fixedTwiceFixture) commit(day int, subject string, trailers ...string) string {
	f.t.Helper()
	stamp := fmt.Sprintf("%d +0000", time.Date(2026, 3, day, 12, 0, 0, 0, time.UTC).Unix())
	env := f.env(
		"GIT_AUTHOR_NAME=Fixture Author", "GIT_AUTHOR_EMAIL=author@example.invalid", "GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_NAME=Fixture Committer", "GIT_COMMITTER_EMAIL=committer@example.invalid", "GIT_COMMITTER_DATE="+stamp,
	)
	message := subject + "\n\nFixture commit.\n"
	if len(trailers) > 0 {
		message += "\n" + strings.Join(trailers, "\n") + "\n"
	}
	f.gitEnv(env, strings.NewReader(message), "commit", "--quiet", "--allow-empty", "--cleanup=verbatim", "-F", "-")
	return f.git(nil, "rev-parse", "HEAD")
}

// fix commits a `fix` change recording the given concepts, then any extra trailers.
func (f *fixedTwiceFixture) fix(day int, concepts []string, extra ...string) string {
	trailers := []string{"Prevention-Cause: the shape had no owner"}
	for _, concept := range concepts {
		trailers = append(trailers, "Prevention-Concept: "+concept)
	}
	trailers = append(trailers, "Prevention-Disposition: removed", "Prevention-Class: gap")
	return f.commit(day, "fix: repair "+strings.Join(concepts, " "), append(trailers, extra...)...)
}

func designMarker(concept, sha string) string { return "Fixed-Twice-Design: " + concept + " " + sha }

type fixedTwiceFix struct {
	sha  string
	date time.Time
}

// check runs the document's concept query for concept and CORE's marker
// one-liner in one shell call, as Rule 2 prescribes, and returns the listed
// fixes (newest first) and each marked commit's values.
func (f *fixedTwiceFixture) check(concept string) (fixes []fixedTwiceFix, markers map[string][]string) {
	f.t.Helper()
	const separator = "-----"
	script := f.concept + "\nprintf '%s\\n' '" + separator + "'\n" + f.marker + "\n"
	cmd := exec.Command(f.shell, "-c", script)
	cmd.Dir = f.dir
	cmd.Env = f.env("CONCEPT="+concept, fixedTwiceSince, fixedTwiceUntil)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		f.t.Fatalf("check for %q: %v: %s", concept, err, strings.TrimSpace(stderr.String()))
	}
	listing, markerList, ok := strings.Cut(stdout.String(), separator+"\n")
	if !ok {
		f.t.Fatalf("check for %q: no separator in output", concept)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(listing), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			f.t.Fatalf("check for %q: listing line has %d fields, want 7", concept, len(fields))
		}
		date, err := time.Parse("2006-01-02T15:04:05Z", fields[1])
		if err != nil {
			f.t.Fatalf("check for %q: listing date: %v", concept, err)
		}
		fixes = append(fixes, fixedTwiceFix{sha: fields[0], date: date})
	}
	markers = map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(markerList), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 || fields[1] == "" {
			f.t.Fatalf("check for %q: marker line has %d fields, want a SHA and its values", concept, len(fields))
		}
		markers[fields[0]] = strings.Split(fields[1], ",")
	}
	return fixes, markers
}

// held applies Rule 2's decision for one concept to the check's output: its
// two newest fixes A and B (B newer) are held when B is within 14 days after
// A, B is not marked "concept A", and no commit is marked "concept B".
func (f *fixedTwiceFixture) held(concept string) bool {
	f.t.Helper()
	fixes, markers := f.check(concept)
	if len(fixes) < 2 {
		return false
	}
	newer, older := fixes[0], fixes[1]
	marked := func(sha, value string) bool { return slices.Contains(markers[sha], value) }
	if newer.date.Sub(older.date) > 14*24*time.Hour || marked(newer.sha, concept+" "+older.sha) {
		return false
	}
	for sha := range markers {
		if marked(sha, concept+" "+newer.sha) {
			return false
		}
	}
	return true
}

func (f *fixedTwiceFixture) expectHeld(concept string, want bool) {
	f.t.Helper()
	if got := f.held(concept); got != want {
		f.t.Errorf("concept %q held = %v, want %v", concept, got, want)
	}
}

func (f *fixedTwiceFixture) expectListing(concept string, want ...string) {
	f.t.Helper()
	fixes, _ := f.check(concept)
	var got []string
	for _, fix := range fixes {
		got = append(got, fix.sha)
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		f.t.Errorf("listing for %q = %v, want %v", concept, got, want)
	}
}

func (f *fixedTwiceFixture) expectMarked(sha, value string) {
	f.t.Helper()
	_, markers := f.check("X")
	if !slices.Contains(markers[sha], value) {
		f.t.Errorf("marker list does not name commit %s with value %q", sha, value)
	}
}

func TestPreventionRulesFixedTwiceFixture(t *testing.T) {
	t.Parallel()

	t.Run("one concept through hold, release, marked fix and a new pair", func(t *testing.T) {
		t.Parallel()
		f := newFixedTwiceFixture(t)
		x := []string{"X"}

		a := f.fix(1, x)
		f.expectHeld("X", false) // one fix is no pair
		b := f.fix(5, x)
		f.commit(6, "chore: unrelated")
		f.expectListing("X", b, a)
		if _, markers := f.check("X"); len(markers) != 0 {
			t.Errorf("marker list before any design has %d entries, want none", len(markers))
		}
		f.expectHeld("X", true)

		m := f.commit(7, "feat: change released by the design", designMarker("X", b))
		f.expectMarked(m, "X "+b)
		f.expectHeld("X", false)

		c := f.fix(10, x, designMarker("X", b))
		f.expectListing("X", c, b, a)
		f.expectHeld("X", false) // C came from the design of the pair ending at B

		d := f.fix(12, x)
		f.expectListing("X", d, c, b, a)
		f.expectHeld("X", true) // (C, D): the shape recurred after the design

		f.fix(40, x)
		f.expectHeld("X", false) // 28 days after D
	})

	t.Run("a marker releases only the concept it names", func(t *testing.T) {
		t.Parallel()
		f := newFixedTwiceFixture(t)
		xy := []string{"X", "Y"}
		f.fix(1, xy)
		b := f.fix(5, xy)
		f.expectHeld("X", true)
		f.expectHeld("Y", true)

		f.commit(6, "feat: released on X only", designMarker("X", b))
		f.expectHeld("X", false)
		f.expectHeld("Y", true)
	})

	t.Run("one change carrying both markers releases both concepts", func(t *testing.T) {
		t.Parallel()
		f := newFixedTwiceFixture(t)
		xy := []string{"X", "Y"}
		f.fix(1, xy)
		b := f.fix(5, xy)
		f.commit(6, "feat: released on both", designMarker("X", b), designMarker("Y", b))
		f.expectHeld("X", false)
		f.expectHeld("Y", false)
	})

	t.Run("a marker naming another fix does not release the concept", func(t *testing.T) {
		t.Parallel()
		f := newFixedTwiceFixture(t)
		f.fix(1, []string{"X", "Y"})
		f.fix(4, []string{"Y"}) // Y's newer fix is b2, not b
		b := f.fix(5, []string{"X"})
		f.commit(6, "feat: marker names b for both", designMarker("X", b), designMarker("Y", b))
		f.expectHeld("X", false)
		f.expectHeld("Y", true)
	})
}
