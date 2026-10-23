//go:build linux

package cli

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2ToolResultHooks(t *testing.T) {
	for _, hook := range []string{"claude", "codex"} {
		for _, size := range []string{"small", "large"} {
			t.Run(hook+"-"+size, func(t *testing.T) {
				f := newFixture(t)
				f.check("tool-result-hooks/"+hook+"-"+size, oracle.JSON, f.run(input(t, "tool-result/"+hook+"-"+size+".json"), f.resultArgs(hook+"-hook")...), 0)
			})
		}
	}
}
