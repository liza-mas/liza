package historyfixture

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The full run's committed evidence: the recorded report of the skill's full
// mode over the fixture history, reviewed by the code reviewer. The test pins
// that the record belongs to this fixture and carries the four findings.
const fullRunRecordPath = "testdata/recorded-runs/full-run.md"

var (
	fullRunHeadsLine = regexp.MustCompile(`(?m)^- Fixture heads: upstream ([0-9a-f]{40}) downstream ([0-9a-f]{40})$`)
	fullRunWordToken = regexp.MustCompile(`\w+`)
	fullRunHexToken  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	fullRunHeading   = regexp.MustCompile(`^(#+) (.*)$`)
	fullRunYoungRate = regexp.MustCompile(`(^|[^0-9A-Za-z./-])5/7([^0-9A-Za-z./-]|$)`)
)

// fullRunNames reports whether text names the commit: its full SHA, or a
// prefix of it of at least 7 hex characters, appears as a whole token. A token
// is a maximal run of word characters, so an identifier embedding a prefix
// (x4cd9e3a) or a hex string longer than a SHA is not a name.
func fullRunNames(text, sha string) bool {
	for _, tok := range fullRunWordToken.FindAllString(text, -1) {
		if fullRunHexToken.MatchString(tok) && strings.HasPrefix(sha, tok) {
			return true
		}
	}
	return false
}

func TestFullRunNames(t *testing.T) {
	const sha = "4cd9e3a8a0497f4ac38ffc6b024589f578938697"
	for _, tc := range []struct {
		name, text string
		want       bool
	}{
		{"full SHA", "see " + sha + " here", true},
		{"seven-character prefix in code spans", "`" + sha[:7] + "` repairs", true},
		{"prefix before punctuation", sha[:12] + ", then", true},
		{"six characters", sha[:6], false},
		{"prefix embedded in an identifier", "x" + sha[:7], false},
		{"prefix followed by word characters", sha[:7] + "z", false},
		{"prefix followed by an underscore tail", sha[:7] + "_x", false},
		{"overlong hex token", sha + "0", false},
		{"another commit", "780d480f85010829a9fd57fff01e4b5311c22adb", false},
	} {
		if got := fullRunNames(tc.text, sha); got != tc.want {
			t.Errorf("%s: fullRunNames = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fullRunSection returns the lines under the first heading whose text starts
// with name, up to the next heading of the same or a higher level.
func fullRunSection(t *testing.T, doc, name string) []string {
	t.Helper()
	var out []string
	level := 0
	for line := range strings.SplitSeq(doc, "\n") {
		m := fullRunHeading.FindStringSubmatch(line)
		if level == 0 {
			if m != nil && strings.HasPrefix(m[2], name) {
				level = len(m[1])
			}
			continue
		}
		if m != nil && len(m[1]) <= level {
			break
		}
		out = append(out, line)
	}
	if level == 0 {
		t.Fatalf("no heading starts with %q", name)
	}
	return out
}

func TestFullRunRecord(t *testing.T) {
	b, err := os.ReadFile(fullRunRecordPath)
	if err != nil {
		t.Fatalf("read %s: %v", fullRunRecordPath, err)
	}
	doc := string(b)

	h, err := Build(filepath.Join(t.TempDir(), "fx"))
	if err != nil {
		t.Fatalf("build the fixture history: %v", err)
	}

	m := fullRunHeadsLine.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("record has no `- Fixture heads: upstream <sha> downstream <sha>` line")
	}
	if m[1] != h.Upstream.Head {
		t.Errorf("recorded upstream head %s differs from a fresh build's %s", m[1], h.Upstream.Head)
	}
	if m[2] != h.Downstream.Head {
		t.Errorf("recorded downstream head %s differs from a fresh build's %s", m[2], h.Downstream.Head)
	}
	if strings.Contains(doc, "/tmp/") {
		t.Error("record names a host temporary path; absolute paths must read <fixture>")
	}

	// The four protocol headings, in order.
	pos := -1
	for _, heading := range []string{"## Header", "## Report", "## Evidence map", "## Deviations"} {
		i := strings.Index(doc, "\n"+heading+"\n")
		if i <= pos {
			t.Errorf("heading %q missing or out of order", heading)
		}
		pos = i
	}

	// The header names the classifier subagents.
	header := strings.Join(fullRunSection(t, doc, "Header"), "\n")
	if !strings.Contains(header, "Classifier subagents") {
		t.Error("Header does not name the classifier subagents")
	}

	sha := func(id string) string {
		c, ok := h.Case(id)
		if !ok {
			t.Fatalf("case %s is not in the fixture history", id)
		}
		return c.SHA
	}
	report := fullRunSection(t, doc, "Report")

	hasLine := func(pred func(line string) bool) bool { return slices.ContainsFunc(report, pred) }
	if !hasLine(func(l string) bool { return fullRunNames(l, sha("u04")) && fullRunNames(l, sha("u03")) }) {
		t.Error("Report has no line naming u04 with u03 (the chain)")
	}
	if !hasLine(func(l string) bool { return fullRunNames(l, sha("u03")) && fullRunNames(l, sha("d02")) }) {
		t.Error("Report has no line naming u03 with d02 (one fix)")
	}
	if !hasLine(func(l string) bool { return fullRunYoungRate.MatchString(l) }) {
		t.Error("Report does not state the young-code share 5/7")
	}
	if !hasLine(func(l string) bool { return fullRunNames(l, sha("u07")) && strings.Contains(l, "misleading") }) {
		t.Error("Report has no line naming u07 as misleading")
	}
}
