package referencecontract

import (
	"reflect"
	"strings"
	"testing"
)

func TestSectionReferenceIDs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		want       []string
		present    bool
		errorText  string
	}{
		{name: "explicit", body: "**Direct references:** [\"a\", \"b\"]\n", want: []string{"a", "b"}, present: true},
		{name: "empty", body: "**Direct references:** []\n", want: []string{}, present: true},
		{name: "legacy", body: "Scope prose\n"},
		{name: "fenced", body: "```md\n**Direct references:** [\"a\"]\n```\n"},
		{name: "tilde fenced", body: "~~~md\n**Direct references:** [\"a\"]\n~~~\n"},
		{name: "indented", body: "    **Direct references:** [\"a\"]\n"},
		{name: "quoted", body: "> **Direct references:** [\"a\"]\n"},
		{name: "child declaration", body: "#### Subsection\n**Direct references:** [\"a\"]\n"},
		{name: "malformed", body: "**Direct references:** a\n", present: true, errorText: "JSON array"},
		{name: "null", body: "**Direct references:** null\n", present: true, errorText: "JSON array"},
		{name: "blank ID", body: "**Direct references:** [\"\"]\n", present: true, errorText: "non-empty"},
		{name: "repeated ID", body: "**Direct references:** [\"a\",\"a\"]\n", present: true, errorText: "duplicate reference ID"},
		{name: "two declarations", body: "**Direct references:** []\n**Direct references:** []\n", present: true, errorText: "duplicate Direct references"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ids, present, err := SectionReferenceIDs("## Decomposition\n### Scope 1\n"+tc.body+"### Scope 2\nOther\n", "Scope 1")
			if present != tc.present {
				t.Fatalf("presence = %v, want %v", present, tc.present)
			}
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("error = %v, want %q", err, tc.errorText)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(ids, tc.want) {
				t.Fatalf("IDs = %#v, error = %v, want %#v", ids, err, tc.want)
			}
		})
	}
}

func TestValidateSectionReferences(t *testing.T) {
	t.Parallel()
	contract := &Contract{DirectReferences: []DirectReference{{ID: "known"}}}
	for _, tc := range []struct{ content, errorText string }{
		{"# Document\n### Scope\n**Direct references:** [\"known\"]\n", ""},
		{"# Document\n### Scope\n**Direct references:** [\"unknown\"]\n", "undeclared"},
		{"# Document\n### Scope\n**Direct references:** wrong\n", "JSON array"},
		{"# Document\n### Scope\n**Direct references:** []\n### Scope\n", "ambiguous"},
	} {
		err := ValidateSectionReferences(tc.content, contract)
		if tc.errorText == "" && err != nil || tc.errorText != "" && (err == nil || !strings.Contains(err.Error(), tc.errorText)) {
			t.Errorf("content %q: error = %v, want %q", tc.content, err, tc.errorText)
		}
	}
}

func TestSectionAtLine(t *testing.T) {
	t.Parallel()
	markdown := "preamble\n# Doc\n## Outer\n### Inner\n```\n# Fake\n```\n## Next\nend\n"
	heading, first, last, err := SectionAtLine(markdown, 6)
	if err != nil || heading != "Inner" || first != 4 || last != 7 {
		t.Fatalf("section = %q %d-%d, error = %v", heading, first, last, err)
	}
	for _, line := range []int{0, 1, 10} {
		if _, _, _, err := SectionAtLine(markdown, line); err == nil {
			t.Errorf("line %d should not resolve", line)
		}
	}
	if _, _, _, err := SectionAtLine("# Duplicate\ntext\n# Duplicate\n", 2); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous citation error = %v", err)
	}
}
