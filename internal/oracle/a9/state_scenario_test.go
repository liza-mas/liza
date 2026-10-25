package a9

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// newStateScenario is newScenario for scenarios that call an exported ops entry
// point on disposable state and Git, with no CLI, process or PATH stub. It
// builds no binary and links nothing, so unlike newScenario it carries no
// platform skip (D53); the golden comparison is the same.
func newStateScenario(t *testing.T, name string) *scenario {
	t.Helper()
	p := &scenario{name: name, root: t.TempDir()}
	p.dir = filepath.Join(p.root, "project")
	p.home = filepath.Join(p.root, "home")
	p.tmp = filepath.Join(p.root, "tmp")
	p.recordDir = filepath.Join(p.root, "records")
	for _, dir := range []string{p.dir, p.home, p.tmp, p.recordDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal("A9 setup: create scenario directory")
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("A9 setup: git prerequisite unavailable")
	}
	p.gitBin = git
	p.env, err = oracle.ChildEnv(oracle.EnvBase{Home: p.home, Path: filepath.Dir(git), Tmp: p.tmp}, scenarioExtras...)
	if err != nil {
		t.Fatal(err)
	}
	p.seed(t, name)
	p.snapshotBefore(t)
	return p
}
