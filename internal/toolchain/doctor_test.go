package toolchain

import (
	"errors"
	"strings"
	"testing"
)

type failingRunner struct {
	fakeRunner
}

func (f *failingRunner) Run(command Command) (CommandOutput, error) {
	return CommandOutput{Stderr: "bad", ExitCode: 2}, errors.New("command failed")
}

func TestDoctorReportsOK(t *testing.T) {
	runner := &fakeRunner{paths: map[string]string{"rtk": "/bin/rtk"}}

	got, err := Doctor(DoctorOptions{ToolID: "rtk", Runner: runner})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if len(got.Checks) != 1 {
		t.Fatalf("checks = %d, want 1", len(got.Checks))
	}
	if got.Checks[0].Status != DoctorOK || got.Checks[0].Path != "/bin/rtk" {
		t.Fatalf("check = %+v, want ok path", got.Checks[0])
	}
}

func TestDoctorReportsMissing(t *testing.T) {
	got, err := Doctor(DoctorOptions{ToolID: "rtk", Runner: &fakeRunner{}})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if got.Checks[0].Status != DoctorMissing {
		t.Fatalf("status = %s, want missing", got.Checks[0].Status)
	}
}

func TestDoctorReportsManualCapability(t *testing.T) {
	got, err := Doctor(DoctorOptions{ToolID: "postgres-mcp", Runner: &fakeRunner{}})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if got.Checks[0].Status != DoctorManual {
		t.Fatalf("status = %s, want manual", got.Checks[0].Status)
	}
}

func TestDoctorReportsFailedVersionProbe(t *testing.T) {
	runner := &failingRunner{fakeRunner: fakeRunner{paths: map[string]string{"rtk": "/bin/rtk"}}}
	got, err := Doctor(DoctorOptions{ToolID: "rtk", Runner: runner})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if got.Checks[0].Status != DoctorFailed {
		t.Fatalf("status = %s, want failed", got.Checks[0].Status)
	}
}

// helpRunner answers --help with a fixed listing and every other probe with
// success.
type helpRunner struct {
	fakeRunner
	help string
}

func (h *helpRunner) Run(command Command) (CommandOutput, error) {
	if len(command.Args) == 1 && command.Args[0] == "--help" {
		return CommandOutput{Stdout: h.help}, nil
	}
	return CommandOutput{Stdout: "scip-search dev"}, nil
}

func TestDoctorAcceptsBinaryListingRequiredCommand(t *testing.T) {
	runner := &helpRunner{
		fakeRunner: fakeRunner{paths: map[string]string{"scip-search": "/bin/scip-search"}},
		help:       "Commands:\n  aggregate-index  Merge indexes.\n  reroot           Copy a SCIP index with a different metadata project root.",
	}
	got, err := Doctor(DoctorOptions{ToolID: "scip-search", Runner: runner})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	if got.Checks[0].Status != DoctorOK {
		t.Fatalf("check = %+v, want ok", got.Checks[0])
	}
}

func TestDoctorReportsBinaryLackingRequiredCommand(t *testing.T) {
	runner := &helpRunner{
		fakeRunner: fakeRunner{paths: map[string]string{"scip-search": "/bin/scip-search"}},
		help:       "Commands:\n  aggregate-index  Merge indexes.\n  rerooted-cache   Not the reroot command.",
	}
	got, err := Doctor(DoctorOptions{ToolID: "scip-search", Runner: runner})
	if err != nil {
		t.Fatalf("Doctor() error = %v", err)
	}
	check := got.Checks[0]
	if check.Status != DoctorFailed {
		t.Fatalf("status = %s, want failed", check.Status)
	}
	for _, want := range []string{"lacks required command reroot", "toolchain install scip-search"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("message = %q, want to contain %q", check.Message, want)
		}
	}
}
