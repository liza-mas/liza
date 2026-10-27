package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C3: the spawn gate refuses a CLI whose C2 provider key matches a
// provider-unavailable record, under the project's runtime configuration, after
// the quota gate and with the requested CLI named in the error and the alert.

func c3SpawnProject(t *testing.T, tools map[string]models.AgentToolConfig) (string, string) {
	t.Helper()
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	state.Config.AgentTools = tools
	testhelpers.WriteInitialState(t, statePath, state)
	t.Setenv("PATH", t.TempDir())
	return projectRoot, filepath.Join(projectRoot, paths.ProjectDirName(), "alerts.log")
}

func c3Unavailable(t *testing.T, projectRoot, label, tool string) {
	t.Helper()
	if err := providersignal.WriteProviderUnavailableSignal(projectRoot, &providersignal.ProviderUnavailable{Provider: label, Tool: tool, Message: "session access denied"}); err != nil {
		t.Fatal(err)
	}
}

func TestC3SpawnRefusesByCanonicalKey(t *testing.T) {
	shared := map[string]models.AgentToolConfig{"foo": {ProviderKey: "shared"}, "bar": {ProviderKey: "shared"}, "baz": {ProviderKey: "other"}}
	split := map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}
	for _, tc := range []struct {
		name         string
		tools        map[string]models.AgentToolConfig
		label, raise string
		spawn        string
		refused      bool
	}{
		{"codex raise refuses codex-acp", nil, "codex", "codex", "codex-acp", true},
		{"codex-acp raise refuses codex", nil, "codex", "codex-acp", "codex", true},
		{"custom shared key", shared, "foo", "foo", "bar", true},
		{"custom unrelated key", shared, "foo", "foo", "baz", false},
		{"override separates aliases", split, "codex", "codex", "codex-acp", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot, alertsPath := c3SpawnProject(t, tc.tools)
			c3Unavailable(t, projectRoot, tc.label, tc.raise)

			_, err := SpawnAgent(projectRoot, "coder", tc.spawn)
			refusal := "provider unavailable for " + tc.spawn + "; refusing to spawn coder"
			if tc.refused {
				if err == nil || err.Error() != refusal {
					t.Fatalf("SpawnAgent error = %v, want %q naming the requested CLI", err, refusal)
				}
				alerts, _ := os.ReadFile(alertsPath)
				if !strings.Contains(string(alerts), tc.spawn+": refused to spawn coder while provider-unavailable signal is set") {
					t.Fatalf("alert does not name the requested CLI %q:\n%s", tc.spawn, alerts)
				}
				return
			}
			if err != nil && strings.Contains(err.Error(), "provider unavailable") {
				t.Fatalf("SpawnAgent refused %s on an unrelated key: %v", tc.spawn, err)
			}
		})
	}
}

func TestC3SpawnQuotaRefusalPrecedesUnavailableAlias(t *testing.T) {
	projectRoot, alertsPath := c3SpawnProject(t, nil)
	if err := providersignal.WriteQuotaSignal(projectRoot, "codex", "quota hit"); err != nil {
		t.Fatal(err)
	}
	c3Unavailable(t, projectRoot, "codex", "codex-acp") // matches codex under C2 keys

	_, err := SpawnAgent(projectRoot, "coder", "codex")
	if err == nil || err.Error() != "provider quota exhausted for codex; refusing to spawn coder" {
		t.Fatalf("SpawnAgent error = %v, want the quota refusal before the unavailable refusal", err)
	}
	alerts, _ := os.ReadFile(alertsPath)
	if strings.Contains(string(alerts), "PROVIDER UNAVAILABLE SPAWN BLOCKED") {
		t.Fatalf("unavailable gate ran before the quota gate:\n%s", alerts)
	}
}

func TestC3QuotaStaysSpelledAlias(t *testing.T) {
	// Quota matching is not converted to C2 keys: a quota block for one tool of
	// a shared custom key does not block its sibling.
	projectRoot, _ := c3SpawnProject(t, map[string]models.AgentToolConfig{"foo": {ProviderKey: "shared"}, "bar": {ProviderKey: "shared"}})
	if err := providersignal.WriteQuotaSignal(projectRoot, "foo", "quota hit"); err != nil {
		t.Fatal(err)
	}
	if providersignal.CheckQuotaSignal(projectRoot, "bar") {
		t.Fatal("a quota signal for foo must not block bar through a shared provider key")
	}
	if !providersignal.CheckQuotaSignal(projectRoot, "foo") {
		t.Fatal("a quota signal still blocks its own tool")
	}
}
