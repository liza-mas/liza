package toolresult

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClaudeShellCaptureHelper(t *testing.T) {
	if os.Getenv("DEV778_CLAUDE_CAPTURE_HELPER") != "1" {
		return
	}
	dir := os.Args[len(os.Args)-1]
	defer CleanupClaudeCapture(dir)
	f, err := os.Open(filepath.Join(dir, "output"))
	if err != nil {
		fmt.Println("CAPTURE_FAILED")
		return
	}
	defer f.Close()
	result, err := ReadClaudeCapture(f, filepath.Join(dir, "status"))
	if err != nil {
		fmt.Println("CAPTURE_FAILED")
		return
	}
	s, err := New(os.Getenv("DEV778_CLAUDE_CAPTURE_STORE"), Config{ThresholdBytes: 2048, DigestBytes: 1024}, nil)
	if err != nil {
		fmt.Println("CAPTURE_FAILED")
		return
	}
	output, err := s.Process(result)
	if err != nil {
		fmt.Println("CAPTURE_FAILED")
		return
	}
	fmt.Print(output)
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()
	_ = os.WriteFile(filepath.Join(dir, "ready"), []byte("0"), 0600)
	DrainClaudeBackground(dir, f)
}

func TestClaudeShellNativeSemantics(t *testing.T) {
	t.Setenv("DEV778_CLAUDE_CAPTURE_HELPER", "1")
	t.Setenv("DEV778_CLAUDE_CAPTURE_STORE", filepath.Join(t.TempDir(), "store"))
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		if _, err := os.Stat(shell); err != nil {
			continue
		}
		for _, sample := range []struct {
			name, command string
			exit          int
			want          string
		}{
			{"background", "sleep 1 & printf bgdone", 0, "bgdone"},
			{"small", "printf 'small exact\n'", 0, "small exact"},
			{"error", "printf 'failed real command\n' >&2; exit 7", 7, "failed real command"},
			{"cwd", "cd /tmp; export DEV778_INSIDE=value; printf '%s:%s' \"$PWD\" \"$DEV778_INSIDE\"", 0, "/tmp:value"},
			{"oversized", "i=0; while [ $i -lt 600 ]; do printf 'source-shaped output line\n'; i=$((i+1)); done", 0, "artifact_id"},
		} {
			t.Run(filepath.Base(shell)+"/"+sample.name, func(t *testing.T) {
				script, err := ClaudeShellCommand(sample.command, []string{os.Args[0], "-test.run=^TestClaudeShellCaptureHelper$", "--"}, Result{TaskID: "DEV-778", SessionID: "native"})
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				started := time.Now()
				cmd := exec.CommandContext(ctx, shell, "-c", script)
				capture, err := os.CreateTemp(t.TempDir(), "native-output")
				if err != nil {
					t.Fatal(err)
				}
				cmd.Stdout, cmd.Stderr = capture, capture
				err = cmd.Run()
				_ = capture.Close()
				out, readErr := os.ReadFile(capture.Name())
				if readErr != nil {
					t.Fatal(readErr)
				}
				code := 0
				if err != nil {
					if e, ok := err.(*exec.ExitError); ok {
						code = e.ExitCode()
					} else {
						t.Fatal(err)
					}
				}
				if code != sample.exit || !strings.Contains(string(out), sample.want) || strings.Contains(string(out), "CAPTURE_FAILED") {
					t.Fatalf("exit=%d expected=%d err=%v out=%s", code, sample.exit, err, out)
				}
				if sample.name == "background" && time.Since(started) > 800*time.Millisecond {
					t.Fatalf("waited for detached background child: %s", time.Since(started))
				}
				if sample.name == "oversized" && len(out) > 1200 {
					t.Fatalf("unbounded output: %d", len(out))
				}
			})
		}
	}
}

func TestClaudeShellRejectsReservedState(t *testing.T) {
	for _, command := range []string{"trap 'echo bye' EXIT", "echo hi; trap - EXIT", "exec 198>&1", "__toolresult_d=/tmp"} {
		if _, err := ClaudeShellCommand(command, []string{"test"}, Result{}); err == nil {
			t.Fatalf("accepted %q", command)
		}
	}
}

func TestClaudeCaptureMissingStatusFailsClosed(t *testing.T) {
	payload, _ := json.Marshal(Result{Tool: "Bash"})
	_, err := ReadClaudeCapture(strings.NewReader(string(payload)+"\nsecret raw output"), filepath.Join(t.TempDir(), "missing"))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe failure: %v", err)
	}
}
