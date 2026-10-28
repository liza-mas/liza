package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C5 correction scenarios: a C commit changed a golden without its same-commit
// record, and a later ledger-only commit attributes the change to it. The
// demo package stands for a6; its first golden is the one C5 changed.

// editUnrecorded stages the C5 shape: a production change and its golden, with
// no ledger record and no driver assertion flipped.
func (s *scenario) editUnrecorded() {
	s.t.Helper()
	s.put("source.txt", "new")
	s.put(sampleGolden, "new/a\n")
}

// recordAttributed appends the four-field form naming the commit that made the change.
func (s *scenario) recordAttributed(path, reason, sha string) {
	s.t.Helper()
	s.recordPath(path, reason+" "+sha)
}

func TestRegenAttributedRecordBindsHistoricalChange(t *testing.T) {
	s, pre := newD56(t)
	s.editUnrecorded()
	x := s.commit("C5 changes the golden without a record")
	s.recordAttributed(sampleGolden, "C5", x)
	y := s.commit("attribute the C5 change")
	var rec recorder
	report := s.run(rec.regen(true), 0, nil,
		"ok "+sampleGolden+" chain pre "+pre+" C5 "+x+"\n",
		"ok "+staticGolden+" chain pre "+pre+"\n",
		"ok "+demoDir+" package pre "+pre+" C5 "+x+"\n")
	// The attributing commit is no change point: never reproduced, never reported.
	rec.expect(t,
		call{"working", true, []string{"./internal/oracle/..."}, []string{demoPattern}},
		call{pre, true, []string{demoPattern}, nil},
		call{x, false, []string{demoPattern}, nil})
	if strings.Contains(report, y) {
		t.Fatalf("the attributing commit is reported:\n%s", report)
	}
}

func TestRegenUnresolvedCPackageKeepsUntouchedGoldensOnH1(t *testing.T) {
	s, _ := newD56(t)
	s.editUnrecorded()
	s.commit("C5 changes the golden without a record")
	s.recordPath(sampleGolden, "C5")
	y := s.commit("plain late record")
	report := s.check2(t, 1,
		"unexplained "+sampleGolden+": record without change "+y,
		"unexplained "+sampleGolden+": C record has no change point",
		"ok "+staticGolden+" H1\n")
	if strings.Contains(report, "extra ") {
		t.Fatalf("an untouched golden is reported extra:\n%s", report)
	}
}

func TestRegenRejectsInvalidAttribution(t *testing.T) {
	merge := func(s *scenario) string {
		s.git("checkout", "-q", "-b", "side")
		s.put("side.txt", "side")
		s.commit("side")
		s.git("checkout", "-q", "main")
		s.put("main.txt", "main")
		s.commit("main")
		s.git("merge", "--no-ff", "--no-commit", "side")
		s.put(sampleGolden, "merged\n")
		return s.commit("merge changes the golden")
	}
	for _, tt := range []struct {
		name, rule string
		target     func(*scenario) string
	}{
		{"commit outside the history of the ledger commit", "attributed record invalid", func(s *scenario) string {
			s.git("checkout", "-q", "-b", "side")
			s.editUnrecorded()
			x := s.commit("change on a side branch")
			s.git("checkout", "-q", "main")
			s.put("main.txt", "main")
			s.commit("main")
			return x
		}},
		{"merge commit", "attributed record invalid", merge},
		{"commit not changing the path", "attributed record invalid", func(s *scenario) string {
			s.put("other.txt", "other")
			return s.commit("changes another file")
		}},
		{"unknown commit", "attributed record invalid", func(*scenario) string { return strings.Repeat("b", 40) }},
		{"golden already recorded by the commit", "change has multiple records", func(s *scenario) string {
			return s.c1()
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newD56(t)
			x := tt.target(s)
			s.recordAttributed(sampleGolden, "C1", x)
			s.commit("attribute")
			s.check2(t, 1, "unexplained "+sampleGolden+": "+tt.rule+" "+x)
		})
	}
}

func TestRegenWriteNewSkipsPackageWithAttributedRecord(t *testing.T) {
	s, _ := newD56(t)
	s.editUnrecorded()
	x := s.commit("C5 changes the golden without a record")
	s.recordAttributed(sampleGolden, "C5", x)
	s.commit("attribute the C5 change")
	inC := demoDir + "/testdata/golden/json/new.json"
	outside := "internal/oracle/other/testdata/golden/json/new.json"
	reg := func(context.Context, string, string, bool, []string, []string) (map[string][]byte, error) {
		return map[string][]byte{inC: []byte("c"), outside: []byte("o")}, nil
	}
	s.run(reg, 0, []string{"-mode", "write-new"}, "wrote "+outside)
	if _, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(inC))); !os.IsNotExist(err) {
		t.Fatal("write-new wrote into a package holding an attributed C record")
	}
}

func TestLedgerAttributedRecord(t *testing.T) {
	sha := strings.Repeat("a", 40)
	record := func(fields string) string { return "start " + sha + "\nchanged " + fields + "\n" }
	l, err := parseLedger([]byte(record(sampleGolden + " C5 " + sha)))
	if err != nil || len(l.changes) != 1 || l.changes[0].path != sampleGolden || l.changes[0].reason != "C5" || l.changes[0].commit != sha {
		t.Fatalf("attributed record not parsed: %v", err)
	}
	for _, tt := range []struct{ name, fields string }{
		{"non-C reason", sampleGolden + " determinism " + sha},
		{"fix reason", sampleGolden + " fix " + sha},
		{"driver path", "internal/oracle/demo/driver_test.go C5 " + sha},
		{"short commit", sampleGolden + " C5 " + sha[:39]},
		{"long commit", sampleGolden + " C5 " + sha + "a"},
		{"non-hex commit", sampleGolden + " C5 " + strings.Repeat("z", 40)},
		{"fifth field", sampleGolden + " C5 " + sha + " " + sha},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseLedger([]byte(record(tt.fields))); err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("want ledger error naming line 2, got %v", err)
			}
		})
	}
}
