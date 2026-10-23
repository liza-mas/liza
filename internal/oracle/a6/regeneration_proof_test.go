//go:build oracleproof

package a6

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestA6RegenerationRejectsAlteredGolden(t *testing.T) {
	platformGate(t)
	const golden = "internal/oracle/a6/testdata/golden/state/WtMerge/success.yaml"
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, golden))
	if err != nil {
		t.Fatal(err)
	}
	f := baseFixture(t)
	clone := filepath.Join(f.tmp, "clone")
	f.git(t, "clone", "--no-hardlinks", root, clone)
	altered := append([]byte{}, original...)
	if len(altered) == 0 {
		t.Fatal("provenance probe needs a nonempty golden")
	}
	altered[0] ^= 1
	if err := os.WriteFile(filepath.Join(clone, golden), altered, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./internal/testhelpers/oracle/cmd/regen", "-mode", "check", "-pkg", "./internal/oracle/a6/...")
	cmd.Dir = clone
	cmd.Env, err = oracle.ChildEnv(oracle.EnvBase{Home: os.Getenv("HOME"), Path: os.Getenv("PATH"), Tmp: f.tmp, Go: true})
	if err != nil {
		t.Fatal(err)
	}
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("altered-golden check timed out")
	}
	if runErr == nil || !bytes.Contains(output, []byte("unexplained "+golden)) {
		t.Fatalf("altered golden must fail naming %s: err=%v\n%s", golden, runErr, output)
	}
	current, err := os.ReadFile(filepath.Join(root, golden))
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("provenance probe modified source golden")
	}
}
