package supplement

import (
	"strings"
	"testing"
)

// The state text is built in the two shapes the blackboard writer gives the
// scalar: double-quoted when Git's output holds a carriage return (progress
// output), a literal block otherwise.

const (
	transcriptHead = "base_commit <commit:orphan> is no longer on integration branch integration and the task was not healed (rebase onto integration conflicts: "
	transcriptTail = "); repair: git -C .worktrees/task-1 rebase --onto integration <commit:orphan>, then run liza update-review-commit task-1"

	gitTranscriptSample = "Rebasing (1/1)\rAuto-merging feature.go\nCONFLICT (add/add): Merge conflict in feature.go\nerror: could not apply 1b2c3d4... candidate\nhint: Resolve all conflicts manually, mark them as resolved with\nhint: \"git add/rm <conflicted_files>\", then run \"git rebase --continue\".\nCould not apply 1b2c3d4... candidate"
)

var transcriptSpec = gitTranscript{Path: "feature.go", Copies: 2}

var yamlEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

// renderDetail writes the diagnostic as the blackboard writer does.
func renderDetail(indent int, key, git string) string {
	value := transcriptHead + transcriptOpen + git + transcriptTail
	pad := strings.Repeat(" ", indent)
	if strings.Contains(value, "\r") {
		return pad + key + `: "` + yamlEscaper.Replace(value) + `"`
	}
	lines := []string{pad + key + ": |-"}
	for _, line := range strings.Split(value, "\n") {
		lines = append(lines, pad+"    "+line)
	}
	return strings.Join(lines, "\n")
}

func transcriptState(first, second string) string {
	return "      integration_failure:\n" + renderDetail(8, "detail", first) + "\n        operation: review-assignment\n" +
		"      history:\n        - event: integration_failed\n" + renderDetail(12, "detail", second) + "\n          reason: review boundary mismatch\n"
}

func maskedTranscriptState() string {
	value := `"` + transcriptHead + transcriptOpen + gitTranscriptToken + transcriptTail + `"`
	return "      integration_failure:\n        detail: " + value + "\n        operation: review-assignment\n" +
		"      history:\n        - event: integration_failed\n            detail: " + value + "\n          reason: review boundary mismatch\n"
}

func TestGitTranscriptMasksVersions(t *testing.T) {
	variants := map[string]string{
		"sample":               gitTranscriptSample,
		"changed hint wording": "Rebasing (1/1)\rAuto-merging feature.go\nCONFLICT (add/add): Merge conflict in feature.go\nerror: could not apply 1b2c3d4... candidate\nhint: see git help rebase\nCould not apply 1b2c3d4... candidate",
		"no progress line":     strings.Replace(gitTranscriptSample, "Rebasing (1/1)\r", "", 1),
		"extra final line":     gitTranscriptSample + "\nCould not apply 1b2c3d4... candidate",
		"missing final line":   strings.TrimSuffix(gitTranscriptSample, "\nCould not apply 1b2c3d4... candidate"),
		"longer abbreviation":  strings.ReplaceAll(gitTranscriptSample, "1b2c3d4", "1b2c3d4c0ffee"),
	}
	seen := map[string]string{}
	for name, git := range variants {
		raw := transcriptState(git, git)
		if prior, dup := seen[raw]; dup {
			t.Errorf("variants %q and %q render identically, so one proves nothing", name, prior)
		}
		seen[raw] = name
		got, err := maskGitTranscript(raw, transcriptSpec)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if got != maskedTranscriptState() {
			t.Errorf("%s: masked text differs from the token form", name)
		}
	}
}

func TestGitTranscriptKeepsProductionText(t *testing.T) {
	good := transcriptState(gitTranscriptSample, gitTranscriptSample)
	for name, mutate := range map[string]func(string) string{
		"reason prefix": func(s string) string {
			return strings.Replace(s, "rebase onto integration conflicts", "rebase onto main conflicts", 1)
		},
		"repair instruction": func(s string) string { return strings.Replace(s, "--onto integration", "--onto main", 1) },
		"orphan base": func(s string) string {
			return strings.Replace(s, "no longer on integration", "still on integration", 1)
		},
		"second copy only": func(s string) string {
			at := strings.LastIndex(s, "update-review-commit task-1")
			return s[:at] + "update-review-commit task-2" + s[at+len("update-review-commit task-1"):]
		},
	} {
		changed := mutate(good)
		if changed == good {
			t.Fatalf("%s: mutation missed its anchor", name)
		}
		if got, err := maskGitTranscript(changed, transcriptSpec); err == nil && got == maskedTranscriptState() {
			t.Errorf("%s: a change to production text went undetected", name)
		}
	}
}

func TestGitTranscriptAssertsFacts(t *testing.T) {
	for name, tc := range map[string]struct {
		text string
		fact string
	}{
		"classification marker": {transcriptState("Rebasing (1/1)\rAuto-merging feature.go", "Rebasing (1/1)\rAuto-merging feature.go"), "CONFLICT"},
		"conflicted path":       {transcriptState(strings.ReplaceAll(gitTranscriptSample, "feature.go", "other.go"), gitTranscriptSample), "feature.go"},
		"copy count":            {"      integration_failure:\n" + renderDetail(8, "detail", gitTranscriptSample) + "\n", "want 2"},
	} {
		_, err := maskGitTranscript(tc.text, transcriptSpec)
		if err == nil || !strings.Contains(err.Error(), tc.fact) {
			t.Errorf("%s: error %v does not name %q", name, err, tc.fact)
		}
		if err != nil && strings.Contains(err.Error(), "Auto-merging") {
			t.Errorf("%s: failure repeats Git output", name)
		}
	}
}
