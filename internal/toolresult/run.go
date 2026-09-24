package toolresult

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// Run captures a non-interactive command before a native client's output cap or
// streaming boundary. Raw output exists only in memory, never in a temporary
// file. The original child exit status is separate from a boundary failure.
func Run(ctx context.Context, store *Store, result Result, args []string, input io.Reader, output io.Writer) (int, error) {
	if store == nil || len(args) == 0 || args[0] == "" {
		return 1, fmt.Errorf("tool-result run requires a store and executable")
	}
	if result.Command == "" {
		identity, _ := json.Marshal(args)
		result.Command = string(identity)
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	configureRunCancellation(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stdin = input
	// The identical writer makes os/exec serialize stdout and stderr writes.
	captured := &cappedBuffer{limit: runCaptureBytes}
	cmd.Stdout, cmd.Stderr = captured, captured
	err := cmd.Run()
	if cmd.ProcessState == nil {
		// exec errors may include secret-bearing arguments/paths.
		return 1, fmt.Errorf("tool-result command could not start")
	}
	code := runExitCode(cmd)
	result.ExitCode = &code
	result.Content = captured.data.String()
	result.Truncated = result.Truncated || captured.truncated || ctx.Err() != nil || errors.Is(err, exec.ErrWaitDelay)
	filtered, filterErr := store.Process(result)
	if filterErr != nil {
		return 1, filterErr
	}
	if _, writeErr := io.WriteString(output, filtered); writeErr != nil {
		return 1, fmt.Errorf("tool-result output could not be written")
	}
	// ErrWaitDelay after the command exited means a background job still holds
	// the output pipe: the command itself finished, as it would natively.
	if err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return 1, fmt.Errorf("tool-result command capture did not finish cleanly")
		}
	}
	return code, nil
}

// runCaptureBytes bounds a runaway command (`yes`, `tail -f`) before it can
// grow memory; the remainder is drained and the result marked truncated.
const runCaptureBytes = 1024 * 1024

type cappedBuffer struct {
	data      bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.data.Len()
	if len(p) > remaining {
		b.truncated = true
		if remaining > 0 {
			b.data.Write(p[:remaining])
		}
		return len(p), nil
	}
	return b.data.Write(p)
}
