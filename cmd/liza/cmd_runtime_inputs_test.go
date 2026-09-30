package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupRuntimeInputCLIProject(t *testing.T) (string, string, string) {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	t.Setenv("HOME", t.TempDir()) // The operator key lives under the global dir.
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	task.Validation = []string{"sh boundary_test.sh"}
	task.RuntimeInputs = []models.RuntimeInput{{ID: "principals", Commands: task.Validation, Recipe: "project.principals",
		Consumption: models.RuntimeInputReusable, Secret: true, Env: []string{"MEMBER_CREDENTIAL"}}}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, statePath, state)
	envelope := filepath.Join(t.TempDir(), "principals.env")
	if err := os.WriteFile(envelope, []byte("MEMBER_CREDENTIAL=rehearsal-credential-77\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, statePath, envelope
}

func TestProvisionCLIIsOperatorOnlyAndRecordOnly(t *testing.T) {
	root, statePath, envelope := setupRuntimeInputCLIProject(t)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(brand.EnvName("AGENT_ID"), "coder-1")
	if _, err := executeRootCommandCapture(t, root, "provision", "--record", "--task", "live", "--input", "principals", "--file", envelope); err == nil || !strings.Contains(err.Error(), "operator-only") {
		t.Fatalf("agent provision err = %v, want operator-only refusal", err)
	}
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	if _, err := executeRootCommandCapture(t, root, "provision", "--task", "live", "--input", "principals", "--file", envelope); err == nil || !strings.Contains(err.Error(), "--record") {
		t.Fatalf("provision without --record err = %v", err)
	}
	if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
		t.Fatal("refused provision changed state")
	}
}

func TestRunLiveCLIDeliversMaskedReusableInputAndExitCode(t *testing.T) {
	root, statePath, envelope := setupRuntimeInputCLIProject(t)
	if _, err := executeRootCommandCapture(t, root, "provision", "--record", "--task", "live", "--input", "principals", "--file", envelope); err != nil {
		t.Fatalf("provision: %v", err)
	}
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "rehearsal-credential-77") || !strings.Contains(string(state), "MEMBER_CREDENTIAL") {
		t.Fatal("ledger must record the variable name and never its value")
	}
	var captured bytes.Buffer
	runLiveCmd.SetOut(&captured) // The test reset discards the root's output.
	t.Cleanup(func() { runLiveCmd.SetOut(nil) })
	_, err = executeRootCommandCapture(t, root, "run-live", "--task", "live", "--", "sh", "-c", `printf 'credential=%s' "$MEMBER_CREDENTIAL"; exit 4`)
	out := captured.String()
	var exit *toolResultExit
	if !errors.As(err, &exit) || exit.code != 4 {
		t.Fatalf("run-live err = %v, want the command's exit code 4", err)
	}
	if !strings.Contains(out, "credential=***") || strings.Contains(out, "rehearsal-credential-77") {
		t.Fatalf("run-live output = %q, want the secret delivered and masked", out)
	}
}
