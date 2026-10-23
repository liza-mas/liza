//go:build linux

package artifact

import (
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2ProjectArtifacts(t *testing.T) {
	e := isolateEnv(t, false)
	project := filepath.Join(e.work, "project")
	if err := writeProject(project); err != nil {
		t.Fatal(err)
	}
	var entries []fileEntry
	for _, tree := range []struct{ root, prefix string }{{project, "project/"}, {e.home, "home/"}} {
		for _, entry := range snapshot(t, tree.root) {
			entry.Path = tree.prefix + entry.Path
			entries = append(entries, entry)
		}
	}
	oracle.Check(t, oracle.Artifact, "project/written.manifest", manifest(entries, false))
	checkTree(t, "project", entries, map[string]string{project: "<PROJECT>", e.home: "<HOME>"})
}

func TestOracleA2HomeAndWorktreeHooks(t *testing.T) {
	e := isolateEnv(t, false)
	if err := writePiGate(); err != nil {
		t.Fatal("write home pi init gate")
	}
	entries := snapshot(t, e.home)
	if len(entries) != 1 {
		t.Fatalf("pi init gate wrote %d files, want 1", len(entries))
	}
	checkTree(t, "home-and-worktree/home", entries, map[string]string{e.home: "<HOME>"})
	oracle.Check(t, oracle.Artifact, "home-and-worktree/worktree-pre-commit", worktreePreCommit())
}
