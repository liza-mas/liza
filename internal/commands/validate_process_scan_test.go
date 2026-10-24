package commands

import (
	"bytes"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestValidateWarnsWhenProcessScanUnavailable characterizes why the A4 D35
// `_after` validation golden is Linux-only (D53): a host without procfs skips
// the live agent process scan with a warning, while a host with it scans and
// stays silent when no process is out of place. The test fixes no behaviour.
func TestValidateWarnsWhenProcessScanUnavailable(t *testing.T) {
	wantWarning := "WARNING: Live " + brand.BinaryName + " agent process scan skipped (procfs unavailable on this host)\n"

	tests := []struct {
		name        string
		scan        func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error)
		wantWarning string
	}{
		{
			name: "procfs unavailable",
			scan: func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
				return procscan.ZombieScanResult{}, procscan.ErrProcessScanUnavailable
			},
			wantWarning: wantWarning,
		},
		{
			name: "scan available and no zombies",
			scan: func(procscan.ZombieScanOptions) (procscan.ZombieScanResult, error) {
				return procscan.ZombieScanResult{}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalFind := findZombieAgents
			t.Cleanup(func() { findZombieAgents = originalFind })
			findZombieAgents = tt.scan

			tmpDir := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.SetupPipelineConfig(t, tmpDir)
			testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

			var warnings bytes.Buffer
			err := ValidateCommandWithOptions(statePath, ValidateOptions{SkipSpecFileCheck: true, WarnWriter: &warnings})
			if err != nil {
				t.Fatalf("ValidateCommandWithOptions() error = %v, want nil", err)
			}
			if got := warnings.String(); got != tt.wantWarning {
				t.Fatalf("warnings = %q, want %q", got, tt.wantWarning)
			}
		})
	}
}
