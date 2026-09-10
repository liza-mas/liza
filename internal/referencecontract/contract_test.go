package referencecontract

import (
	"strings"
	"testing"
)

const (
	defaultOID  = "1111111111111111111111111111111111111111"
	overrideOID = "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
)

func TestParseStrictContract(t *testing.T) {
	markdown := "# Plan\n\n" + strictDocument(
		"- \"goal\\\"α\": \"specs/goals/goal.md#Intent `exact` — α #2\"\n"+
			"- \"decision\": \"specs/architecture/ADR/0001-choice.md#Decision\" @ \""+overrideOID+"\"",
		"- \"OBL-1\" -> \"goal\\\"α\", \"decision\"",
	) + "\n\n## Local Decisions\nowned here\n"

	contract, err := Parse(markdown)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if contract == nil {
		t.Fatal("Parse() contract = nil, want strict contract")
	}
	if contract.SourceRevision != defaultOID {
		t.Errorf("SourceRevision = %q, want %q", contract.SourceRevision, defaultOID)
	}
	if len(contract.DirectReferences) != 2 {
		t.Fatalf("len(DirectReferences) = %d, want 2", len(contract.DirectReferences))
	}
	first := contract.DirectReferences[0]
	if first.ID != "goal\"α" || first.Path != "specs/goals/goal.md" || first.Heading != "Intent `exact` — α #2" || first.Revision != "" {
		t.Errorf("first DirectReference = %#v", first)
	}
	if got := first.EffectiveRevision(contract.SourceRevision); got != defaultOID {
		t.Errorf("default EffectiveRevision() = %q, want %q", got, defaultOID)
	}
	second := contract.DirectReferences[1]
	if got := second.EffectiveRevision(contract.SourceRevision); got != overrideOID {
		t.Errorf("override EffectiveRevision() = %q, want %q", got, overrideOID)
	}
	if len(contract.ObligationCoverage) != 1 {
		t.Fatalf("len(ObligationCoverage) = %d, want 1", len(contract.ObligationCoverage))
	}
	coverage := contract.ObligationCoverage[0]
	if coverage.ID != "OBL-1" || strings.Join(coverage.ReferenceIDs, ",") != "goal\"α,decision" {
		t.Errorf("ObligationCoverage = %#v", coverage)
	}
}

func TestParseLegacyMarkerDetection(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		strict   bool
	}{
		{name: "marker free", markdown: "# Legacy\nRead specs/goal.md\n"},
		{name: "wrong level", markdown: "### Source References\n"},
		{name: "missing marker space", markdown: "##Source References\n"},
		{name: "seven hashes", markdown: "####### Source References\n"},
		{name: "indented code", markdown: "    ## Source References\n"},
		{name: "leading tab", markdown: "\t## Source References\n"},
		{name: "blockquote marker", markdown: "> ## Source References\n"},
		{name: "backtick fenced only", markdown: "```markdown\n## Source References\n```\n"},
		{name: "tilde fenced only", markdown: "~~~\n## Source References\n~~~~\n"},
		{
			name: "fenced example plus real marker",
			markdown: "```\n## Source References\n```\n\n" + strictDocument(
				"- \"goal\": \"specs/goal.md#Goal\"",
				"- \"OBL\" -> \"goal\"",
			),
			strict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract, err := Parse(tt.markdown)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if (contract != nil) != tt.strict {
				t.Fatalf("Parse() strict = %v, want %v", contract != nil, tt.strict)
			}
		})
	}
}

