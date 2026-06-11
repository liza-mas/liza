package toolchain

import (
	"errors"
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
