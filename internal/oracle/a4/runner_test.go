package a4

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type runResult struct {
	stdout, stderr []byte
	code           int
	archive        bool
}

func (f *fixture) environment(t testing.TB, identity string, extra ...string) []string {
	t.Helper()
	pairs := []string{"TZ=Etc/GMT-2", "GIT_CONFIG_NOSYSTEM=1"}
	switch identity {
	case "op":
	case "orch":
		pairs = append(pairs, "LIZA_AGENT_ID=orchestrator-1", "LIZA_AGENT_GENERATION=gen-1")
	case "arch":
		pairs = append(pairs, "LIZA_AGENT_ID=architect-1", "LIZA_AGENT_GENERATION=gen-a1")
	default:
		t.Fatal("unknown fixture identity class")
	}
	pairs = append(pairs, extra...)
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: os.Getenv("PATH"), Tmp: f.tmp}, pairs...)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func run(t testing.TB, dir string, env []string, stdin []byte, bin string, args ...string) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, bytes.NewReader(stdin)
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	r := runResult{stdout: out.Bytes(), stderr: errout.Bytes()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		r.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal("A4 child failed to start")
	}
	if ctx.Err() != nil {
		t.Fatal("A4 child exceeded two-minute timeout")
	}
	return r
}

func (f *fixture) cli(t testing.TB, identity string, args ...string) runResult {
	t.Helper()
	return run(t, f.root, f.environment(t, identity), nil, oracle.BuildCLI(t, "./cmd/liza"), args...)
}

func (f *fixture) helper(t testing.TB, op, identity string) runResult {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return run(t, f.root, f.environment(t, identity), nil, bin, "a4-helper", op, f.root)
}