func TestParseRejectsMalformedStrictContract(t *testing.T) {
	validDirect := "- \"goal\": \"specs/goal.md#Goal\""
	validCoverage := "- \"OBL\" -> \"goal\""
	tests := []struct {
		name     string
		markdown string
		want     string
	}{
		{
			name:     "duplicate marker",
			markdown: strictDocument(validDirect, validCoverage) + "\n## Source References\n",
			want:     "expected exactly one eligible level-2 marker",
		},
		{
			name: "missing source revision",
			markdown: "## Source References\n\n### Direct References\n" + validDirect +
				"\n\n### Obligation Coverage\n" + validCoverage + "\n",
			want: "expected Source revision declaration",
		},
		{
			name: "uppercase source revision",
			markdown: strings.Replace(strictDocument(validDirect, validCoverage), defaultOID,
				"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", 1),
			want: "lowercase hexadecimal",
		},
		{
			name: "wrong subsection order",
			markdown: "## Source References\nSource revision: \"" + defaultOID +
				"\"\n\n### Obligation Coverage\n" + validCoverage + "\n",
			want: "expected level-3 Direct References heading",
		},
		{
			name:     "empty direct references",
			markdown: strictDocument("", validCoverage),
			want:     "Direct References list must not be empty",
		},
		{
			name:     "empty obligation coverage",
			markdown: strictDocument(validDirect, ""),
			want:     "Obligation Coverage list must not be empty",
		},
		{
			name: "duplicate reference ID after JSON decoding",
			markdown: strictDocument(validDirect+"\n- \"go\\u0061l\": \"specs/other.md#Other\"",
				validCoverage),
			want: "duplicate reference ID",
		},
		{
			name: "duplicate obligation ID",
			markdown: strictDocument(validDirect,
				validCoverage+"\n- \"OBL\" -> \"goal\""),
			want: "duplicate obligation ID",
		},
		{
			name:     "undeclared covered reference",
			markdown: strictDocument(validDirect, "- \"OBL\" -> \"missing\""),
			want:     "undeclared reference ID",
		},
		{
			name:     "empty reference ID",
			markdown: strictDocument("- \"\": \"specs/goal.md#Goal\"", validCoverage),
			want:     "reference ID must be a non-empty single-line string",
		},
		{
			name:     "newline in obligation ID",
			markdown: strictDocument(validDirect, "- \"OBL\\n2\" -> \"goal\""),
			want:     "obligation ID must be a non-empty single-line string",
		},
		{
			name:     "malformed JSON string",
			markdown: strictDocument("- \"goal\\x\": \"specs/goal.md#Goal\"", validCoverage),
			want:     "invalid reference ID",
		},
		{
			name:     "trailing reference content",
			markdown: strictDocument(validDirect+" trailing", validCoverage),
			want:     "unexpected content after reference target",
		},
		{
			name:     "bad override",
			markdown: strictDocument(validDirect+" @ \"HEAD\"", validCoverage),
			want:     "reference revision override must be",
		},
		{
			name:     "missing coverage separator space",
			markdown: strictDocument(validDirect, "- \"OBL\" -> \"goal\",\"goal\""),
			want:     "expected ', '",
		},
		{
			name:     "unexpected nested heading",
			markdown: strictDocument(validDirect, validCoverage+"\n#### Notes"),
			want:     "unexpected heading",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract, err := Parse(tt.markdown)
			if err == nil {
				t.Fatalf("Parse() = %#v, nil; want error containing %q", contract, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %q, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseRejectsUnsafeReferencePaths(t *testing.T) {
	paths := []string{
		"/specs/goal.md",
		"C:/specs/goal.md",
		"./specs/goal.md",
		"specs/../goal.md",
		"specs//goal.md",
		"specs\\goal.md",
		"specs/goal.txt",
		"specs/goal\\u0000.md",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			direct := "- \"goal\": \"" + path + "#Goal\""
			contract, err := Parse(strictDocument(direct, "- \"OBL\" -> \"goal\""))
			if err == nil {
				t.Fatalf("Parse() = %#v, nil; want unsafe path error", contract)
			}
		})
	}
}

func strictDocument(directEntries, coverageEntries string) string {
	return "## Source References\n" +
		"Source revision: \"" + defaultOID + "\"\n\n" +
		"### Direct References\n" + directEntries + "\n\n" +
		"### Obligation Coverage\n" + coverageEntries + "\n"
}
