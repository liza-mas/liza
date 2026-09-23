// Package gitenv provides locale-safe environment for git subprocesses.
package gitenv

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultCommandTimeout bounds git subprocesses owned by Liza. It is deliberately
// conservative: most git plumbing commands complete in milliseconds, while local
// rebases and large-tree operations may need substantially longer.
var DefaultCommandTimeout = 5 * time.Minute

// DefaultCommandWaitDelay bounds how long waits may continue after cancellation
// when child processes still hold stdout/stderr pipes open.
var DefaultCommandWaitDelay = 5 * time.Second

// TimeoutError reports a git command killed after exceeding its deadline.
type TimeoutError struct {
	Args    []string
	Dir     string
	Timeout time.Duration
	Output  []byte
}

func (e *TimeoutError) Error() string {
	if e.Dir != "" {
		return fmt.Sprintf("git %v timed out after %s in %s", e.Args, e.Timeout, e.Dir)
	}
	return fmt.Sprintf("git %v timed out after %s", e.Args, e.Timeout)
}

// Env returns the current environment with LC_ALL=C forced, so git output
// is always in English regardless of the system locale.
func Env() []string {
	env := os.Environ()
	for i, e := range env {
		if strings.HasPrefix(e, "LC_ALL=") {
			env[i] = "LC_ALL=C"
			return env
		}
	}
	return append(env, "LC_ALL=C")
}

// Command creates an exec.Cmd for git with LC_ALL=C forced.
func Command(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = Env()
	return cmd
}

// CombinedOutput runs git with the package default timeout and returns combined
// stdout/stderr. The timeout is applied to the subprocess, not to any caller-held
// locks; callers should still avoid invoking git while holding state locks.
func CombinedOutput(dir string, args ...string) ([]byte, error) {
	return CombinedOutputWithTimeout(DefaultCommandTimeout, dir, args...)
}

// CombinedOutputContext runs git with the package default timeout and aborts
// the subprocess when ctx is canceled.
func CombinedOutputContext(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return combinedOutputContext(ctx, DefaultCommandTimeout, dir, args...)
}

// CombinedOutputWithTimeout runs git with a caller-specified timeout. It exists
// primarily for tests and for operations with well-understood tighter bounds.
func CombinedOutputWithTimeout(timeout time.Duration, dir string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	return combinedOutputContext(context.Background(), timeout, dir, args...)
}

func combinedOutputContext(parent context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = Env()
	cmd.WaitDelay = DefaultCommandWaitDelay

	output, err := cmd.CombinedOutput()
	if err != nil && parent.Err() != nil {
		return output, parent.Err()
	}
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return output, &TimeoutError{
			Args:    append([]string(nil), args...),
			Dir:     dir,
			Timeout: timeout,
			Output:  append([]byte(nil), output...),
		}
	}
	return output, err
}

// Output runs git with the package default timeout and returns stdout only.
func Output(dir string, args ...string) ([]byte, error) {
	return OutputWithTimeout(DefaultCommandTimeout, dir, args...)
}

// OutputContext runs git with the package default timeout and aborts the
// subprocess when ctx is canceled.
func OutputContext(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return outputContext(ctx, DefaultCommandTimeout, dir, args...)
}

// OutputWithTimeout runs git with a caller-specified timeout and returns stdout
// only. Use this when command semantics depend on stdout rather than stderr.
func OutputWithTimeout(timeout time.Duration, dir string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	return outputContext(context.Background(), timeout, dir, args...)
}

// OutputWithStdin runs git with the package default timeout, feeds stdin to
// the subprocess, and returns stdout only. It serves one-shot batch queries
// (for example `git cat-file --batch-check`) that answer many questions in a
// single short-lived process instead of one process per question.
func OutputWithStdin(dir, stdin string, args ...string) ([]byte, error) {
	return outputStdinContext(context.Background(), DefaultCommandTimeout, dir, stdin, args...)
}

func outputContext(parent context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	return outputStdinContext(parent, timeout, dir, "", args...)
}

func outputStdinContext(parent context.Context, timeout time.Duration, dir, stdin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = Env()
	cmd.WaitDelay = DefaultCommandWaitDelay
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	output, err := cmd.Output()
	if err != nil && parent.Err() != nil {
		return output, parent.Err()
	}
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return output, &TimeoutError{
			Args:    append([]string(nil), args...),
			Dir:     dir,
			Timeout: timeout,
			Output:  append([]byte(nil), output...),
		}
	}
	return output, err
}
