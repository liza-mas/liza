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
