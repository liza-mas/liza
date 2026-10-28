//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2ToolResultRun(t *testing.T) {
	for _, size := range []string{"small", "large"} {
		t.Run(size, func(t *testing.T) {
			f := newFixture(t)
			f.stub("fixture-output", "output")
			content := input(t, "tool-result/"+size+".txt")
			path := filepath.Join(f.project, "output.txt")
			must(t, os.WriteFile(path, content, 0644))
			args := f.resultArgs("run", "--tool", "Bash", "--command", "fixture output", "--", filepath.Join(f.bin, "fixture-output"), size, path)
			result := f.run(nil, args...)
			kind := oracle.Human
			if size == "large" {
				kind = oracle.JSON
			}
			f.check("tool-result-run/"+size, kind, result, 0)
			if size == "large" {
				f.retained(result, content)
			}
		})
	}
}
