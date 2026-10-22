package main

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupDeferredValidationCLIProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	return root, statePath
}

func TestDeferredValidationCLIAuthorityIsOperatorOnly(t *testing.T) {
	for _, envName := range []string{brand.EnvName("AGENT_ID"), brand.LegacyEnvName("AGENT_ID")} {
		for _, args := range [][]string{
			{"deferred-validation", "rule", "--command", "make long", "--ruling", "D-97"},
			{"deferred-validation", "unrule", "--command", "make long"},
			{"deferred-validation", "retire", "--command", "make long", "--ruling", "D-97"},
			{"deferred-validation", "settle", "--ruling", "D-97"},
			{"deferred-validation", "abort", "--ruling", "D-181"},
		} {
			t.Run(envName+"/"+args[1], func(t *testing.T) {
				resetRootCmdForTest(t)
				t.Setenv(brand.EnvName("AGENT_ID"), "")
				t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
				t.Setenv(envName, "coder-1")
				root, statePath := setupDeferredValidationCLIProject(t)
				before, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				_, err = executeRootCommandCapture(t, root, args...)
				if err == nil || !strings.Contains(err.Error(), "operator-only") {
					t.Fatalf("agent session %s = %v, want operator-only refusal", args[1], err)
				}
				if after, _ := os.ReadFile(statePath); !bytes.Equal(before, after) {
					t.Fatal("refused agent session changed state")
				}
			})
		}
	}
}

func TestDeferredValidationCLIRuleAndRunJSON(t *testing.T) {
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root, statePath := setupDeferredValidationCLIProject(t)

	if _, err := executeRootCommandCapture(t, root, "deferred-validation", "rule", "--command", "make long", "--ruling", "D-97", "--json"); err != nil {
		t.Fatalf("operator rule failed: %v", err)
	}
	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if ruling := models.DeferredValidationRulingFor(state.Config, "make long"); ruling == nil || ruling.Ruling != "D-97" {
		t.Fatalf("rule did not record the ruling: %+v", state.Config.DeferredValidation)
	}

	// An agent session may run the batch; with nothing deferred it is a no-op.
	t.Setenv(brand.EnvName("AGENT_ID"), "coder-1")
	resetRootCmdForTest(t)
	stdout, err := executeRootCommandCapture(t, root, "deferred-validation", "run", "--json")
	if runtime.GOOS == "windows" {
		// ADR-0204: the runner refuses on Windows before reading the ledger.
		if !errors.Is(err, jsonout.ErrAlreadyWritten) {
			t.Fatalf("run on windows = %v, want the refusal written as JSON", err)
		}
		assertJSONError(t, stdout, "validation", "not supported on windows")
		return
	}
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	env := parseEnvelope(t, stdout)
	result, _ := env["result"].(map[string]any)
	if env["ok"] != true || result["outcome"] != "skipped" || result["reason"] != "nothing_deferred" {
		t.Fatalf("run envelope = %v, want ok skipped/nothing_deferred", env)
	}
}
