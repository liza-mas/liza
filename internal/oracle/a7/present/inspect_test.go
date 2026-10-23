package present

import (
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleInspect(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "get", []invocation{
		{"tasks.txt", oracle.Human, []string{"get", "tasks"}},
		{"tasks-all.txt", oracle.Human, []string{"get", "tasks", "--all"}},
		{"tasks-format-table.txt", oracle.Human, []string{"get", "tasks", "--format", "table"}},
		{"tasks-format-yaml.txt", oracle.Human, []string{"get", "tasks", "--format", "yaml"}},
		{"tasks-json.json", oracle.JSON, []string{"get", "tasks", "--json"}},
		{"tasks-active-summary-json.json", oracle.JSON, []string{"get", "tasks", "--active", "--summary", "--json"}},
		{"task-output-summary-json.json", oracle.JSON, []string{"get", "task-executing", "--output-summary", "--json"}},
		{"tasks-fields-json.json", oracle.JSON, []string{"get", "tasks", "--field", "id,status", "--json"}},
		{"tasks-id.txt", oracle.Human, []string{"get", "tasks", "task-executing"}},
		{"tasks-id-json.json", oracle.JSON, []string{"get", "tasks", "task-executing", "--json"}},
		{"task-history-json.json", oracle.JSON, []string{"get", "tasks.task-executing.history", "--json"}},
		{"task-rejection-reason.txt", oracle.Human, []string{"get", "task-executing.rejection_reason"}},
		{"agents.txt", oracle.Human, []string{"get", "agents"}},
		{"agents-json.json", oracle.JSON, []string{"get", "agents", "--json"}},
		{"agents-format-yaml.txt", oracle.Human, []string{"get", "agents", "--format", "yaml"}},
		{"agents-id.txt", oracle.Human, []string{"get", "agents", "coder-1"}},
		{"metrics.txt", oracle.Human, []string{"get", "metrics"}},
		{"anomalies.txt", oracle.Human, []string{"get", "anomalies"}},
		{"human-notes-json.json", oracle.JSON, []string{"get", "human_notes", "--json"}},
		{"quarantined-verdicts-json.json", oracle.JSON, []string{"get", "quarantined_verdicts", "--json"}},
		{"config-mode.txt", oracle.Human, []string{"get", "config.mode"}},
		{"config-mode-json.json", oracle.JSON, []string{"get", "config.mode", "--json"}},
		{"sprint-tasks-done.txt", oracle.Human, []string{"get", "sprint.metrics.tasks_done"}},
		{"task-shorthand.txt", oracle.Human, []string{"get", "task-executing"}},
		{"agent-shorthand.txt", oracle.Human, []string{"get", "coder-1"}},
		{"refusal-unknown-id-json.json", oracle.JSON, []string{"get", "task-unknown", "--json"}},
		{"refusal-unknown-id.txt", oracle.Human, []string{"get", "task-unknown"}},
		{"refusal-field-arity-json.json", oracle.JSON, []string{"get", "config.mode", "extra", "--json"}},
	})
	t.Run("sprint-elapsed", func(t *testing.T) {
		e := newEnvironment(t)
		result := runCLI(t, e, materialiseFixture(t, e), nil, "get", "sprint.elapsed")
		// This query's value is a relative clock without a printed label. Only
		// its stdout is registered; exit and stderr remain asserted in full.
		e.norm.Literal(string(result.stdout), "<REL>\n")
		checkResult(t, e, oracle.Human, "get/sprint-elapsed.txt", result)
	})
}

func TestOracleInspectGetTasks(t *testing.T) {
	skipUnlessLinux(t)
	checkInvocations(t, "get-tasks", []invocation{
		{"default.txt", oracle.Human, []string{"get-tasks"}},
		{"id.txt", oracle.Human, []string{"get-tasks", "task-executing"}},
		{"json.json", oracle.JSON, []string{"get-tasks", "--json"}},
	})
}
