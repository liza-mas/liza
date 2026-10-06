package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/spf13/cobra"
)

func TestLifecycleSignalScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("direct process interrupt delivery is unavailable on Windows")
	}
	for _, mode := range []string{"unrelated", "operator", "no-pair", "invalid-pair", "missing-generation", "flag-agent", "positional-agent"} {
		t.Run(mode, func(t *testing.T) {
			_, statePath := setupMutationTestProject(t, func(state *models.State) {
				state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, time.Now().UTC())}
			})
			before := readStateBytes(t, statePath)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLifecycleSignalScopeHelper$")
			child.Env = append(os.Environ(), "D41C_SIGNAL_SCOPE_MODE="+mode, "D41C_SIGNAL_SCOPE_STATE="+statePath)
			child.Stderr = os.Stderr
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stdin.Close()
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_ = child.Process.Kill()
				if child.ProcessState == nil {
					_ = child.Wait()
				}
			}()
			lines := bufio.NewScanner(stdout)
			if !lines.Scan() || lines.Text() != "ready" {
				t.Fatalf("signal helper did not reach its barrier: %q (%v)", lines.Text(), lines.Err())
			}
			if err := child.Process.Signal(os.Interrupt); err != nil {
				t.Fatal(err)
			}
			if mode == "flag-agent" || mode == "positional-agent" {
				if !lines.Scan() || lines.Text() != "cancelled" {
					t.Fatalf("original captured retry context did not cancel: %q (%v)", lines.Text(), lines.Err())
				}
				// The helper remains alive after cancellation. Default handling
				// must already be restored when its cancellation barrier is seen.
				if err := child.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			err = child.Wait()
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("helper did not terminate from an interrupt: %v", err)
			}
			status, ok := exit.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
				t.Fatalf("expected default SIGINT termination, got %v", exit.ProcessState)
			}
			if readStateBytes(t, statePath) != before {
				t.Fatal("signal cancellation changed state")
			}
		})
	}
}

func TestLifecycleSignalScopeHelper(t *testing.T) {
	mode := os.Getenv("D41C_SIGNAL_SCOPE_MODE")
	if mode == "" {
		return
	}
	statePath := os.Getenv("D41C_SIGNAL_SCOPE_STATE")
	cmd := &cobra.Command{Use: "signal-fixture"}
	ctx, cleanup := lifecycleExecutionContext(context.Background())
	defer cleanup()
	cmd.SetContext(ctx)
	if mode != "unrelated" {
		addLifecycleFlags(cmd)
		if mode != "no-pair" {
			if err := cmd.Flags().Set("request-id", "signal-request"); err != nil {
				t.Fatal(err)
			}
			if mode != "invalid-pair" {
				state, err := db.For(statePath).ReadSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				if err := cmd.Flags().Set("expected-transition", models.TaskTransitionID(state.FindTask("target"))); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	// This deliberately precedes authority resolution, as the real CLI handlers
	// capture their options before resolving a flag/env or positional actor.
	opts, optionErr := lifecycleRequestOptions(cmd)
	if optionErr != nil && mode != "invalid-pair" {
		t.Fatal(optionErr)
	}
	if mode == "missing-generation" {
		t.Setenv(brand.EnvName("AGENT_GENERATION"), "")
		t.Setenv(brand.LegacyEnvName("AGENT_GENERATION"), "")
	}
	var authority models.AgentAuthority
	var err error
	if mode != "unrelated" && mode != "operator" {
		if mode == "flag-agent" {
			addAgentIDFlag(cmd)
			if err := cmd.Flags().Set("agent-id", "orchestrator-1"); err != nil {
				t.Fatal(err)
			}
			authority, err = requireAgentAuthority(cmd)
		} else {
			authority, err = requireAgentAuthorityForID(cmd, "orchestrator-1")
		}
		if err != nil && mode != "missing-generation" {
			t.Fatal(err)
		}
	}
	if mode == "flag-agent" || mode == "positional-agent" {
		release := testhelpers.HoldFileLock(t, statePath)
		defer release()
		var ready sync.Once
		t.Cleanup(filelock.SetAcquisitionTimeoutHookForTest(statePath, func() {
			ready.Do(func() { fmt.Fprintln(os.Stdout, "ready") })
		}))
		_, err := ops.RequestBlackboard(statePath, &authority, opts).WithLockTimeout(20 * time.Millisecond).Read()
		if !errors.Is(err, context.Canceled) || !errors.Is(opts.RetryContext.Err(), context.Canceled) {
			t.Fatalf("captured request did not cancel while acquiring the lock: %v", err)
		}
		fmt.Fprintln(os.Stdout, "cancelled")
	} else {
		fmt.Fprintln(os.Stdout, "ready")
	}
	// Keep the process alive without a timer or context observer. Only default
	// signal behavior can terminate this otherwise unresponsive operation.
	_, _ = os.Stdin.Read(make([]byte, 1))
}
