//go:build oracleproof

package a4

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestA4RegenerationRejectsAlteredGolden(t *testing.T) {
	gate(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: os.Getenv("HOME"), Path: os.Getenv("PATH"), Tmp: tmp, Go: true}, "GIT_CONFIG_NOSYSTEM=1")
	if err != nil {
		t.Fatal(err)
	}
	shaResult := run(t, root, env, nil, "git", "rev-parse", "HEAD")
	if shaResult.code != 0 {
		t.Fatal("resolve proof commit")
	}
	clone := filepath.Join(tmp, "clone")
	r := run(t, tmp, env, nil, "git", "clone", "--no-hardlinks", "--no-checkout", root, clone)
	if r.code != 0 {
		t.Fatal("clone proof repository")
	}
	r = run(t, clone, env, nil, "git", "checkout", "--detach", strings.TrimSpace(string(shaResult.stdout)))
	if r.code != 0 {
		t.Fatal("checkout proof commit")
	}
	const golden = "internal/oracle/a4/testdata/golden/json/add-task/success.json"
	path := filepath.Join(clone, golden)
	b := readFile(t, path)
	old, newValue := []byte(`"exit_code": 0`), []byte(`"exit_code": 1`)
	if bytes.Count(b, old) != 1 {
		t.Fatal("altered-golden proof requires exactly one success exit code")
	}
	writeFile(t, path, bytes.Replace(b, old, newValue, 1))
	r = run(t, clone, env, nil, "go", "run", "./internal/testhelpers/oracle/cmd/regen", "-repo", clone, "-pkg", "./internal/oracle/a4/...")
	if r.code == 0 || !bytes.Contains(append(r.stdout, r.stderr...), []byte(golden)) {
		t.Fatalf("regeneration did not reject and name altered golden: exit %d; %s %s", r.code, r.stdout, r.stderr)
	}
}
