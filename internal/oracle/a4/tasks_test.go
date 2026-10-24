package a4

import (
	"strings"
	"testing"
)

func TestOracleAddTask(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t2", 0, ""
			if name == "refused" {
				id, code, message = "t1", 1, "task 't1' already exists"
			}
			runCase(t, oracleCase{name: "add-task/" + name, fixture: "base", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"add-task", "--id", id, "--desc", "D", "--done", "W", "--scope", "S", "--spec", "specs/vision.md", "--role-pair", "coding-pair"}})
		})
	}
}

func TestOracleAddTasks(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			file, code, message := "add-tasks.json", 0, ""
			if name == "refused" {
				file, code, message = "add-tasks-empty.json", 1, "at least one task is required"
			}
			runCase(t, oracleCase{name: "add-tasks/" + name, fixture: "base", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"add-tasks", "--tasks-file", "@input:" + file}})
		})
	}
}

func TestOracleSetTaskOutput(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code := "arch-1", 0
			if name == "refused" {
				id, code = "arch-2", 1
			}
			runCase(t, oracleCase{name: "set-task-output/" + name, fixture: "architecting", identity: "arch", json: true, wantExit: code, args: []string{"set-task-output", id, "--output", "@input:task-output.json", "--agent-id", "architect-1"}})
		})
	}
}

func TestOracleReplaceTask(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			token, code := "@transition:t1", 0
			if name == "refused" {
				token, code = strings.Repeat("0", 64), 1
			}
			runCase(t, oracleCase{name: "replace-task/" + name, fixture: "graph", identity: "orch", json: true, wantExit: code, args: []string{"replace-task", "--replacement-file", "@input:replacement.json", "--request-id", "replace-1", "--expected-transition", token}})
		})
	}
}

func TestOracleSupersedeTask(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code := "t1", 0
			if name == "refused" {
				id, code = "m1", 1
			}
			runCase(t, oracleCase{name: "supersede-task/" + name, fixture: "graph", identity: "orch", json: true, wantExit: code, args: []string{"supersede-task", id, "t3", "--reason", "Rescoped"}})
		})
	}
}

func TestOracleCancelTask(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code := "t3", 0
			if name == "refused" {
				id, code = "m1", 1
			}
			runCase(t, oracleCase{name: "cancel-task/" + name, fixture: "graph", identity: "orch", json: true, wantExit: code, args: []string{"cancel-task", id, "No longer needed"}})
		})
	}
}

func TestOracleDeleteTask(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t3", 0, "Deleted task t3 (was DRAFT_CODE)"
			if name == "refused" {
				id, code, message = "m1", 1, "cannot delete MERGED task m1"
			}
			runCase(t, oracleCase{name: "delete-task/" + name, fixture: "graph", identity: "orch", wantExit: code, contains: message, args: []string{"delete", "task", id}})
		})
	}
}
