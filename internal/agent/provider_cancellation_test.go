//go:build linux

package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers"
	"golang.org/x/sys/unix"
)

// Keep subreaper ownership in a separate test process: changing it on the suite
// process would also adopt unrelated tests' descendants. Each harness invokes
// the real adapter, and reaps the known child even when cancellation is broken.
func TestProviderCancellation(t *testing.T) {
	for _, backend := range []string{"cli", "acpx-prompt", "acpx-setup"} {
		for _, scenario := range []string{"cancel-group", "deadline-group", "cancel-escaped"} {
			t.Run(backend+"/"+scenario, func(t *testing.T) {
				runProviderCancellationHarness(t, backend, scenario)
			})
		}
	}
}

func TestProviderNormalCompletionDrainsOutput(t *testing.T) {
	for _, backend := range []string{"cli", "acpx-prompt"} {
		t.Run(backend, func(t *testing.T) {
			runProviderCancellationHarness(t, backend, "complete")
		})
	}
}

func TestProviderInteractivePreservesProcessGroup(t *testing.T) {
	for _, backend := range []string{"cli", "acpx-prompt"} {
		t.Run(backend, func(t *testing.T) {
			runProviderCancellationHarness(t, backend, "interactive")
		})
	}
}

func runProviderCancellationHarness(t *testing.T, backend, scenario string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestProviderCancellationHelperProcess$", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "PROVIDER_CANCEL_ROLE=harness", "PROVIDER_CANCEL_BACKEND="+backend, "PROVIDER_CANCEL_SCENARIO="+scenario)
	// Setup invokes several short-lived copies of this race-instrumented test
	// binary. Its exit sleep is not provider work or part of our readiness bound.
	cmd.Env = append(cmd.Env, "GORACE="+os.Getenv("GORACE")+" atexit_sleep_ms=0")
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("provider harness: %v\n%s", err, output)
	}
}

func TestProviderCancellationHelperProcess(t *testing.T) {
	switch os.Getenv("PROVIDER_CANCEL_ROLE") {
	case "harness":
		providerCancellationHarness(t)
	case "wrapper":
		providerCancellationWrapper()
		os.Exit(0)
	case "child":
		providerCancellationChild()
		os.Exit(0)
	}
}

func providerCancellationHarness(t *testing.T) {
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		t.Fatalf("enable isolated child reaping: %v", err)
	}
	testhelpers.WithShortSubprocessTimeout(t, 100*time.Millisecond)
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// exec replaces the shell, so it adds no untracked process to the fixture.
	script := "#!/bin/sh\nexport PROVIDER_CANCEL_ROLE=wrapper\nexec " + shellQuoteForTest(executable) + " -test.run=^TestProviderCancellationHelperProcess$ -- \"$@\"\n"
	for _, name := range []string{"gemini", "acpx", "codex"} {
		testhelpers.WriteShellStub(t, filepath.Join(root, name), script)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PROVIDER_CANCEL_ROOT", root)
	backend := os.Getenv("PROVIDER_CANCEL_BACKEND")
	scenario := os.Getenv("PROVIDER_CANCEL_SCENARIO")
	var adapter LLMAgent = NewCLIAgent("")
	backendName := "gemini"
	if backend != "cli" {
		adapter = NewACPXAgent("")
		backendName = "codex-acp"
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if scenario == "deadline-group" {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithTimeout(ctx, 3*time.Second)
		defer deadlineCancel()
	}
	if scenario == "interactive" {
		code, err := adapter.RunInteractive(ctx, LLMAgentInteractiveRequest{
			BackendName: backendName, AgentID: "provider-test", ProjectRoot: root,
			Environment: os.Environ(), LaunchGate: immediateLaunchGate,
		})
		if err != nil || code != 0 {
			t.Fatalf("RunInteractive = (%d, %v)", code, err)
		}
		pgid := providerCancellationReadPID(t, filepath.Join(root, "pgid"))
		if pgid != syscall.Getpgrp() {
			t.Fatalf("interactive process group = %d, want inherited %d", pgid, syscall.Getpgrp())
		}
		return
	}

	type outcome struct {
		result LLMAgentRunResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := adapter.Run(ctx, LLMAgentRunRequest{
			BackendName: backendName, AgentID: "provider-test", TaskID: "review-test",
			Prompt: "fixture", ProjectRoot: root, Environment: os.Environ(), LaunchGate: immediateLaunchGate,
		})
		done <- outcome{result, err}
	}()
	finished := false
	// Registered before readiness polling: failure to launch must also clean up.
	t.Cleanup(func() {
		cancel()
		for _, name := range []string{"wrapper", "child"} {
			if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
				if pid, err := strconv.Atoi(string(data)); err == nil && pid > 0 {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}
		if !finished {
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Error("adapter did not finish after owned fixture cleanup")
			}
		}
		data, err := os.ReadFile(filepath.Join(root, "child"))
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Errorf("invalid child PID: %q", data)
			return
		}
		reapedDone := make(chan error, 1)
		go func() {
			var status syscall.WaitStatus
			reaped, err := syscall.Wait4(pid, &status, 0, nil)
			for err == syscall.EINTR {
				reaped, err = syscall.Wait4(pid, &status, 0, nil)
			}
			if reaped == pid {
				reapedDone <- nil
				return
			}
			if err == syscall.ECHILD {
				if _, statErr := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(statErr) {
					reapedDone <- nil
					return // The wrapper reaped it before exiting.
				}
			}
			reapedDone <- fmt.Errorf("reap owned child %d: pid=%d, error=%v", pid, reaped, err)
		}()
		select {
		case err := <-reapedDone:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("owned child %d was not reaped", pid)
		}
	})
	if scenario == "complete" {
		select {
		case got := <-done:
			finished = true
			if got.err != nil || got.result.ExitCode != 0 || !strings.Contains(got.result.Output, "final-provider-output") {
				t.Fatalf("normal Run lost output or failed: %+v, %v", got.result, got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("normal provider did not finish")
		}
		return
	}
	child := providerCancellationReadPID(t, filepath.Join(root, "child"))
	providerCancellationWaitFile(t, filepath.Join(root, "pulse"))
	if scenario == "deadline-group" {
		<-ctx.Done()
	} else {
		cancel()
	}
	select {
	case <-done:
		finished = true
	case <-time.After(1500 * time.Millisecond):
		t.Error("Run did not return within 1.5s of cancellation while child held inherited pipes")
	}
	if scenario != "cancel-escaped" {
		before, err := os.ReadFile(filepath.Join(root, "pulse"))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(150 * time.Millisecond)
		defer deadline.Stop()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			// A zombie is terminated and remains ours to reap in cleanup.
			// Return on that observation; no grace-period sleep is needed.
			stat, statErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", child))
			if os.IsNotExist(statErr) {
				return
			}
			if statErr != nil {
				t.Fatalf("inspect owned child %d: %v", child, statErr)
			}
			fields := strings.Fields(string(stat)[strings.LastIndex(string(stat), ")")+1:])
			if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
				return
			}
			after, err := os.ReadFile(filepath.Join(root, "pulse"))
			if err != nil {
				t.Fatal(err)
			}
			if len(before) > 0 && len(after) > 0 && string(before) != string(after) {
				t.Errorf("owned child %d continued work/output after cancellation", child)
				t.Errorf("owned child %d has not terminated after cancellation: %s", child, stat)
				return
			}
			if len(before) == 0 {
				before = after
			}
			select {
			case <-deadline.C:
				t.Errorf("owned child %d has not terminated after cancellation: %s", child, stat)
				return
			case <-ticker.C:
			}
		}
	}
}

