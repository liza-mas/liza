package embedded

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// altitudeRuleText is the frozen body of "## Altitude rule" (FR5).
const altitudeRuleText = "An invariant states a property of the system and what it protects against, at the level of a " +
	"principle. The bug-specific detail (the case, the input, the file) lives in its enforcement: a " +
	"type, a lint, a test or a guard. Every invariant records how it is enforced; one with no " +
	"enforcement yet says `unenforced` in its Enforced cell and stays listed, so the gap stays visible."

const altitudeHeading = "## Altitude rule"

// markdownSectionBody returns the bytes after the exact heading line up to the
// first following line that starts with '#' or equals '---', with surrounding
// whitespace trimmed. The bool is false when the heading line is absent.
func markdownSectionBody(doc []byte, heading string) ([]byte, bool) {
	lines := strings.Split(string(doc), "\n")
	for i, line := range lines {
		if strings.TrimRight(line, " \t\r") != heading {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			if strings.HasPrefix(next, "#") || strings.TrimRight(next, " \t\r") == "---" {
				break
			}
			body = append(body, next)
		}
		return bytes.TrimSpace([]byte(strings.Join(body, "\n"))), true
	}
	return nil, false
}

func readRepoFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(findRepoRoot(t), name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return raw
}

func TestAltitudeRule_InvariantsPreamble(t *testing.T) {
	doc := readRepoFile(t, "INVARIANTS.md")
	text := string(doc)

	if n := strings.Count(text, "\n"+altitudeHeading+"\n"); n != 1 {
		t.Fatalf("INVARIANTS.md must hold exactly one %q section, found %d", altitudeHeading, n)
	}

	firstSection := strings.Index(text, "\n## 1.")
	if firstSection < 0 {
		t.Fatal("INVARIANTS.md has no '## 1.' section")
	}
	if at := strings.Index(text, "\n"+altitudeHeading+"\n"); at > firstSection {
		t.Fatalf("%q must sit in the preamble, before '## 1.'", altitudeHeading)
	}

	body, ok := markdownSectionBody(doc, altitudeHeading)
	if !ok {
		t.Fatalf("section %q not found", altitudeHeading)
	}
	got := strings.Join(strings.Fields(string(body)), " ")
	want := strings.Join(strings.Fields(altitudeRuleText), " ")
	if got != want {
		t.Fatalf("altitude rule body differs from the frozen text\n got: %q\nwant: %q", got, want)
	}

	var legend string
	for line := range strings.SplitSeq(text[:firstSection], "\n") {
		if strings.HasPrefix(line, "**Enforcement legend:**") {
			legend = line
			break
		}
	}
	if legend == "" {
		t.Fatal("preamble has no '**Enforcement legend:**' line")
	}
	if !strings.Contains(legend, "`unenforced`") {
		t.Fatalf("enforcement legend must name `unenforced`: %q", legend)
	}
}

func TestAltitudeRule_GuardrailsPointer(t *testing.T) {
	doc := readRepoFile(t, "GUARDRAILS.md")
	body, ok := markdownSectionBody(doc, "### G1.2: Invariant compliance")
	if !ok {
		t.Fatal("GUARDRAILS.md has no '### G1.2: Invariant compliance' section")
	}
	const link = "[Altitude rule](INVARIANTS.md#altitude-rule)"
	if !strings.Contains(string(body), link) {
		t.Fatalf("G1.2 must link %s", link)
	}
}
