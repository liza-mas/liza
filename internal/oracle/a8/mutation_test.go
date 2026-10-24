package a8

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOraclePromptMissingGolden(t *testing.T) {
	if os.Getenv("A8_MISSING_HELPER") == "1" {
		oracle.Check(t, oracle.Prompt, "missing.txt", []byte("fixture prompt"))
		return
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOraclePromptMissingGolden$")
	cmd.Dir = dir
	cmd.Env = childEnvironment(t, oracle.EnvBase{Home: dir, Path: os.Getenv("PATH"), Tmp: dir}, "A8_MISSING_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("golden "+filepath.FromSlash("testdata/golden/prompt/missing.txt")+" (kind prompt) is missing")) {
		t.Fatal("missing-golden refusal was not observed through the shared harness")
	}
	if _, err := os.Stat(filepath.Join(dir, "testdata")); !os.IsNotExist(err) {
		t.Fatal("live missing-golden comparison created files")
	}
}

// This opt-in experiment changes exactly one byte in a disposable committed
// candidate clone. Both real comparators must reject T01 specifically; setup
// errors, compile errors and unrelated test failures cannot satisfy the proof.
func TestOraclePromptAlteredGolden(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("prompt goldens embed POSIX native paths")
	}
	if os.Getenv("A8_ORACLE_MUTATION_CHECK") != "1" {
		t.Skip("opt-in starting-tree regeneration experiment")
	}
	repo, err := os.Getwd()
	if err != nil {
		t.Fatal("locate candidate checkout")
	}
	for {
		if _, err := os.Stat(filepath.Join(repo, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(repo)
		if parent == repo {
			t.Fatal("candidate go.mod missing")
		}
		repo = parent
	}
	root := t.TempDir()
	env := childEnvironment(t, oracle.EnvBase{Home: root, Path: os.Getenv("PATH"), Tmp: root}, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"))
	writeFile(t, filepath.Join(root, "gitconfig"), nil, 0o644)
	sha := strings.TrimSpace(gitCommand(t, repo, env, "rev-parse", "HEAD"))
	clone := filepath.Join(root, "candidate")
	gitCommand(t, root, env, "clone", "--shared", "--quiet", "--no-checkout", repo, clone)
	gitCommand(t, clone, env, "checkout", "--quiet", "--detach", sha)
	goEnv := childEnvironment(t, oracle.EnvBase{Home: root, Path: os.Getenv("PATH"), Tmp: root, Go: true}, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(root, "gitconfig"))
	sync := exec.Command("go", "run", "./internal/brandrender/cmd/sync-embedded", "--repo-root", clone)
	sync.Dir, sync.Env = clone, goEnv
	if err := sync.Run(); err != nil {
		t.Fatal("mutation clone embedded sync failed")
	}
	path := filepath.Join(clone, "internal/oracle/a8", goldenPath("T01"))
	data := readFile(t, path)
	if len(data) == 0 {
		t.Fatal("T01 golden is empty")
	}
	data[0] ^= 1
	writeFile(t, path, data, 0o644)
	proofs := []struct {
		name       string
		args       []string
		diagnostic string
	}{
		{"live", []string{"test", "-count=1", "-run", "^TestOraclePrompt$", "./internal/oracle/a8/..."}, "golden testdata/golden/prompt/T01.txt (kind prompt) differs at line"},
		{"regen", []string{"run", "./internal/testhelpers/oracle/cmd/regen", "-pkg", "./internal/oracle/a8/..."}, "unexplained internal/oracle/a8/testdata/golden/prompt/T01.txt"},
	}
	for _, proof := range proofs {
		cmd := exec.Command("go", proof.args...)
		cmd.Dir, cmd.Env = clone, goEnv
		output, err := cmd.CombinedOutput()
		if err == nil || !bytes.Contains(output, []byte(proof.diagnostic)) {
			t.Fatalf("%s did not reject the altered T01 golden for its byte mismatch", proof.name)
		}
		t.Logf("%s rejected the one-byte change in internal/oracle/a8/testdata/golden/prompt/T01.txt", proof.name)
	}
}