func providerCancellationReadPID(t *testing.T, path string) int {
	t.Helper()
	data := providerCancellationWaitFile(t, path)
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid process identifier %q in %s", data, path)
	}
	return pid
}

func providerCancellationWaitFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
		select {
		case <-deadline.C:
			t.Fatalf("provider readiness file missing: %s", path)
			return nil
		case <-ticker.C:
		}
	}
}

func providerCancellationWrapper() {
	root := os.Getenv("PROVIDER_CANCEL_ROOT")
	scenario := os.Getenv("PROVIDER_CANCEL_SCENARIO")
	if scenario == "interactive" {
		providerCancellationWrite(filepath.Join(root, "pgid"), strconv.Itoa(syscall.Getpgrp()))
		return
	}
	if os.Getenv("PROVIDER_CANCEL_BACKEND") != "cli" {
		args := " " + strings.Join(os.Args, " ") + " "
		if strings.Contains(args, " sessions show ") {
			return
		}
		setup := os.Getenv("PROVIDER_CANCEL_BACKEND") == "acpx-setup"
		if strings.Contains(args, " sessions ensure ") != setup {
			return
		}
	}
	if scenario == "complete" {
		fmt.Println(`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"text":"final-provider-output"}}}}`)
		return
	}
	providerCancellationWrite(filepath.Join(root, "wrapper"), strconv.Itoa(os.Getpid()))
	executable, err := os.Executable()
	if err != nil {
		os.Exit(91)
	}
	cmd := exec.Command(executable, "-test.run=^TestProviderCancellationHelperProcess$")
	cmd.Env = append(os.Environ(), "PROVIDER_CANCEL_ROLE=child")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if scenario == "cancel-escaped" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		os.Exit(92)
	}
	providerCancellationWrite(filepath.Join(root, "child"), strconv.Itoa(cmd.Process.Pid))
	_ = cmd.Wait()
}

func providerCancellationChild() {
	root := os.Getenv("PROVIDER_CANCEL_ROOT")
	// Ignoring SIGPIPE is deliberate: an escaped worker can keep doing work even
	// after the adapter closes its pipes. Only group members must be terminated.
	signal.Ignore(syscall.SIGPIPE)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for n := 1; n <= 1200; n++ {
		providerCancellationWrite(filepath.Join(root, "pulse"), strconv.Itoa(n))
		_, _ = fmt.Fprintln(os.Stdout, "{}")
		_, _ = fmt.Fprintln(os.Stderr, "tick")
		<-ticker.C
	}
}

func providerCancellationWrite(path, data string) {
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		os.Exit(93)
	}
}
