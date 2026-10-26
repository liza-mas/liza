package a6

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestOracleIntegrationReconcileGaps covers evaluator branches of the K-INT
// split that I1/I2 leave unmapped: a decision waiting on incomplete slice
// coverage, and the global-analysis projection after complete coverage.
// Both run the production reconciliation in the delivered re-executed adapter.
func TestOracleIntegrationReconcileGaps(t *testing.T) {
	platformGate(t)
	for _, c := range []struct {
		name    string
		created []string
		holds   []string
	}{
		{name: "coverage_waiting"},
		{
			name: "global_projection", created: []string{"integration-global-1"},
			holds: []string{"integration-global-1", "global:1", "integration-slice-plan-multi", "coding-single"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := gapIntegrationFixture(t, c.name)
			assertIsolated(t, f)
			before := f.state(t)
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			o := runProcess(t, f, binary, []string{f.root}, []string{"ORACLE_A6_HELPER=integration"})
			if o.exit != 0 {
				t.Fatalf("integration helper failed: exit %d: %s", o.exit, o.stderr)
			}
			var result struct {
				Changed bool     `json:"changed"`
				Created []string `json:"created_task_ids"`
			}
			if err := json.Unmarshal(o.stdout, &result); err != nil {
				t.Fatalf("integration helper result: %v", err)
			}
			after := f.state(t)
			if c.created == nil {
				if result.Changed || len(result.Created) != 0 || !bytes.Equal(before, after) {
					t.Fatal("incomplete slice coverage did not remain an unchanged integration wait")
				}
			} else {
				if !result.Changed || !reflect.DeepEqual(result.Created, c.created) {
					t.Fatal("complete coverage did not project the first global analysis")
				}
				for _, value := range c.holds {
					if !bytes.Contains(after, []byte(value)) {
						t.Fatalf("global projection lacks %s", value)
					}
				}
			}
			assertIsolated(t, f)
			f.norm.CheckState(t, "IntegrationReconcileGaps/"+c.name+".yaml", []byte(f.preprocess(string(before))), []byte(f.preprocess(string(after))))
		})
	}
}

func gapIntegrationFixture(t *testing.T, name string) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	for _, change := range []struct{ path, token string }{{"a.txt", "@A@"}, {"b.txt", "@B@"}, {"single.txt", "@HEAD@"}} {
		payload, err := os.ReadFile(filepath.Join("testdata", "fixtures", "integration", change.path))
		if err != nil {
			t.Fatal(err)
		}
		f.write(t, change.path, string(payload))
		f.git(t, "add", change.path)
		f.git(t, "commit", "-m", "test: add "+change.path)
		f.tokens[change.token] = strings.TrimSpace(f.git(t, "rev-parse", "HEAD"))
	}
	f.install(t, "gap/"+strings.ReplaceAll(name, "_", "-")+".yaml")
	return f
}
