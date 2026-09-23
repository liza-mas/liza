package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pkgA = "example.com/m/a"

func ev(action, pkg, test string, elapsed float64, output string) string {
	return fmt.Sprintf(`{"Time":"2026-09-23T10:00:00Z","Action":%q,"Package":%q,"Test":%q,"Elapsed":%v,"Output":%q}`,
		action, pkg, test, elapsed, output)
}

func sampleStream() string {
	return strings.Join([]string{
		`{"ImportPath":"example.com/m/a","Action":"build-output","Output":"# example.com/m/a\n"}`,
		ev("start", pkgA, "", 0, ""),
		ev("run", pkgA, "TestOK", 0, ""),
		ev("output", pkgA, "TestOK", 0, "=== RUN   TestOK\n"),
		ev("output", pkgA, "TestOK", 0, "quiet on success\n"),
		ev("pass", pkgA, "TestOK", 1.5, ""),
		ev("run", pkgA, "TestBad", 0, ""),
		ev("run", pkgA, "TestBad/sub", 0, ""),
		ev("output", pkgA, "TestBad/sub", 0, "sub failure detail\n"),
		ev("fail", pkgA, "TestBad/sub", 0.2, ""),
		ev("output", pkgA, "TestBad", 0, "parent detail\n"),
		ev("fail", pkgA, "TestBad", 3.3, ""),
		ev("run", pkgA, "TestSkip", 0, ""),
		ev("skip", pkgA, "TestSkip", 0, ""),
		"panic text that is not JSON",
		ev("output", pkgA, "", 0, "FAIL\texample.com/m/a\t5.0s\n"),
		ev("fail", pkgA, "", 5.0, ""),
	}, "\n") + "\n"
}

func TestReadResultsKeepsPackageAndTopLevelTerminals(t *testing.T) {
	pkgs, tests, err := readResults(strings.NewReader(sampleStream()), "ops-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Action != "fail" || pkgs[0].Elapsed != 5.0 {
		t.Fatalf("package results %+v", pkgs)
	}
	var got []string
	for _, r := range tests {
		got = append(got, r.Test+":"+r.Action)
	}
	if want := "TestOK:pass,TestBad:fail,TestSkip:skip"; strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

func TestFormatterPrintsFailuresNotPasses(t *testing.T) {
	var out bytes.Buffer
	if err := runFormatter(strings.NewReader(sampleStream()), &out, 0); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"# example.com/m/a", "sub failure detail", "parent detail", "FAIL\texample.com/m/a", "panic text that is not JSON", "passed=1 failed=1 skipped=1"} {
		if !strings.Contains(s, want) {
			t.Errorf("formatter output lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "quiet on success") {
		t.Errorf("formatter printed output of a passing test:\n%s", s)
	}
}

func TestFormatterHeartbeatNamesRunningTest(t *testing.T) {
	var out bytes.Buffer
	f := newFormatter(&out)
	f.handle([]byte(ev("run", pkgA, "TestStuck", 0, "")))
	f.handle([]byte(ev("run", pkgA, "TestWaiting", 0, "")))
	f.handle([]byte(ev("pause", pkgA, "TestWaiting", 0, "")))
	f.heartbeat()
	if !strings.Contains(out.String(), "running: example.com/m/a TestStuck (") {
		t.Fatalf("heartbeat does not name the running test: %s", out.String())
	}
	if strings.Contains(out.String(), "TestWaiting") {
		t.Fatalf("heartbeat lists a paused parallel test as running: %s", out.String())
	}
}

func TestSummaryListsFailuresAndSlowest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "windows-test-ops-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte(sampleStream()), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := cmdSummary([]string{"-top", "2", filepath.Dir(dir)}, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"1 fail", "`example.com/m/a TestBad`", "| 1 | 3.3 | fail | windows-test-ops-1 | `a.TestBad` |", "2 slowest packages"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "TestSkip") {
		t.Errorf("summary exceeded -top 2:\n%s", s)
	}
}
