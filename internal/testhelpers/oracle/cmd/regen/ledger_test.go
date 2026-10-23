package main

import (
	"strings"
	"testing"
)

const sampleGolden = "internal/oracle/demo/testdata/golden/human/value.txt"

func TestLedger(t *testing.T) {
	sha := strings.Repeat("a", 40)
	valid := "start " + sha + "\nfix " + sha + "\nchanged " + sampleGolden + " C1\nchanged " + sampleGolden + " determinism\n"
	l, err := parseLedger([]byte(valid))
	if err != nil || l.start != sha || len(l.fixes) != 1 || len(l.changes) != 2 {
		t.Fatalf("valid ledger not parsed: %v", err)
	}
	for _, tt := range []struct{ name, text, line string }{
		{"missing start", "fix " + sha + "\n", "line 1"},
		{"empty", "", "line 1"},
		{"second start", "start " + sha + "\nstart " + sha + "\n", "line 2"},
		{"nonhex", "start " + strings.Repeat("z", 40) + "\n", "line 1"},
		{"sha reason", "start " + sha + "\nchanged " + sampleGolden + " " + sha + "\n", "line 2"},
		{"fourth field", "start " + sha + "\nchanged " + sampleGolden + " C1 " + sha + "\n", "line 2"},
		{"unknown reason", "start " + sha + "\nchanged " + sampleGolden + " C7\n", "line 2"},
		{"unknown kind", "start " + sha + "\nchanged internal/oracle/demo/testdata/golden/yaml/x C1\n", "line 2"},
		{"traversal", "start " + sha + "\nchanged internal/oracle/demo/../testdata/golden/human/x C1\n", "line 2"},
		{"blank", "start " + sha + "\n\n", "line 2"},
		{"spaces", "start  " + sha + "\n", "line 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseLedger([]byte(tt.text))
			if err == nil || !strings.Contains(err.Error(), tt.line) {
				t.Fatalf("want ledger error naming %s, got %v", tt.line, err)
			}
		})
	}
}
