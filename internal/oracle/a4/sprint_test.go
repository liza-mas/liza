package a4

import "testing"

func TestOracleSprintCheckpoint(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "sprint-checkpoint/success", fixture: "base", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, contains: "sprint_summary.md"},
		// The well-formed CHECKPOINT fixture reaches ErrSprintAlreadyCheckpoint.
		// JSON deliberately records internal/internal error; human mode names it.
		{name: "sprint-checkpoint/refused", fixture: "sprint-at-checkpoint", identity: "orch", args: []string{"sprint-checkpoint"}, json: true, wantExit: 1},
	} {
		t.Run(c.name[len("sprint-checkpoint/"):], func(t *testing.T) { runCase(t, c) })
	}
}

func TestOraclePassedPlanCheckpoint(t *testing.T) {
	t.Run("checkpoints", func(t *testing.T) {
		runCase(t, oracleCase{name: "passed-plan-checkpoint/checkpoints", fixture: "plan-passed", helper: "passed-plan-checkpoint", summary: "plans: [cp-1]"})
	})
	t.Run("none", func(t *testing.T) {
		// Base has no planning task, so the admission returns its no-work sentinel.
		runCase(t, oracleCase{name: "passed-plan-checkpoint/none", fixture: "base", helper: "passed-plan-checkpoint", summary: "no passed plan awaits a checkpoint"})
	})
}

func TestOracleUpdateSprintMetrics(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		runCase(t, oracleCase{name: "update-sprint-metrics/success", fixture: "metrics", identity: "orch", args: []string{"update-sprint-metrics"}, json: true, contains: "suspiciously high"})
	})
}

func TestOracleAnalyze(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "analyze/ok", fixture: "base", identity: "orch", args: []string{"analyze"}, json: true},
		{name: "analyze/triggered", fixture: "retry-cluster", identity: "orch", args: []string{"analyze"}, json: true, contains: "retry_cluster"},
	} {
		t.Run(c.name[len("analyze/"):], func(t *testing.T) { runCase(t, c) })
	}
}

func TestOracleAddHumanNote(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "add-human-note/success", fixture: "base", identity: "op", args: []string{"add-human-note", "t1", "--note-file", "@input:note.txt"}, json: true, contains: "t1"},
		// Same valid note and operator identity, only the target is absent. Without
		// the target-existence refusal this would append a note and exit zero.
		{name: "add-human-note/refused", fixture: "base", identity: "op", args: []string{"add-human-note", "t9", "--note-file", "@input:note.txt"}, json: true, wantExit: 1},
	} {
		t.Run(c.name[len("add-human-note/"):], func(t *testing.T) { runCase(t, c) })
	}
}

func TestOracleSetDiscoveryDisposition(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "set-discovery-disposition/success", fixture: "discovery", identity: "orch", args: []string{"set-discovery-disposition", "disc-1", "deferred"}, json: true},
		// disc-1 exists; only the syntactically valid task reference t9 is missing.
		{name: "set-discovery-disposition/refused", fixture: "discovery", identity: "orch", args: []string{"set-discovery-disposition", "disc-1", "t9"}, json: true, wantExit: 1, contains: "which does not exist"},
	} {
		t.Run(c.name[len("set-discovery-disposition/"):], func(t *testing.T) { runCase(t, c) })
	}
}
