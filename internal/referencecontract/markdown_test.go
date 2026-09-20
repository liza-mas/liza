package referencecontract

import (
	"errors"
	"strings"
	"testing"
)

func TestExtractSectionUsesExactFenceAwareATXSpans(t *testing.T) {
	markdown := strings.Join([]string{
		"# Document",
		"",
		"```markdown",
		"## Intent `exact` — α #2",
		"## Fenced terminator",
		"```",
		"",
		"    ## Intent `exact` — α #2",
		"\t## Intent `exact` — α #2",
		"",
		"  ## Intent `exact` — α #2 ###  ",
		"body",
		"Heading-like setext",
		"-------------------",
		"#### Nested decision",
		"nested body",
		"~~~",
		"## Fenced sibling",
		"~~~",
		"### Child after fence",
		"child body",
		"## Next",
		"outside",
	}, "\n") + "\n"

	got, err := ExtractSection(markdown, "Intent `exact` — α #2")
	if err != nil {
		t.Fatalf("ExtractSection() error = %v", err)
	}
	want := strings.Join([]string{
		"  ## Intent `exact` — α #2 ###  ",
		"body",
		"Heading-like setext",
		"-------------------",
		"#### Nested decision",
		"nested body",
		"~~~",
		"## Fenced sibling",
		"~~~",
		"### Child after fence",
		"child body",
	}, "\n") + "\n"
	if got != want {
		t.Errorf("ExtractSection() = %q, want %q", got, want)
	}
}

func TestExtractSectionFenceClosingRules(t *testing.T) {
	markdown := strings.Join([]string{
		"````",
		"## hidden by long opener",
		"```",
		"~~~",
		"```` trailing text",
		"## still hidden",
		"  `````  ",
		"## Visible",
		"visible body",
	}, "\n")

	got, err := ExtractSection(markdown, "Visible")
	if err != nil {
		t.Fatalf("ExtractSection() error = %v", err)
	}
	if got != "## Visible\nvisible body" {
		t.Errorf("ExtractSection() = %q", got)
	}
	for _, hidden := range []string{"hidden by long opener", "still hidden"} {
		_, err := ExtractSection(markdown, hidden)
		var matchErr *HeadingMatchError
		if !errors.As(err, &matchErr) || matchErr.Matches != 0 {
			t.Errorf("ExtractSection(%q) error = %v, want missing HeadingMatchError", hidden, err)
		}
	}
}

func TestExtractSectionRejectsMissingAndAmbiguousHeadings(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		heading  string
		matches  int
	}{
		{
			name:     "missing exact case",
			markdown: "## Exact Heading\nbody\n",
			heading:  "exact heading",
			matches:  0,
		},
		{
			name:     "ambiguous across levels",
			markdown: "## Same\none\n### Same\ntwo\n",
			heading:  "Same",
			matches:  2,
		},
		{
			name:     "ineligible forms do not match",
			markdown: "####### Too Many\n##No Space\n    ## Indented\n",
			heading:  "Indented",
			matches:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractSection(tt.markdown, tt.heading)
			var matchErr *HeadingMatchError
			if !errors.As(err, &matchErr) {
				t.Fatalf("ExtractSection() error = %v, want HeadingMatchError", err)
			}
			if matchErr.Matches != tt.matches {
				t.Errorf("HeadingMatchError.Matches = %d, want %d", matchErr.Matches, tt.matches)
			}
		})
	}
}

func TestExtractSectionPreservesCRLFAndEOF(t *testing.T) {
	markdown := "# Top\r\n\r\n### Selected\r\nbody\r\n#### Child\r\nchild"
	want := "### Selected\r\nbody\r\n#### Child\r\nchild"
	got, err := ExtractSection(markdown, "Selected")
	if err != nil {
		t.Fatalf("ExtractSection() error = %v", err)
	}
	if got != want {
		t.Errorf("ExtractSection() = %q, want exact source bytes %q", got, want)
	}
}

// A peer shares the assigned section's level and its enclosing heading. A
// section at the same level under a different heading is not a peer: that is
// what keeps a plan's analysis and design sections inlined while its other
// task sections elide.
func TestPeerSectionsSelectsSameLevelUnderSameParent(t *testing.T) {
	t.Parallel()

	const doc = "# Plan\n\n## Design\n\n### Parity\ndesign\n\n## Tasks\n\n### Task 1\none\n\n### Task 2\ntwo\n\n### Task 3\nthree\n\n## Out of Scope\nscope\n"

	peers := peerSections(doc, "Task 2")
	var got []string
	for _, peer := range peers {
		got = append(got, peer.heading)
		if strings.Contains(doc[peer.start:peer.end], "\n## ") {
			t.Errorf("peer %q span runs past its own section into a higher-level heading", peer.heading)
		}
	}
	if len(got) != 2 || got[0] != "Task 1" || got[1] != "Task 3" {
		t.Errorf("peers = %v, want [Task 1 Task 3]: Parity and Out of Scope sit under other headings", got)
	}
}

// Headings inside a fenced block are text, not structure. Narrowing on one
// would cut a section at a line the document does not divide.
func TestPeerSectionsIgnoresFencedHeadings(t *testing.T) {
	t.Parallel()

	const doc = "## Tasks\n\n### Task 1\n```md\n### Task 2\n```\nstill task one\n\n### Task 2\ntwo\n"

	peers := peerSections(doc, "Task 2")
	if len(peers) != 1 || peers[0].heading != "Task 1" {
		t.Fatalf("peers = %+v, want the single real Task 1 section", peers)
	}
	if !strings.Contains(doc[peers[0].start:peers[0].end], "still task one") {
		t.Error("peer span ended at a fenced heading instead of the next real one")
	}
}

// An ambiguous heading cannot identify one assigned section, and narrowing on
// a guess would drop context the task needs.
func TestPeerSectionsRefusesAmbiguousHeading(t *testing.T) {
	t.Parallel()

	const doc = "## A\n\n### Task 1\none\n\n## B\n\n### Task 1\nalso one\n\n### Task 2\ntwo\n"

	if peers := peerSections(doc, "Task 1"); peers != nil {
		t.Errorf("peers = %+v, want none for an ambiguous assigned heading", peers)
	}
}
