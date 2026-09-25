package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

// capturedStopHookInput is a Stop hook payload recorded from Claude Code
// 2.1.280 in headless mode while a backgrounded shell job was still running.
const capturedStopHookInput = `{"session_id":"3e7dbcd6-ae6c-4d51-966d-211b117b9d04","transcript_path":"/home/u/.claude/projects/p/3e7dbcd6.jsonl","cwd":"/tmp/work","prompt_id":"b548602b-ce10-4567-b29c-77051feeaa52","permission_mode":"auto","effort":{"level":"high"},"hook_event_name":"Stop","stop_hook_active":false,"background_tasks":[{"id":"b0j9egxpq","type":"shell","status":"running","description":"Sleep 40 seconds then print finished","command":"sleep 40; echo finished"}],"session_crons":[]}`

func agentEnv(agentID string) func(string) string {
	return func(key string) string {
		if key == brand.EnvName("AGENT_ID") {
			return agentID
		}
		return ""
	}
}

func runStopGuard(t *testing.T, input, agentID string) (stopGuardDecision, bool) {
	t.Helper()
	var out bytes.Buffer
	if err := StopGuardCommand(strings.NewReader(input), &out, agentEnv(agentID)); err != nil {
		t.Fatalf("StopGuardCommand: %v", err)
	}
	if out.Len() == 0 {
		return stopGuardDecision{}, false
	}
	var decision stopGuardDecision
	if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
		t.Fatalf("decision is not JSON: %v\n%s", err, out.String())
	}
	return decision, true
}

func stopInput(t *testing.T, tasks ...map[string]any) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"hook_event_name": "Stop", "stop_hook_active": false, "background_tasks": tasks})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func task(id, status, command string) map[string]any {
	return map[string]any{"id": id, "type": "shell", "status": status, "command": command}
}

func TestStopGuard_BlocksCapturedRunningJobForAgent(t *testing.T) {
	t.Parallel()

	decision, blocked := runStopGuard(t, capturedStopHookInput, "coder-1")

	if !blocked || decision.Decision != "block" {
		t.Fatalf("want block decision, got %+v (blocked=%v)", decision, blocked)
	}
	for _, want := range []string{"b0j9egxpq", "sleep 40; echo finished", "AGENT_TOOLS.md"} {
		if !strings.Contains(decision.Reason, want) {
			t.Errorf("reason missing %q: %s", want, decision.Reason)
		}
	}
}

func TestStopGuard_IgnoresSessionsWithoutAgentID(t *testing.T) {
	t.Parallel()

	if decision, blocked := runStopGuard(t, capturedStopHookInput, ""); blocked {
		t.Fatalf("pairing session must not be guarded, got %+v", decision)
	}
}

func TestStopGuard_AllowsStopWithoutRunningJobs(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty list":       stopInput(t),
		"finished jobs":    stopInput(t, task("a", "completed", "make test"), task("b", "failed", "go vet ./..."), task("c", "stopped", "sleep 1")),
		"no list":          `{"hook_event_name":"Stop","stop_hook_active":true}`,
		"null list":        `{"hook_event_name":"Stop","background_tasks":null}`,
		"reordered fields": `{"background_tasks":[{"command":"make test","status":"completed","type":"shell","id":"a"}]}`,
	}
	for name, input := range cases {
		if decision, blocked := runStopGuard(t, input, "coder-1"); blocked {
			t.Errorf("%s: want no block, got %+v", name, decision)
		}
	}
}

func TestStopGuard_ReadsReorderedFieldsAndEscapedCommands(t *testing.T) {
	t.Parallel()

	command := `bash -c 'echo "{\"k\": [1]}" && pytest -k "a or b"'`
	input := `{"background_tasks":[{"command":` + mustJSON(t, command) + `,"status":"running","id":"j1","type":"shell"}]}`

	decision, blocked := runStopGuard(t, input, "coder-1")

	if !blocked {
		t.Fatal("want block for running job with reordered fields")
	}
	if !strings.Contains(decision.Reason, "j1") || !strings.Contains(decision.Reason, `pytest -k "a or b"`) {
		t.Errorf("reason does not name the job and its command: %s", decision.Reason)
	}
}

