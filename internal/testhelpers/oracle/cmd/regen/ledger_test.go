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
		{"fourth field", "start " + sha + "\nchanged " + sampleGolden + " C1 abc\n", "line 2"},
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

func TestLedgerRecordPaths(t *testing.T) {
	sha := strings.Repeat("a", 40)
	record := func(name string) string { return "start " + sha + "\nchanged " + name + " C2\n" }
	for _, name := range []string{
		sampleGolden,
		"internal/oracle/demo/driver_test.go",
		"internal/oracle/demo/testdata/fixtures/case.json",
		"internal/oracle/a3/control/adapter.go",
	} {
		if _, err := parseLedger([]byte(record(name))); err != nil {
			t.Errorf("record path %s rejected: %v", name, err)
		}
	}
	for _, tt := range []struct{ name, path string }{
		{"unknown golden kind", "internal/oracle/demo/testdata/golden/yaml/x"},
		{"golden directory", "internal/oracle/demo/testdata/golden"},
		{"traversal", "internal/oracle/demo/../other/x.go"},
		{"provenance", "internal/oracle/PROVENANCE"},
		{"outside a package", "internal/oracle/helper.go"},
		{"outside oracle", "internal/other/demo/x.go"},
		{"whitespace", "internal/oracle/demo/a b.go"},
		{"trailing slash", "internal/oracle/demo/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseLedger([]byte(record(tt.path))); err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("want ledger error naming line 2, got %v", err)
			}
		})
	}
}

func TestPackageDir(t *testing.T) {
	for name, want := range map[string]string{
		"internal/oracle/a3/control/testdata/golden/human/x.txt": "internal/oracle/a3/control",
		"internal/oracle/a3/testdata/fixtures/y.json":            "internal/oracle/a3",
		"internal/oracle/a3/control/driver_test.go":              "internal/oracle/a3/control",
		"internal/oracle/a3/adapter.go":                          "internal/oracle/a3",
	} {
		if got := packageDir(name); got != want {
			t.Errorf("packageDir(%s) = %s, want %s", name, got, want)
		}
	}
}
