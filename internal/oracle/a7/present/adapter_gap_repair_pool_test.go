package present

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// spawnRequest is one start the production entry asked the launcher for.
type spawnRequest struct {
	role, cli, agentID string
	cliFromConfig      bool
	modelsItem         int
}

func (r spawnRequest) String() string {
	return fmt.Sprintf("role=%s cli=%s agent_id=%s from_config=%t models_item=%d", r.role, r.cli, r.agentID, r.cliFromConfig, r.modelsItem)
}

type poolSpawnCase struct {
	name, project string
	options       func(commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions
	// fail returns the error a start of role (list item 0 for none) fails with.
	fail func(role string, item int) error
	// livePID registers the fixture's coder-1 under this process's PID, which
	// makes it a live claim-valid agent.
	livePID bool
}

func poolSpawnCases() []poolSpawnCase {
	refuse := func(roles ...string) func(string, int) error {
		return func(role string, _ int) error {
			if slices.Contains(roles, role) {
				return errors.New("oracle launcher refused " + role)
			}
			return nil
		}
	}
	return []poolSpawnCase{
		{name: "spawn-success", project: "demand"},
		{name: "spawn-failure", project: "demand", fail: refuse("code-reviewer", "orchestrator")},
		{name: "spawn-failure-every-role", project: "demand", fail: refuse("coder", "code-reviewer", "orchestrator")},
		{name: "dry-run", project: "demand", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.DryRun = true
			return o
		}},
		{name: "explicit-cli", project: "demand", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.CLI = "codex"
			return o
		}},
		{name: "roles-filter", project: "demand", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.Roles = []string{"code-reviewer"}
			return o
		}},
		{name: "pending-spawns", project: "demand", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.PendingSpawns = map[string][]int{"coder": {0}}
			o.PendingAgentIDs = map[string]bool{"code-reviewer-1": true}
			return o
		}},
		{name: "covered-by-idle-agent", project: "demand", livePID: true},
		{name: "merge-owner", project: "merge-owner"},
		{name: "review-slots", project: "slots"},
		{name: "review-slots-item-failure", project: "slots", fail: func(role string, item int) error {
			if role == "code-reviewer" && item == 1 {
				return errors.New("oracle launcher refused item 1")
			}
			return nil
		}},
		{name: "unservable", project: "slots", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.CLI = "codex"
			return o
		}},
		{name: "provider-blocked", project: "provider-blocked"},
		{name: "provider-blocked-explicit-cli", project: "provider-blocked", options: func(o commands.RepairAgentPoolOptions) commands.RepairAgentPoolOptions {
			o.CLI = "codex"
			return o
		}},
	}
}

// TestOracleRepairAgentPoolSpawn pins the production entry
// commands.RepairAgentPool (gap G-a): the exported spawn seam records every
// start request (role, CLI, agent ID, configured-CLI flag, models item) and
// starts no process. The golden holds the requests, the result as JSON and the
// error of RepairAgentPool, then the output and error of RepairAgentPoolCommand
// over the same project. Cases cover spawn success and failure (all roles, some
// roles, one list item), a dry run, an explicit CLI, a role filter, pending
// spawns, an agent covering demand, an ownerless merge, review slots, work no
// reviewer could claim, and a quota-blocked provider.
func TestOracleRepairAgentPoolSpawn(t *testing.T) {
	skipUnlessLinux(t)
	// The seam, the process environment and stdout are process-wide.
	for _, tc := range poolSpawnCases() {
		t.Run(tc.name, func(t *testing.T) { runPoolSpawnCase(t, tc) })
	}
}

func runPoolSpawnCase(t *testing.T, tc poolSpawnCase) {
	t.Helper()
	e := newEnvironment(t)
	project := materialisePool(t, e, tc.project)
	if tc.livePID {
		statePath := filepath.Join(project, ".liza", "state.yaml")
		state, err := os.ReadFile(statePath)
		if err != nil || !bytes.Contains(state, []byte("5000001")) {
			t.Fatal("fixture coder-1 must carry the placeholder PID")
		}
		if err := os.WriteFile(statePath, bytes.ReplaceAll(state, []byte("5000001"), []byte(strconv.Itoa(os.Getpid()))), 0o644); err != nil {
			t.Fatal("write fixture state")
		}
	}
	isolateProcessEnvironment(t, e)

	var requests []spawnRequest
	spawned := 0
	restore := commands.SetRepairAgentPoolSpawnForTest(func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error) {
		if projectRoot != project {
			t.Errorf("start request names a project other than the fixture")
		}
		requests = append(requests, spawnRequest{role, cli, agentID, cliFromConfig, modelsItem})
		if tc.fail != nil {
			if err := tc.fail(role, modelsItem); err != nil {
				return 0, err
			}
		}
		if role == "orchestrator" {
			return 0, nil // a launcher reporting no PID
		}
		spawned++
		return 4000 + spawned, nil
	})
	defer restore()

	opts := commands.RepairAgentPoolOptions{ProjectRoot: project, Missing: true}
	if tc.options != nil {
		opts = tc.options(opts)
	}
	result, resultErr := commands.RepairAgentPool(opts)
	first := append([]spawnRequest(nil), requests...)

	requests, spawned = nil, 0
	output, renderErr := captureStdout(t, func() error { return commands.RepairAgentPoolCommand(opts) })
	if len(requests) != len(first) {
		t.Fatalf("the command's start requests differ from the entry's: %d against %d", len(requests), len(first))
	}
	for i := range first {
		if requests[i] != first[i] {
			t.Fatalf("start request %d differs between the entry and the command", i+1)
		}
	}

	var record strings.Builder
	fmt.Fprintf(&record, "--- requests\n")
	for i, request := range first {
		fmt.Fprintf(&record, "%d: %s\n", i+1, request)
	}
	if len(first) == 0 {
		record.WriteString("none\n")
	}
	record.WriteString("--- result\n")
	if result == nil {
		record.WriteString("nil\n")
	} else {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			t.Fatal("encode result")
		}
		record.Write(data)
		record.WriteString("\n")
	}
	fmt.Fprintf(&record, "--- error\n%s\n--- rendered output\n%s--- rendered error\n%s\n", errorText(resultErr), output, errorText(renderErr))
	e.norm.Check(t, oracle.Human, "repair-pool-spawn/"+tc.name+".txt", []byte(record.String()))
}

func errorText(err error) string {
	if err == nil {
		return "none"
	}
	return err.Error()
}

// isolateProcessEnvironment gives the in-process entry the environment the
// binary-driven oracle builds: a fresh HOME, the embedded provider catalog, no
// auto-update and none of the brand's own variables from the host (names only).
func isolateProcessEnvironment(t *testing.T, e *environment) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(name), "LIZA_") {
			t.Setenv(name, "")
			if err := os.Unsetenv(name); err != nil {
				t.Fatalf("unset %s", name)
			}
		}
	}
	t.Setenv("HOME", e.home)
	t.Setenv("LIZA_PROVIDER_CATALOG_URL", "http://catalog.invalid/")
	t.Setenv("LIZA_SKIP_AUTO_UPDATE", "1")
}

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what it
// printed with fn's error.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal("create stdout pipe")
	}
	saved := os.Stdout
	os.Stdout = writer
	captured := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- data
	}()
	runErr := func() error {
		defer func() { os.Stdout = saved }()
		return fn()
	}()
	if err := writer.Close(); err != nil {
		t.Fatal("close stdout pipe")
	}
	out := <-captured
	_ = reader.Close()
	return string(out), runErr
}
