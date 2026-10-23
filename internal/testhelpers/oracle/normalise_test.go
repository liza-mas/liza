package oracle

import (
	"strings"
	"testing"
)

func TestNormaliseTimestamps(t *testing.T) {
	n := NewNormaliser()
	for _, in := range []string{
		"2026-10-23T18:40:00Z",
		"2026-10-23T18:40:00.123456789Z",
		"2026-10-23T18:40:00+02:00",
		"2026-10-23T18:40:00-0500",
		"2026-10-23 18:40:00",
		"2026-10-23 18:40:00.5 +0200",
	} {
		if got := n.Text("at " + in + " done"); got != "at <TIME> done" {
			t.Errorf("timestamp form %q normalised to %q", in, got)
		}
	}
	if got := n.Text("2026-10-23 and 18:40:00"); got != "2026-10-23 and 18:40:00" {
		t.Errorf("a date and a time apart must stay, got %q", got)
	}
}

func TestNormaliseDirsLongestFirstAndNested(t *testing.T) {
	n := &Normaliser{}
	n.Dir("/work/run1", TokenTmp)
	n.Dir("/work/run1/project", TokenProject)
	n.Dir("/work/run1/home", TokenHome)
	in := "cwd=/work/run1/project/sub file=/work/run1/home/.cfg tmp=/work/run1/x"
	want := "cwd=<PROJECT>/sub file=<HOME>/.cfg tmp=<TMP>/x"
	if got := n.Text(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormaliseDirWholePathOnly(t *testing.T) {
	n := &Normaliser{}
	n.Dir("/tmp", TokenTmp)
	for in, want := range map[string]string{
		"/tmp/a":      "<TMP>/a",
		"in /tmp.":    "in <TMP>.",
		"/tmpfoo/a":   "/tmpfoo/a",
		"/var/tmp/a":  "/var/tmp/a",
		"x/tmp":       "x/tmp",
		"file:///tmp": "file://<TMP>",
	} {
		if got := n.Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplaceDirRewritesWindowsSeparatorsInTail(t *testing.T) {
	got := replaceDir(`C:\Users\x\sub\f.txt and C:\Users\x`, `C:\Users\x`, TokenHome)
	if want := "<HOME>/sub/f.txt and <HOME>"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNormaliseIdentifiersAndLiterals(t *testing.T) {
	n := &Normaliser{}
	n.Literal("agent-7", "<AGENT>")
	n.Literal("agent-77", "<AGENT>")
	sha1 := strings.Repeat("ab12", 10)
	sha256 := strings.Repeat("0f", 32)
	in := "sha " + sha1 + " digest " + sha256 + " id 123e4567-e89b-12d3-a456-426614174000 by agent-77 and agent-7 short abcdef12"
	want := "sha <ID> digest <ID> id <ID> by <AGENT> and <AGENT> short abcdef12"
	if got := n.Text(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSortLines(t *testing.T) {
	if got := SortLines("b\na\nc\n"); got != "a\nb\nc\n" {
		t.Errorf("got %q", got)
	}
	if got := SortLines("b\na"); got != "a\nb" {
		t.Errorf("no trailing newline must stay absent, got %q", got)
	}
}

func TestNormaliseIsIdempotent(t *testing.T) {
	n := NewNormaliser()
	n.Dir("/work/run1/project", TokenProject)
	n.Literal("agent-7", "<AGENT>")
	in := "2026-10-23T18:40:00+02:00 /work/run1/project/a " + strings.Repeat("ab", 20) + " agent-7 " + n.Text("")
	once := n.Text(in)
	if twice := n.Text(once); twice != once {
		t.Errorf("not idempotent: %q then %q", once, twice)
	}
}
