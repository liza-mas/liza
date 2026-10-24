package a4

import "testing"

// AC-budget-2: command-0 wall time is recorded in acceptance-a4-2.json after
// measuring the complete suite, including fixture validation and the CLI build.
func TestOracleRetargetDependency(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t2", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "cannot retarget dependencies on terminal task m1 (MERGED)"
			}
			runCase(t, oracleCase{name: "retarget-dependency/" + name, fixture: "graph", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"retarget-dependency", id, "t1", "t3", "--reason", "Moved"}})
		})
	}
}

func TestOracleNarrowInheritedDependencies(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "cp-b", 0, ""
			if name == "refused" {
				id, code, message = "t1", 1, "producer t1 has no executed per-subtask transition; pass --transition explicitly"
			}
			runCase(t, oracleCase{name: "narrow-inherited-dependencies/" + name, fixture: "inherited", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"narrow-inherited-dependencies", id, "--selections", "@input:narrow.yaml", "--reason", "Only first output"}})
		})
	}
}

func TestOracleApplyDependencyRepair(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "b1", 0, ""
			if name == "refused" {
				id, code, message = "t1", 1, "cannot apply dependency repair from task t1 in status DRAFT_CODE (must be BLOCKED)"
			}
			runCase(t, oracleCase{name: "apply-dependency-repair/" + name, fixture: "repair-request", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"apply-dependency-repair", id, "--reason", "Apply"}})
		})
	}
}

func TestOracleRepairSupersededDependencies(t *testing.T) {
	t.Run("success", runD35Success)
	t.Run("refused", func(t *testing.T) {
		runCase(t, oracleCase{name: "repair-superseded-dependencies/refused", fixture: "graph", identity: "orch", json: true, wantExit: 1, contains: "cannot repair dependencies on task t1 in status DRAFT_CODE (must be SUPERSEDED)", args: []string{"repair-superseded-dependencies", "t1", "--reason", "Repair"}})
	})
}

func TestOracleDeferProviderDependency(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "cp-x", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "task m1 is not unstarted"
			}
			runCase(t, oracleCase{name: "defer-provider-dependency/" + name, fixture: "provider-wait", identity: "op", json: true, wantExit: code, contains: message, args: []string{"defer-provider-dependency", id, "--provider-task", "cp-p", "--transition", "code-plan-to-coding", "--reason", "Hold only writers"}})
		})
	}
}

func TestOracleReserveProvider(t *testing.T) {
	for _, name := range []string{"success", "refused"} {
		t.Run(name, func(t *testing.T) {
			id, code, message := "t1", 0, ""
			if name == "refused" {
				id, code, message = "m1", 1, "writer m1 is terminal (MERGED); there is nothing left to place"
			}
			runCase(t, oracleCase{name: "reserve-provider/" + name, fixture: "provider", identity: "orch", json: true, wantExit: code, contains: message, args: []string{"reserve-provider", id, "cp-p", "--transition", "code-plan-to-coding", "--reason", "Writer order"}})
		})
	}
}
