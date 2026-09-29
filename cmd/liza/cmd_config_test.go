package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

func TestConfigGetDelegatedFlagContract(t *testing.T) {
	// config get delegates to get's RunE. Review this contract whenever
	// get gains a flag or changes a type/default: omitted booleans rely
	// on false, while config's field-only query explicitly uses value format.
	// The task-only field flag is absent on config get, yielding no projection.
	snapshot := func(flags *pflag.FlagSet) map[string][2]string {
		result := make(map[string][2]string)
		flags.VisitAll(func(flag *pflag.Flag) {
			if flag.Name != "help" { // Cobra adds help lazily.
				result[flag.Name] = [2]string{flag.Value.Type(), flag.DefValue}
			}
		})
		return result
	}
	wantGet := map[string][2]string{
		"format":         {"string", ""},
		"field":          {"stringArray", "[]"},
		"json":           {"bool", "false"},
		"summary":        {"bool", "false"},
		"active":         {"bool", "false"},
		"zombies":        {"bool", "false"},
		"output-summary": {"bool", "false"},
	}
	wantConfigGet := map[string][2]string{
		"format": {"string", "value"},
		"json":   {"bool", "false"},
	}
	if got := snapshot(getCmd.LocalNonPersistentFlags()); !reflect.DeepEqual(got, wantGet) {
		t.Fatalf("get flag contract changed; review config get delegation: got %v, want %v", got, wantGet)
	}
	if got := snapshot(configGetCmd.LocalNonPersistentFlags()); !reflect.DeepEqual(got, wantConfigGet) {
		t.Fatalf("config get flag contract changed: got %v, want %v", got, wantConfigGet)
	}
}

