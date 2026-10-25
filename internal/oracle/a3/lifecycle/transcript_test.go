package lifecycle

import (
	"strings"
	"testing"
)

// R4: the Git-transcript normaliser. State text is built in the shapes the
// CLI's YAML encoder gives the scalar: double-quoted when Git's output holds a
// carriage return (progress output), a literal block otherwise.

const (
	gitOut243 = "Rebasing (1/1)\rAuto-merging README\nCONFLICT (content): Merge conflict in README\nerror: could not apply e5d6113... task edit\nhint: Resolve all conflicts manually, mark them as resolved with\nhint: \"git add/rm <conflicted_files>\", then run \"git rebase --continue\".\nhint: You can instead skip this commit: run \"git rebase --skip\".\nhint: To abort and get back to the state before \"git rebase\", run \"git rebase --abort\".\nCould not apply e5d6113... task edit"
	// committed243Excerpt is the excerpt scalar of the golden as committed
	// before this correction, from Git 2.43.
	committed243Excerpt = `"rebase conflict: Rebasing (1/1)\rAuto-merging README\nCONFLICT (content): Merge conflict in README\nerror: could not apply e5d6113... task edit\nhint: Resolve all conflicts manually, mark them as resolved with\nhint: \"git add/rm <conflicted_files>\", then run \"git rebase --continue\".\nhint: You can instead skip this commit: run \"git rebase --skip\".\nhint: To abort and get back to the state before \"git rebase\", run \"git rebase --abort\".\nCould not apply e5d6113... task edit"`

	evidenceHead = "command=git -C .worktrees/task-1 rebase <commit:integration> target_ref=integration target_sha=<commit:integration> exit_code=1 "
	excerptHead  = "rebase conflict: "
	evidenceTail = "stderr=" + excerptHead
)

type scalarShape struct {
	lead            string
	indent, content int
}

var (
	evidenceShape = scalarShape{lead: "- ", indent: 12, content: 14}
	excerptShape  = scalarShape{lead: "stdout_stderr_excerpt: ", indent: 12, content: 16}
)

var yamlEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

func (s scalarShape) quoted(value string) string {
	return strings.Repeat(" ", s.indent) + s.lead + `"` + yamlEscaper.Replace(value) + `"`
}

// render writes value as the CLI's encoder does: double-quoted with a
// carriage return, else a literal block, clipped when value ends in a newline.
func (s scalarShape) render(value string) string {
	if strings.Contains(value, "\r") {
		return s.quoted(value)
	}
	chomp := "-"
	if strings.HasSuffix(value, "\n") {
		chomp, value = "", strings.TrimSuffix(value, "\n")
	}
	lines := []string{strings.Repeat(" ", s.indent) + s.lead + "|" + chomp}
	for _, line := range strings.Split(value, "\n") {
		lines = append(lines, strings.Repeat(" ", s.content)+line)
	}
	return strings.Join(lines, "\n")
}

// transcriptState wraps the two scalars in neighbouring state lines.
func transcriptState(evidence, excerpt string) string {
	return "        evidence:\n" + evidence + "\n        validation:\n            - git status\n" +
		"            reason: unblock_rebase_conflict\n" + excerpt + "\n        lifecycle:\n"
}

func renderedState(out string) string {
	return transcriptState(evidenceShape.render(evidenceHead+evidenceTail+out), excerptShape.render(excerptHead+out))
}

func maskedState() string {
	return transcriptState(evidenceShape.quoted(evidenceHead+evidenceTail+gitTranscriptToken), excerptShape.quoted(excerptHead+gitTranscriptToken))
}

func TestLifecycleGitTranscriptRenderingMatchesCommitted(t *testing.T) {
	if excerptShape.quoted(excerptHead + gitOut243)[len(excerptShape.lead)+excerptShape.indent:] != committed243Excerpt {
		t.Fatal("test rendering of the 2.43 excerpt differs from the committed golden scalar")
	}
}

