package a4

import "testing"

func TestOracleArchiveTerminalTasks(t *testing.T) {
	for _, c := range []oracleCase{
		// S7 has no principal state precondition refusal below discovery: the
		// operator guard is O1, invalid limits are flag errors, and no work succeeds.
		{name: "archive-terminal-tasks/success", fixture: "metrics", identity: "op", args: []string{"archive-terminal-tasks"}, json: true},
		// S8 likewise succeeds with nothing to restore; the guard is O1 and the
		// batch-limit combination is a flag error. Keep success-only per Case Table.
		{name: "archive-terminal-tasks/restore", fixture: "archived", identity: "op", args: []string{"archive-terminal-tasks", "--restore-inline"}, json: true},
	} {
		t.Run(c.name[len("archive-terminal-tasks/"):], func(t *testing.T) { runCase(t, c) })
	}
}
