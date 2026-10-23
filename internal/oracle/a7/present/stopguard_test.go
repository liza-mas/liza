package present

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleStopGuard(t *testing.T) {
	skipUnlessLinux(t)
	for _, name := range []string{"no-agent-id", "no-live-job", "live-job", "unreadable"} {
		t.Run(name, func(t *testing.T) {
			var extra []string
			if name != "no-agent-id" {
				extra = []string{"LIZA_AGENT_ID=coder-1"}
			}
			e := newEnvironment(t, extra...)
			input, err := os.ReadFile(filepath.Join("testdata", "fixtures", "stop-guard", name+".json"))
			if err != nil {
				t.Fatalf("read stop-guard fixture %s", name)
			}
			if name == "unreadable" {
				// A JSON string stores malformed stdin without excluding the
				// committed .json fixture from repository validation hooks.
				var raw string
				if err := json.Unmarshal(input, &raw); err != nil {
					t.Fatal("decode unreadable stop-guard fixture")
				}
				input = []byte(raw)
			}
			checkResult(t, e, oracle.JSON, "stop-guard/"+name+".json", runCLI(t, e, e.project, input, "hook-stop-guard"))
		})
	}
}
