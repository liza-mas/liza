package toolresultacp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/toolresult"
)

type terminalRequest struct {
	SessionID  string   `json:"sessionId"`
	TerminalID string   `json:"terminalId"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	Cwd        string   `json:"cwd"`
	Env        []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"env"`
	OutputByteLimit *uint64 `json:"outputByteLimit"`
}

type terminalExit struct {
	ExitCode *int    `json:"exitCode"`
	Signal   *string `json:"signal,omitempty"`
}

type terminalOutput struct {
	Output     string        `json:"output"`
	Truncated  bool          `json:"truncated"`
	ExitStatus *terminalExit `json:"exitStatus,omitempty"`
}

type terminal struct {
	mu               sync.Mutex
	data             bytes.Buffer
	captureTruncated bool
	final            *terminalOutput
	finalErr         error
	finalizing       chan struct{}
	cmd              *exec.Cmd
	done             chan struct{}
	status           *terminalExit
	command          string
	session          string
	limit            *uint64
}

// terminalCaptureBytes bounds a misbehaving terminal process before it can grow
// the ACP proxy without limit. The final result marks this as source truncation.
const terminalCaptureBytes = 1024 * 1024

// Write captures a bounded prefix while continuing to drain the child pipes.
func (t *terminal) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	remaining := terminalCaptureBytes - t.data.Len()
	if remaining <= 0 {
		t.captureTruncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, err := t.data.Write(p[:remaining])
		t.captureTruncated = true
		return len(p), err
	}
	return t.data.Write(p)
}

func startTerminal(ctx context.Context, req terminalRequest, defaultDir string) (*terminal, error) {
	if req.Command == "" {
		return nil, errors.New("terminal command is required")
	}
	dir := req.Cwd
	if dir == "" {
		dir = defaultDir
	}
	if !filepath.IsAbs(dir) {
		return nil, errors.New("terminal cwd must be absolute")
	}
	command := req.Command
	for _, arg := range req.Args {
		command += " '" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
	}
	var cmd *exec.Cmd
	if len(req.Args) > 0 {
		cmd = exec.CommandContext(ctx, req.Command, req.Args...)
	} else if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for _, e := range req.Env {
		if e.Name == "" || strings.ContainsAny(e.Name, "=\x00") || strings.ContainsRune(e.Value, 0) {
			return nil, errors.New("invalid terminal environment")
		}
		cmd.Env = append(cmd.Env, e.Name+"="+e.Value)
	}
	configureProcess(cmd)
	cmd.WaitDelay = time.Second
	t := &terminal{cmd: cmd, done: make(chan struct{}), command: command, session: req.SessionID, limit: req.OutputByteLimit}
	cmd.Stdout = t
	cmd.Stderr = t
	if err := cmd.Start(); err != nil {
		return nil, errors.New("cannot start terminal command")
	}
	go func() {
		_ = cmd.Wait()
		exit := cmd.ProcessState.ExitCode()
		status := &terminalExit{ExitCode: &exit}
		if exit < 0 {
			status.ExitCode = nil
			s := strings.TrimPrefix(cmd.ProcessState.String(), "signal: ")
			status.Signal = &s
		}
		t.mu.Lock()
		t.status = status
		t.mu.Unlock()
		close(t.done)
	}()
	return t, nil
}

func (t *terminal) output(store *toolresult.Store, metadata toolresult.Result) (terminalOutput, error) {
	t.mu.Lock()
	raw := t.data.String()
	status := t.status
	captureTruncated := t.captureTruncated
	if status != nil {
		if t.final != nil {
			result, err := *t.final, t.finalErr
			t.mu.Unlock()
			return result, err
		}
		if wait := t.finalizing; wait != nil {
			t.mu.Unlock()
			<-wait
			t.mu.Lock()
			result, err := *t.final, t.finalErr
			t.mu.Unlock()
			return result, err
		}
		t.finalizing = make(chan struct{})
	}
	t.mu.Unlock()
	// A partial UTF-8 character belongs to the next poll, not a replacement rune.
	if status == nil {
		for len(raw) > 0 && !utf8.ValidString(raw) && len(raw)-lastValidUTF8(raw) <= 3 {
			raw = raw[:len(raw)-1]
		}
	}
	raw = strings.ToValidUTF8(raw, "�")
	if status == nil {
		return t.liveOutput(store, raw, captureTruncated), nil
	}
	result, err := t.persistFinal(store, metadata, raw, status, captureTruncated)
	t.mu.Lock()
	t.final, t.finalErr = &result, err
	close(t.finalizing)
	t.mu.Unlock()
	return result, err
}

func (t *terminal) liveOutput(store *toolresult.Store, raw string, captureTruncated bool) terminalOutput {
	limit := store.ThresholdBytes()
	if t.limit != nil && *t.limit < uint64(limit) {
		limit = int(*t.limit)
	}
	output := store.Sanitize(raw)
	limited := captureTruncated || len(output) > limit
	if limited {
		output = utf8Tail(output, limit)
	}
	return terminalOutput{Output: output, Truncated: limited}
}

func (t *terminal) persistFinal(store *toolresult.Store, metadata toolresult.Result, raw string, status *terminalExit, captureTruncated bool) (terminalOutput, error) {
	metadata.Tool = "exec"
	metadata.Command = t.command
	metadata.SessionID = t.session
	metadata.Content = raw
	metadata.ExitCode = nil
	metadata.Truncated = captureTruncated
	if status != nil {
		metadata.ExitCode = status.ExitCode
	}
	limited := t.limit != nil && uint64(len(raw)) > *t.limit
	var output string
	var err error
	if limited {
		// Persist the complete sanitized result, then return only an ACP-compliant
		// UTF-8 tail. A digest may itself exceed an extremely small ACP limit.
		processed, processErr := store.ProcessForced(metadata)
		if processErr != nil {
			return terminalOutput{}, processErr
		}
		if len(processed) <= int(*t.limit) {
			output = processed
		} else {
			output = utf8Tail(store.Sanitize(raw), int(*t.limit))
		}
	} else {
		output, err = store.Process(metadata)
		if err != nil {
			return terminalOutput{}, err
		}
	}
	return terminalOutput{Output: output, Truncated: captureTruncated || limited || output != raw, ExitStatus: status}, nil
}

func utf8Tail(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	start := len(value) - limit
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return value[start:]
}

func lastValidUTF8(s string) int {
	for i := len(s); i > max(0, len(s)-4); i-- {
		if utf8.ValidString(s[:i]) {
			return i
		}
	}
	return 0
}

func (t *terminal) close() { _ = killProcess(t.cmd); <-t.done }