func TestConfigAgentCapabilityAndGeneration(t *testing.T) {
	root, statePath := setupMutationTestProject(t, nil)
	args := []string{"config", "set", ops.PostWorktreeConfigKey, "make setup", "--agent-id", "orchestrator-1", "--json"}
	stdout, err := executeRootCommandCapture(t, root, args...)
	if err == nil {
		t.Fatal("default orchestrator unexpectedly has config capability")
	}
	assertJSONError(t, stdout, "permission_denied", "config-set-post-worktree-cmd")

	// An explicitly configured capability is allowed, with the same generation
	// fence as other agent mutations. The shipped pipeline remains unchanged.
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatal(err)
	}
	role := cfg.Pipeline.Roles["orchestrator"]
	role.AllowedOperations = append(role.AllowedOperations, "config-set-post-worktree-cmd")
	cfg.Pipeline.Roles["orchestrator"] = role
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.New(root).LizaDir(), "pipeline.yaml"), data, 0644); err != nil {
		t.Fatal(err)
	}

	previous := afterRBACAdmissionTestHook
	t.Cleanup(func() { afterRBACAdmissionTestHook = previous })
	afterRBACAdmissionTestHook = func(agentID string) {
		if err := db.For(statePath).Modify(func(s *models.State) error {
			a := s.Agents[agentID]
			a.Generation = "replacement-generation"
			s.Agents[agentID] = a
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	stdout, err = executeRootCommandCapture(t, root, args...)
	if err == nil || parseEnvelope(t, stdout)["ok"] != false {
		t.Fatalf("stale generation accepted: %s", stdout)
	}
	details := parseEnvelope(t, stdout)["error"].(map[string]any)["details"].(map[string]any)
	if details["outcome"] != "STALE_CALLER" || details["safe_action"] != "stop" {
		t.Fatalf("missing safe fence outcome: %s", stdout)
	}
	if details["current_generation"] != nil || details["losing_generation"] != nil ||
		details["current_generation_fingerprint"] != nil || details["losing_generation_fingerprint"] != nil ||
		strings.Contains(stdout, fmt.Sprintf("%x", sha256.Sum256([]byte("replacement-generation")))) {
		t.Fatal("fence diagnostics exposed registration values or fingerprints")
	}
	if strings.Contains(stdout, "replacement-generation") {
		t.Fatal("fence diagnostic exposes reusable generation")
	}
	if readState(t, statePath).Config.PostWorktreeCmd != nil {
		t.Fatal("stale generation wrote config")
	}
	afterRBACAdmissionTestHook = nil

	// Use the direct command runner because executeRootCommand intentionally
	// resets the generation environment to its shared fixture value.
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_GENERATION"), "replacement-generation")
	t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
	rootCmd.SetArgs([]string{"-C", root, "config", "set", ops.PostWorktreeConfigKey, "make setup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := readState(t, statePath).Config.PostWorktreeCmd; got == nil || *got != "make setup" {
		t.Fatal("current generation did not write config")
	}
}

func TestConfigJSONKeepsAuditInProcessLog(t *testing.T) {
	root, _ := setupMutationTestProject(t, nil)
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.PostWorktreeConfigKey, "make setup", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if parseEnvelope(t, stdout)["ok"] != true {
		t.Fatal(stdout)
	}
	if !strings.Contains(output.String(), "config_set key="+ops.PostWorktreeConfigKey) {
		t.Fatal("JSON mode discarded audit record")
	}
	if strings.Contains(stdout, "config_set") {
		t.Fatal("audit contaminated JSON stdout")
	}
}

func TestConfigSetAfterMergeDetectionReturnsConflict(t *testing.T) {
	const taskID = "config-after-merge"
	root, statePath, _ := setupE2EMergeProject(t, taskID, "code-reviewer-1")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"bootstrap"}`), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, root, "add", "package.json")
	testhelpers.MustGit(t, root, "commit", "-m", "Add project scaffold")
	commit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	if err := db.For(statePath).Modify(func(s *models.State) error {
		s.FindTask(taskID).ReviewCommit = &commit
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.MergeWorktree(root, taskID, "code-reviewer-1"); err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.PostWorktreeConfigKey, "make custom", "--json")
	if err == nil {
		t.Fatal("CLI reported success after overwriting detected command")
	}
	assertJSONError(t, stdout, "validation", "already set", "--replace")
	if got := readState(t, statePath).Config.PostWorktreeCmd; got == nil || *got != "npm install" {
		t.Fatal("detected command lost")
	}
}

func TestConfigPoolKeys(t *testing.T) {
	root, statePath := setupMutationTestProject(t, nil)
	run := func(args ...string) (string, error) {
		t.Helper()
		return executeRootCommandCapture(t, root, append([]string{"config"}, args...)...)
	}

	// Unset max_instances takes a first value without --replace.
	stdout, err := run("set", ops.MaxInstancesConfigKey, "5", "--json")
	if err != nil || parseEnvelope(t, stdout)["ok"] != true {
		t.Fatalf("first set failed: %v %s", err, stdout)
	}
	if got := readState(t, statePath).Config.MaxInstances; got != 5 {
		t.Fatalf("max_instances = %d, want 5", got)
	}

	// A different value needs --replace with --reason.
	stdout, err = run("set", ops.MaxInstancesConfigKey, "4", "--json")
	if err == nil {
		t.Fatal("replacement without --replace succeeded")
	}
	assertJSONError(t, stdout, "validation", "already set to 5", "--replace")
	stdout, err = run("set", ops.MaxInstancesConfigKey, "4", "--replace", "--reason", "narrower fan-out", "--json")
	if err != nil || parseEnvelope(t, stdout)["ok"] != true {
		t.Fatalf("replacement failed: %v %s", err, stdout)
	}
	if got := readState(t, statePath).Config.MaxInstances; got != 4 {
		t.Fatalf("max_instances = %d, want 4", got)
	}
	stdout, err = run("get", ops.MaxInstancesConfigKey, "--json")
	if err != nil || parseEnvelope(t, stdout)["result"] != float64(4) {
		t.Fatalf("config get = %s (%v), want 4", stdout, err)
	}

	// Bounds and parsing are enforced before any write.
	for _, tc := range []struct {
		key, value, want string
	}{
		{ops.MaxInstancesConfigKey, "0", "at least 1"},
		{ops.DoerMaxWaitConfigKey, "120", "at least 300"},
		{ops.ReviewerMaxWaitConfigKey, "ten", "integer"},
	} {
		stdout, err = run("set", tc.key, tc.value, "--replace", "--reason", "bounds", "--json")
		if err == nil {
			t.Fatalf("set %s %s succeeded", tc.key, tc.value)
		}
		assertJSONError(t, stdout, "validation", tc.want)
	}
	stdout, err = run("set", ops.ReviewerMaxWaitConfigKey, "900", "--replace", "--reason", "slower idle exit", "--json")
	if err != nil || parseEnvelope(t, stdout)["ok"] != true {
		t.Fatalf("reviewer wait set failed: %v %s", err, stdout)
	}
	if got := readState(t, statePath).Config.ReviewerMaxWait; got != 900 {
		t.Fatalf("reviewer_max_wait = %d, want 900", got)
	}

	// Agents cannot resize their own pool.
	stdout, err = run("set", ops.MaxInstancesConfigKey, "9", "--agent-id", "orchestrator-1", "--replace", "--reason", "agent", "--json")
	if err == nil {
		t.Fatal("agent write accepted")
	}
	assertJSONError(t, stdout, "validation", "operator-only")
	if got := readState(t, statePath).Config.MaxInstances; got != 4 {
		t.Fatalf("max_instances = %d after refused agent write, want 4", got)
	}
}
