package a4

import (
	"bytes"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestOracleArchiveBatches pins the batch loop and the limit refusals that the
// S7/S8 success cases leave open: one default-limit batch and one restore of an
// already archived fixture. The fixture holds one live task and three terminal
// ones, so each limit has a distinct, countable effect on the batch total.
func TestOracleArchiveBatches(t *testing.T) {
	for _, c := range []oracleCase{
		{name: "archive-terminal-tasks/batches_by_tasks", fixture: "archive-batches", identity: "op", json: true,
			args: []string{"archive-terminal-tasks", "--max-tasks", "1"}},
		// The first task of a batch is always admitted, so a one-byte budget
		// still archives one task per batch.
		{name: "archive-terminal-tasks/batches_by_bytes", fixture: "archive-batches", identity: "op", json: true,
			args: []string{"archive-terminal-tasks", "--max-bytes", "1"}},
		// JSON mode reports these flag errors by code only; the human golden pins
		// the message that names the refusal.
		{name: "archive-terminal-tasks/limits_refused", fixture: "archive-batches", identity: "op", json: true, wantExit: 1,
			args: []string{"archive-terminal-tasks", "--max-tasks", "0"}},
		{name: "archive-terminal-tasks/restore_limits_refused", fixture: "archived", identity: "op", json: true, wantExit: 1,
			args: []string{"archive-terminal-tasks", "--restore-inline", "--max-tasks", "1"}},
	} {
		t.Run(c.name[len("archive-terminal-tasks/"):], func(t *testing.T) { runCase(t, c) })
	}
	t.Run("restore_after_batches", func(t *testing.T) { runArchiveThenRestore(t, "archive-terminal-tasks/restore_after_batches") })
}

// runArchiveThenRestore archives in three one-task batches, then restores
// inline. oracleCase runs one command per fixture, so the sequence composes the
// shared builders; the state golden spans both commands, which records the
// archive objects as written and retained.
func runArchiveThenRestore(t *testing.T, name string) {
	t.Helper()
	gate(t)
	var jsonState []byte
	for _, mode := range []string{"json", "human"} {
		f := newFixture(t, "archive-batches")
		before := readFile(t, f.statePath())
		snapshot := projectFiles(t, f.root)
		archived := f.cli(t, "op", "archive-terminal-tasks", "--max-tasks", "1", "--json")
		if archived.code != 0 || !strings.Contains(string(archived.stdout), `"batches":3`) {
			t.Fatalf("%s archival leg did not run three batches: exit %d %s %s", mode, archived.code, archived.stdout, archived.stderr)
		}
		args := []string{"archive-terminal-tasks", "--restore-inline"}
		if mode == "json" {
			args = append(args, "--json")
		}
		res := f.cli(t, "op", args...)
		res.archive = true
		if res.code != 0 {
			t.Fatalf("%s restore exit %d: %s %s", mode, res.code, f.normaliser().text(string(res.stdout)), f.normaliser().text(string(res.stderr)))
		}
		if mode == "json" {
			f.harness().Check(t, oracle.JSON, name+".json", jsonDocument(t, f, res))
		} else {
			f.harness().Check(t, oracle.Human, name+".txt", humanDocument(t, f, res))
		}
		b, a := stateDocument(t, f, before, snapshot)
		if mode == "human" {
			if !bytes.Equal(jsonState, a) {
				t.Fatalf("json/human final state differs for %s:\njson:\n%s\nhuman:\n%s", name, jsonState, a)
			}
		} else {
			f.harness().CheckState(t, name+".txt", b, a)
			jsonState = a
		}
	}
}
