package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// invoke retains every binary invocation, including prerequisites, as distinct
// labelled streams. Expected exit assertions prevent a failed prerequisite from
// becoming the oracle for an unreachable action boundary.
func (f *fixture) invoke(t *testing.T, label, stdin string, omitStderr bool, expected int, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, oracle.BuildCLI(t, "./cmd/liza"), args...)
	cmd.Dir, cmd.Env = f.project, f.env
	cmd.Stdin = bytes.NewBufferString(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	exit := 0
	if err := cmd.Run(); err != nil {
		var failure *exec.ExitError
		if !errors.As(err, &failure) || ctx.Err() != nil {
			t.Fatalf("invocation %s failed to complete", label)
		}
		exit = failure.ExitCode()
	}
	fmt.Fprintf(&f.human, "--- invocation %s\nexit %d\n--- stdout\n%s\n", label, exit, stdout.Bytes())
	if omitStderr {
		f.human.WriteString("--- stderr omitted (AC5)\n")
	} else {
		fmt.Fprintf(&f.human, "--- stderr\n%s\n", stderr.Bytes())
	}
	if exit != expected {
		t.Fatalf("invocation %s exit %d; want %d", label, exit, expected)
	}
}

func (f *fixture) checkHuman(t *testing.T, name string) {
	t.Helper()
	data := []byte(f.human.String())
	f.normaliser(data).Check(t, oracle.Human, name+".txt", data)
}

func (f *fixture) checkState(t *testing.T, name string, before, after []byte) {
	t.Helper()
	f.normaliser(before, after).CheckState(t, name+".state", before, after)
}
