package main

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRecoverIntegrationCLIRequiresOperator(t *testing.T) {
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "integration-analyst-1")
	rootCmd.SetArgs([]string{"recover-integration", "integration-global-1", "--reason", "premature analysis", "--dry-run"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "operator-only") {
		t.Fatalf("agent recovery accepted: %v", err)
	}
}

func TestRecoverIntegrationCLIJSONErrors(t *testing.T) {
	for _, tc := range []struct{ name, reason, agent, want string }{
		{"operator required", "premature", "integration-analyst-1", "operator-only"},
		{"blank reason", " ", "", "reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetRootCmdForTest(t)
			t.Setenv(brand.EnvName("AGENT_ID"), tc.agent)
			t.Setenv(brand.LegacyEnvName("AGENT_ID"), tc.agent)
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			testhelpers.SetupLizaDir(t, root)
			stdout, err := executeRootCommandCapture(t, root, "recover-integration", "integration-global-1", "--reason", tc.reason, "--dry-run", "--json")
			if err == nil {
				t.Fatal("invalid command succeeded")
			}
			envelope := parseEnvelope(t, stdout)
			if envelope["ok"] != false || !strings.Contains(stdout, tc.want) {
				t.Fatalf("missing refusal envelope: %s", stdout)
			}
		})
	}
}

func TestRecoverIntegrationCLIRequiresReason(t *testing.T) {
	resetRootCmdForTest(t)
	rootCmd.SetArgs([]string{"recover-integration", "integration-global-1", "--dry-run"})
	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("missing reason accepted: %v", err)
	}
}
