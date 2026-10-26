package present

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// poolFixtures holds the committed states of gap G-a: each directory is
// overlaid on the presentation project's .liza directory, so state.yaml, an
// optional models.yaml and optional provider quota signals are committed files.
const poolFixtures = "testdata/fixtures/gap/repair-pool"

// coding-pair reviews with quorum 1 in the frozen pipeline; a quorum of 2 lets
// a half-approved task await a second review, which the review-slot cases need.
// The edit is applied to every pool project, so one pipeline serves all cases.
var (
	poolPipelinePolicy = []byte("task-slug: code\n      review-policy:\n        quorum: 1\n")
	poolPipelineQuorum = []byte("task-slug: code\n      review-policy:\n        quorum: 2\n")
)

// materialisePool builds the presentation project with the named pool state
// overlaid on its .liza directory.
func materialisePool(t *testing.T, e *environment, name string) string {
	t.Helper()
	project := materialiseFixture(t, e)
	liza := filepath.Join(project, ".liza")
	pipelinePath := filepath.Join(liza, "pipeline.yaml")
	pipeline, err := os.ReadFile(pipelinePath)
	if err != nil || bytes.Count(pipeline, poolPipelinePolicy) != 1 {
		t.Fatal("fixture pipeline must hold exactly one coding-pair quorum policy")
	}
	if err := os.WriteFile(pipelinePath, bytes.Replace(pipeline, poolPipelinePolicy, poolPipelineQuorum, 1), 0o644); err != nil {
		t.Fatal("write fixture pipeline")
	}
	source := filepath.Join(poolFixtures, name)
	entries, err := os.ReadDir(source)
	if err != nil || len(entries) == 0 {
		t.Fatalf("read pool fixture %s", name)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			t.Fatalf("read pool fixture file %s", entry.Name())
		}
		if err := os.WriteFile(filepath.Join(liza, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("write pool fixture file %s", entry.Name())
		}
	}
	return project
}

func TestRepairPoolFixturesValidate(t *testing.T) {
	skipUnlessLinux(t)
	for _, name := range []string{"demand", "provider-blocked", "merge-owner", "slots"} {
		t.Run(name, func(t *testing.T) {
			e := newEnvironment(t)
			result := runCLI(t, e, materialisePool(t, e, name), nil, "validate")
			if result.exit != 0 {
				t.Fatalf("pool fixture validate exited %d:\n%s\n%s", result.exit, e.norm.Text(string(result.stdout)), e.norm.Text(string(result.stderr)))
			}
		})
	}
}

// TestOracleRepairAgentPoolDryRun pins the frozen surface of
// `repair-agent-pool --dry-run` (gap G-a): the binary runs over the committed
// pool states and the golden records stdout, stderr and the exit code. Cases
// cover the default, --missing, an explicit --cli, the refusal of an unknown
// CLI, a merge awaiting an owner and the no-usable-provider refusal (a quota
// signal blocks the default CLI, so the role has no provider to start). The
// command registers no --json flag: every case also runs with --json, and its
// golden freezes the usage refusal exactly as the starting tree prints it. A
// later unit may not add JSON output (goal-frozen).
func TestOracleRepairAgentPoolDryRun(t *testing.T) {
	skipUnlessLinux(t)
	type poolCase struct {
		name, project string
		args          []string
	}
	cases := []poolCase{
		{"default", "demand", []string{"--dry-run"}},
		{"missing", "demand", []string{"--missing", "--dry-run"}},
		{"cli-codex", "demand", []string{"--cli", "codex", "--dry-run"}},
		{"invalid-cli", "demand", []string{"--cli", "oracle-unknown", "--dry-run"}},
		{"merge-owner", "merge-owner", []string{"--dry-run"}},
		{"review-slots", "slots", []string{"--dry-run"}},
		{"review-slots-explicit-cli", "slots", []string{"--cli", "codex", "--dry-run"}},
		{"no-usable-provider", "provider-blocked", []string{"--dry-run"}},
		{"no-usable-provider-explicit-cli", "provider-blocked", []string{"--cli", "codex", "--dry-run"}},
	}
	for _, tc := range cases {
		for _, json := range []bool{false, true} {
			name, args := tc.name, append([]string{"repair-agent-pool"}, tc.args...)
			if json {
				name, args = name+"-json", append(args, "--json")
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				e := newEnvironment(t)
				project := materialisePool(t, e, tc.project)
				checkResult(t, e, oracle.Human, "repair-pool/"+name+".txt", runCLI(t, e, project, nil, args...))
			})
		}
	}
}
