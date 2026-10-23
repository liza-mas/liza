package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestOracleAnalysisReport(t *testing.T) {
	t.Parallel()
	p := newProject(t, "analysis")
	got := p.run(t, false, "analyze")
	requireExit(t, got, 0)
	report := string(readFile(t, filepath.Join(p.root, ".liza", "circuit_breaker_report.md")))
	if !strings.Contains(report, "**Pattern:** retry_cluster") {
		t.Fatal("analysis did not reach retry-cluster detection")
	}
	// A1 owns only the report file and CLI exit. stdout/state belong to A4.
	p.check(t, "human/analysis-report.txt", observation{ExitCode: got.ExitCode, Stdout: report})
}
