package a6

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleIntegrationReconcile(t *testing.T) {
	platformGate(t)
	for _, name := range []string{"projects", "waiting"} {
		t.Run(name, func(t *testing.T) {
			f := integrationFixture(t, name)
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
			if name == "projects" {
				if !result.Changed || !reflect.DeepEqual(result.Created, []string{"integration-slice-plan-multi"}) {
					t.Fatal("coding completion did not project the expected slice analysis")
				}
				for _, value := range []string{"contributing_set:", "coverage:", "plan-single", "approval_attestations:", "integration-slice-plan-multi", "source_snapshot_paths:"} {
					if !bytes.Contains(after, []byte(value)) {
						t.Fatalf("integration projection lacks %s", value)
					}
				}
			} else if result.Changed || len(result.Created) != 0 || !bytes.Equal(before, after) {
				t.Fatal("unsettled planning did not remain an unchanged integration wait")
			}
			assertIsolated(t, f)
			f.norm.CheckState(t, "IntegrationReconcile/"+name+".yaml", []byte(f.preprocess(string(before))), []byte(f.preprocess(string(after))))
		})
	}
}

func integrationFixture(t *testing.T, name string) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	if name == "projects" {
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
	}
	f.install(t, "integration/"+name+".yaml")
	return f
}

func TestOracleRecoverIntegration(t *testing.T) {
	platformGate(t)
	for _, name := range []string{"dry_run", "success", "ineligible"} {
		t.Run(name, func(t *testing.T) {
			c := caseSpec{
				setup: func(t *testing.T) *fixture { return recoveryFixture(t, name) },
				args:  []string{"recover-integration", "integration-global-1", "--reason", "premature freeze"},
				kinds: []oracle.Kind{oracle.JSON, oracle.Human, oracle.State},
				check: func(t *testing.T, f *fixture, o observation) {
					assertIsolated(t, f)
					if name == "ineligible" {
						if !bytes.Contains(append(o.stdout, o.stderr...), []byte("recovery requires a submitted analysis with a worktree and report commit")) {
							t.Fatal("ineligible recovery did not reach the submitted-analysis precondition")
						}
						return
					}
					if name == "success" {
						if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "integration-global-1")); !os.IsNotExist(err) {
							t.Fatal("recovery did not remove its analysis worktree")
						}
						pinned := strings.TrimSpace(f.git(t, "rev-parse", "refs/integration-recovery/integration-global-1"))
						if pinned != f.tokens["@REPORT@"] {
							t.Fatal("recovery did not preserve the submitted report")
						}
					} else {
						pin := runProcess(t, f, "git", []string{"show-ref", "--verify", "--quiet", "refs/integration-recovery/integration-global-1"}, nil)
						if pin.exit != 1 {
							t.Fatal("dry-run created a report preservation ref")
						}
						if _, err := os.Stat(filepath.Join(f.root, ".worktrees", "integration-global-1", "report.md")); err != nil {
							t.Fatal("dry-run removed submitted report worktree")
						}
					}
				},
			}
			if name == "dry_run" {
				c.args = append(c.args, "--dry-run")
				c.noDelta = true
			}
			if name == "ineligible" {
				c.exit, c.noDelta = 1, true
			}
			runCase(t, c)
		})
	}
}

func recoveryFixture(t *testing.T, name string) *fixture {
	t.Helper()
	platformGate(t)
	f := baseFixture(t)
	worktree := filepath.Join(f.root, ".worktrees", "integration-global-1")
	f.git(t, "worktree", "add", "-b", "task/integration-global-1", worktree, "integration")
	payload, err := os.ReadFile(filepath.Join("testdata", "fixtures", "integration", "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, ".worktrees/integration-global-1/report.md", string(payload))
	f.git(t, "-C", worktree, "add", "report.md")
	f.git(t, "-C", worktree, "commit", "-m", "test: preserve premature report")
	f.tokens["@REPORT@"] = strings.TrimSpace(f.git(t, "-C", worktree, "rev-parse", "HEAD"))
	f.tokens["@ANALYSIS_STATUS@"] = "INTEGRATION_ANALYSIS_TO_REVIEW"
	if name == "ineligible" {
		f.tokens["@ANALYSIS_STATUS@"] = "DRAFT_INTEGRATION_ANALYSIS"
	}
	f.install(t, "integration/recovery.yaml")
	assertIsolated(t, f)
	return f
}