func TestLifecycleGitTranscriptMasksVersions(t *testing.T) {
	noHints := strings.Join([]string{"Rebasing (1/1)\rAuto-merging README", "CONFLICT (content): Merge conflict in README", "error: could not apply e5d6113... task edit", "hint: see git help rebase", "Could not apply e5d6113... task edit"}, "\n")
	variants := map[string]string{
		"committed 2.43":             gitOut243,
		"changed hint wording":       noHints,
		"no progress line":           strings.Replace(gitOut243, "Rebasing (1/1)\r", "", 1),
		"no progress, clipped final": strings.Replace(gitOut243, "Rebasing (1/1)\r", "", 1) + "\n",
		"extra final line":           gitOut243 + "\nCould not apply e5d6113... task edit",
		"missing final line":         strings.TrimSuffix(gitOut243, "\nCould not apply e5d6113... task edit"),
		"longer abbreviation":        strings.ReplaceAll(gitOut243, "e5d6113", "e5d6113c0ffee"),
	}
	want := maskedState()
	seen := map[string]string{}
	for name, out := range variants {
		raw := renderedState(out)
		if prior, dup := seen[raw]; dup {
			t.Errorf("variants %q and %q render identically, so one proves nothing", name, prior)
		}
		seen[raw] = name
		got, err := maskGitTranscript(raw)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if got != want {
			t.Errorf("%s: masked text differs from the token form", name)
		}
	}
	// Without the normaliser the variants differ: the golden would pin one Git.
	if renderedState(gitOut243) == renderedState(variants["no progress line"]) {
		t.Error("variants must differ before masking")
	}
}

func TestLifecycleGitTranscriptKeepsProductionText(t *testing.T) {
	for name, mutate := range map[string]func(prefix string) string{
		"command":    func(p string) string { return strings.Replace(p, "command=git", "command=gif", 1) },
		"target_ref": func(p string) string { return strings.Replace(p, "target_ref=integration", "target_ref=main", 1) },
		"target_sha": func(p string) string {
			return strings.Replace(p, "target_sha=<commit:integration>", "target_sha=<commit:task>", 1)
		},
		"exit_code": func(p string) string { return strings.Replace(p, "exit_code=1", "exit_code=2", 1) },
	} {
		changed := mutate(evidenceHead)
		if changed == evidenceHead {
			t.Fatalf("%s: mutation missed its anchor", name)
		}
		got, err := maskGitTranscript(transcriptState(evidenceShape.render(changed+evidenceTail+gitOut243), excerptShape.render(excerptHead+gitOut243)))
		if err == nil && got == maskedState() {
			t.Errorf("%s: change to production text went undetected", name)
		}
	}
	for name, raw := range map[string]string{
		"evidence prefix": transcriptState(evidenceShape.render(evidenceHead+"stderr=rebase conflicted: "+gitOut243), excerptShape.render(excerptHead+gitOut243)),
		"excerpt prefix":  transcriptState(evidenceShape.render(evidenceHead+evidenceTail+gitOut243), excerptShape.render("rebase conflic: "+gitOut243)),
	} {
		got, err := maskGitTranscript(raw)
		if err == nil && got == maskedState() {
			t.Errorf("%s: change to the production prefix went undetected", name)
		}
	}
}

func TestLifecycleGitTranscriptAssertsClassificationFacts(t *testing.T) {
	for name, tc := range map[string]struct{ out, fact string }{
		"CONFLICT and could not apply": {"Rebasing (1/1)\rAuto-merging README\nhint: nothing", "CONFLICT"},
		"README":                       {"Rebasing (1/1)\rCONFLICT (content): Merge conflict in other\nerror: could not apply e5d6113... task edit", "README"},
	} {
		_, err := maskGitTranscript(renderedState(tc.out))
		if err == nil || !strings.Contains(err.Error(), tc.fact) {
			t.Errorf("%s: missing fact not named in the failure", name)
		}
		if err != nil && strings.Contains(err.Error(), tc.out) {
			t.Errorf("%s: failure repeats Git output", name)
		}
	}
	// Either classification marker alone satisfies production.
	for _, out := range []string{"README CONFLICT only", "README error: could not apply only"} {
		if _, err := maskGitTranscript(renderedState(out)); err != nil {
			t.Errorf("classification marker alone refused: %v", err)
		}
	}
	if _, err := maskGitTranscript("agents:\n    coder-1:\n        role: coder\n"); err == nil {
		t.Error("an opted-in case without a transcript must fail, not pass unmasked")
	}
}

func TestLifecycleGitTranscriptOptIn(t *testing.T) {
	raw := renderedState(gitOut243)
	if got, err := (lifecycleCase{}).normaliseState(raw); err != nil || got != raw {
		t.Fatal("a case without the opt-in must keep its raw text")
	}
	if got, err := (lifecycleCase{gitTranscript: true}).normaliseState(raw); err != nil || got != maskedState() {
		t.Fatal("an opted-in case must mask the transcript")
	}
}
