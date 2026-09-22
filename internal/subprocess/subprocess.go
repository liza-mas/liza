// Package subprocess provides bounded execution for Liza-owned external tools.
package subprocess

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultCommandTimeout bounds advisory runtime tool calls such as repository
// index refreshes. These calls should improve agent context, not block lifecycle
// state transitions indefinitely.
var DefaultCommandTimeout = 90 * time.Second

// DefaultWaitDelay bounds how long Wait may continue after cancellation when a
// child process still holds stdout/stderr pipes open.
var DefaultWaitDelay = 5 * time.Second

// ConfigureCancellation gives an unstarted exec.CommandContext command owned
// process-tree cancellation and bounded output draining. Use only for headless
// commands: Unix process groups would change interactive terminal ownership.
// Callers that read StdoutPipe/StderrPipe themselves must also bound those reads.
func ConfigureCancellation(cmd *exec.Cmd) {
	configProcessGroupKill(cmd)
	cmd.WaitDelay = DefaultWaitDelay
}

// TimeoutError reports a killed subprocess after exceeding its deadline.
type TimeoutError struct {
	Name    string
	Args    []string
	Dir     string
	Timeout time.Duration
	Output  []byte
}

func (e *TimeoutError) Error() string {
	command := strings.TrimSpace(strings.Join(append([]string{e.Name}, e.Args...), " "))
	if e.Dir != "" {
		return fmt.Sprintf("%s timed out after %s in %s", command, e.Timeout, e.Dir)
	}
	return fmt.Sprintf("%s timed out after %s", command, e.Timeout)
}

// CombinedOutput runs a command with the package default timeout.
func CombinedOutput(name string, args []string, dir string) (string, error) {
	return CombinedOutputWithTimeout(DefaultCommandTimeout, name, args, dir)
}

// CombinedOutputWithTimeout runs a command with a caller-specified timeout and
// returns combined stdout/stderr.
func CombinedOutputWithTimeout(timeout time.Duration, name string, args []string, dir string) (string, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	ConfigureCancellation(cmd)

	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(output), &TimeoutError{
			Name:    name,
			Args:    append([]string(nil), args...),
			Dir:     dir,
			Timeout: timeout,
			Output:  append([]byte(nil), output...),
		}
	}
	return string(output), err
}
