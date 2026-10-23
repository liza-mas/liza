//go:build linux

package artifact

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2IndexActivation(t *testing.T) {
	e := isolateEnv(t, true)
	repo := initRepo(t, e)
	for _, name := range []string{"go.mod", "main.go"} {
		writeFile(t, repo, name, readFile(t, "testdata/fixtures/gomodule", name), 0o644)
	}
	gitRun(t, e, repo, "add", "go.mod", "main.go")
	gitRun(t, e, repo, "commit", "-q", "-m", "fixture module")
	gitDir := filepath.Join(repo, ".git")
	before := make(map[string]fileEntry)
	for _, entry := range snapshot(t, gitDir) {
		before[entry.Path] = entry
	}
	if err := activateIndexes(repo); err != nil {
		t.Fatal("plan and install index activation")
	}
	var installed []fileEntry
	for _, entry := range snapshot(t, gitDir) {
		old, exists := before[entry.Path]
		if !exists || old.Mode != entry.Mode || !bytes.Equal(old.Content, entry.Content) {
			installed = append(installed, entry)
		}
	}
	oracle.Check(t, oracle.Artifact, "index-activation/installed.manifest", manifest(installed, false))
	checkTree(t, "index-activation/git", installed, map[string]string{repo: "<REPO>"})
}

func TestOracleA2IndexActivationCollision(t *testing.T) {
	e := isolateEnv(t, true)
	repo := initRepo(t, e)
	foreign := readFile(t, "testdata/fixtures", "foreign-post-commit")
	writeFile(t, repo, ".git/hooks/post-commit", foreign, 0o755)
	err := activateCollision(repo)
	if err == nil {
		t.Fatal("index activation accepted a foreign post-commit hook")
	}
	after := readFile(t, repo, ".git/hooks/post-commit")
	if !bytes.Equal(after, foreign) {
		t.Fatal("index activation changed the foreign post-commit hook")
	}
	oracle.Check(t, oracle.Artifact, "index-activation-collision/refusal.txt", tokenise([]byte(err.Error()+"\n"), map[string]string{repo: "<REPO>"}))
	oracle.Check(t, oracle.Artifact, "index-activation-collision/post-commit", after)
}