func TestStopGuard_ExemptsOnlyPlainAwaitCommands(t *testing.T) {
	t.Parallel()
	bin := brand.RuntimeValues().BinaryName

	exempt := []string{
		bin + " await-verdict task-1",
		bin + " -C /repo await-resubmission task-1 --json",
		"/usr/local/bin/" + bin + " --project-root=/repo -v await-verdict task-1",
		bin + ` -C "/tmp/repo with spaces" await-verdict task-1`,
		`"/opt/my tools/` + bin + `" -C '/repo' await-resubmission task-1`,
	}
	for _, command := range exempt {
		if decision, blocked := runStopGuard(t, stopInput(t, task("w", "running", command)), "coder-1"); blocked {
			t.Errorf("await command %q must be exempt, got %+v", command, decision)
		}
	}

	notExempt := []string{
		"echo " + bin + " await-verdict",
		bin + " await-verdict task-1; make test",
		bin + " await-verdict task-1 && " + bin + " submit-for-review task-1",
		bin + " -C \"$ROOT\" await-verdict task-1",
		bin + " submit-for-review task-1",
		"other-" + bin + " await-verdict task-1",
		bin + " -C",
		bin + ` -C "/tmp/repo await-verdict work" submit-for-review task-1`,
		bin + ` -C '/tmp/repo await-verdict' submit-for-review task-1`,
		bin + ` -C "/tmp/repo await-verdict task-1`,
		bin + " -C /tmp/* await-verdict task-1",
		bin + " \"await-verdict task-1\"",
	}
	for _, command := range notExempt {
		if _, blocked := runStopGuard(t, stopInput(t, task("g", "running", command)), "coder-1"); !blocked {
			t.Errorf("command %q merely mentions await and must not be exempt", command)
		}
	}
}

func TestStopGuard_MixedAwaitAndValidationJobsBlockOnValidation(t *testing.T) {
	t.Parallel()
	bin := brand.RuntimeValues().BinaryName

	input := stopInput(t,
		task("await", "running", bin+" await-verdict task-1"),
		task("gate", "running", bin+" submit-for-review task-1"),
		task("done", "completed", "make lint"),
	)

	decision, blocked := runStopGuard(t, input, "coder-1")

	if !blocked {
		t.Fatal("want block while the submit gate runs")
	}
	if !strings.Contains(decision.Reason, "gate") || strings.Contains(decision.Reason, "await (") || strings.Contains(decision.Reason, "done (") {
		t.Errorf("reason must name only the live non-await job: %s", decision.Reason)
	}
}

func TestStopGuard_FailsClosedOnUnreadableInput(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"invalid JSON":                           `{"background_tasks":[`,
		"list not an array":                      `{"background_tasks":{"id":"x","status":"running"}}`,
		"entry not an object":                    `{"background_tasks":["x"]}`,
		"entry without status":                   `{"background_tasks":[{"id":"x","command":"make test"}]}`,
		"entry without id":                       `{"background_tasks":[{"status":"completed","command":"make test"}]}`,
		"status of the wrong type":               `{"background_tasks":[{"id":"x","status":1}]}`,
		"unrecognised entry beside finished job": `{"background_tasks":[{"id":"a","status":"completed"},{"job":"b"}]}`,
	}
	for name, input := range cases {
		decision, blocked := runStopGuard(t, input, "coder-1")
		if !blocked || !strings.Contains(decision.Reason, "could not read") {
			t.Errorf("%s: want fail-closed block, got %+v (blocked=%v)", name, decision, blocked)
		}
	}
}

func TestStopGuard_TruncatesLongCommandsOnRuneBoundary(t *testing.T) {
	t.Parallel()

	command := strings.Repeat("é", stopGuardCommandLimit+50)

	decision, blocked := runStopGuard(t, stopInput(t, task("long", "running", command)), "coder-1")

	if !blocked {
		t.Fatal("want block")
	}
	if strings.Contains(decision.Reason, strings.Repeat("é", stopGuardCommandLimit+1)) {
		t.Errorf("command not truncated: %d bytes", len(decision.Reason))
	}
	if !strings.Contains(decision.Reason, strings.Repeat("é", stopGuardCommandLimit)+"…") {
		t.Errorf("truncation did not keep %d whole runes", stopGuardCommandLimit)
	}
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
