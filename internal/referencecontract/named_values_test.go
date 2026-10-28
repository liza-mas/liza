package referencecontract

import (
	"strings"
	"testing"
)

// Edges are literals, independent of the constants that carry them.
func TestAcceptanceJSONNestingEdge(t *testing.T) {
	nested := func(depth int) string {
		return `{"x":` + strings.Repeat("[", depth) + strings.Repeat("]", depth) + `}`
	}
	// Sixteen nested arrays pass the nesting check and fail later on the
	// unknown field; seventeen fail the nesting check itself.
	err := decodeAcceptanceJSON(nested(16), &AcceptanceContract{})
	if err == nil || strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("16 levels: err = %v, want an unknown-field error", err)
	}
	err = decodeAcceptanceJSON(nested(17), &AcceptanceContract{})
	if err == nil || !strings.Contains(err.Error(), "JSON nesting exceeds acceptance schema limit") {
		t.Fatalf("17 levels: err = %v, want the nesting limit error", err)
	}
}

func TestMarkdownIndentAndHeadingLevelEdges(t *testing.T) {
	for _, tc := range []struct {
		line      string
		wantLevel int
		wantOK    bool
	}{
		{"   ### Heading", 3, true},
		{"    ### Heading", 0, false},
		{"###### Heading", 6, true},
		{"####### Heading", 0, false},
	} {
		level, _, ok := parseATXHeading(tc.line)
		if level != tc.wantLevel || ok != tc.wantOK {
			t.Errorf("parseATXHeading(%q) = level %d ok %v, want level %d ok %v", tc.line, level, ok, tc.wantLevel, tc.wantOK)
		}
	}
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"```", true},
		{"~~~", true},
		{"``", false},
		{"   ```", true},
		{"    ```", false},
	} {
		if _, got := openingFence(tc.line); got != tc.want {
			t.Errorf("openingFence(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

func TestOIDAndDriveRootLengths(t *testing.T) {
	if !validOID(strings.Repeat("a", 40)) || validOID(strings.Repeat("a", 39)) || validOID(strings.Repeat("a", 41)) {
		t.Fatal("validOID length edges are not 40")
	}
	if err := validateMarkdownPath("C:/x.md"); err == nil || !strings.Contains(err.Error(), "repository-relative") {
		t.Fatalf("drive-rooted path: err = %v, want repository-relative refusal", err)
	}
	if err := validateMarkdownPath("C:x.md"); err != nil {
		t.Fatalf("relative path with colon refused: %v", err)
	}
}
