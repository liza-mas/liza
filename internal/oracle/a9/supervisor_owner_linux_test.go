//go:build linux

package a9

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// D48 regression coverage for the kit's supervisor entry points (startSupervisor,
// runSupervisor, startNamedSupervisor): a supervisor that ends by cancellation,
// by a signal the test sends, or by a failing test must not leave its detached
// provider alive or an adopted zombie unreaped. The scenario holds the provider
// on a wait file, so the provider outlives its supervisor in a process group of
// its own.

const ownerProbeVar = "ORACLE_A9_OWNER_PROBE"

// ownerHold starts the held-provider scenario under D48 ownership and returns the
// supervisor and provider pids, observed while both are alive.
func ownerHold(t *testing.T) (*scenario, *runningAgent, []int) {
	t.Helper()
	p := newScenario(t, "i19-held")
	a := p.startSupervisor(t, signalCoderArgs("claude")...)
	p.await(t, "held provider", func() bool {
		_, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml"))
		return err == nil
	})
	below, err := signalChildrenOf(a.cmd.Process.Pid)
	if err != nil || len(below) == 0 {
		t.Fatal("A9 owner: the held provider must be a child of the supervisor")
	}
	pids := []int{a.cmd.Process.Pid}
	for _, child := range below {
		if child.pgid != child.pid {
			t.Fatal("A9 owner: the held provider must run in a group of its own")
		}
		pids = append(pids, child.pid)
	}
	return p, a, pids
}

// ownerRequireNone fails the test unless every pid is gone and reaped and the
// test process has no child left.
func ownerRequireNone(t *testing.T, pids []int) {
	t.Helper()
	if len(pids) < 2 {
		t.Fatal("A9 owner: no provider observed")
	}
	for _, pid := range pids {
		if !signalGone(pid) {
			t.Fatal("A9 owner: a started process outlived its test")
		}
	}
	if left := orchChildren(); len(left) != 0 {
		t.Fatalf("A9 owner: %d children of the test process remain", len(left))
	}
}

// ownerWaitDone waits for the supervisor's exit to be published.
func ownerWaitDone(t *testing.T, a *runningAgent) {
	t.Helper()
	select {
	case <-a.done:
	case <-time.After(10 * time.Second):
		t.Fatal("A9 owner: the supervisor did not exit")
	}
}

// ownerAdopted waits until the provider is a child of the test process, as the
// subreaper adopts it once its supervisor is gone.
func ownerAdopted(t *testing.T, p *scenario, provider int) {
	t.Helper()
	p.await(t, "provider adopted", func() bool {
		for _, child := range orchChildren() {
			if child.pid == provider {
				return true
			}
		}
		return false
	})
}

// TestOracleA9SupervisorOwnership ends the held-provider scenario in the three
// ways that leave the provider behind. The subtest's cleanup is the ownership
// cleanup under test; the assertions after it prove its effect.
func TestOracleA9SupervisorOwnership(t *testing.T) {
	cases := []struct {
		name string
		end  func(t *testing.T, p *scenario, a *runningAgent, provider int)
	}{
		{"cancelled", func(t *testing.T, _ *scenario, a *runningAgent, _ int) {
			a.cancel()
			ownerWaitDone(t, a)
		}},
		// The supervisor dies without the kit's cancellation: the live provider is
		// adopted and only the cleanup can end it.
		{"live-adopted", func(t *testing.T, p *scenario, a *runningAgent, provider int) {
			if err := syscall.Kill(a.cmd.Process.Pid, syscall.SIGKILL); err != nil {
				t.Fatal("A9 owner: supervisor kill")
			}
			ownerWaitDone(t, a)
			ownerAdopted(t, p, provider)
		}},
		// Then the adopted provider exits unwaited: an already-adopted zombie that no
		// tracker ever observed.
		{"adopted-zombie", func(t *testing.T, p *scenario, a *runningAgent, provider int) {
			if err := syscall.Kill(a.cmd.Process.Pid, syscall.SIGKILL); err != nil {
				t.Fatal("A9 owner: supervisor kill")
			}
			ownerWaitDone(t, a)
			ownerAdopted(t, p, provider)
			if err := syscall.Kill(provider, syscall.SIGKILL); err != nil {
				t.Fatal("A9 owner: provider kill")
			}
			p.await(t, "provider zombie", func() bool {
				for _, child := range orchChildren() {
					if child.pid == provider && child.zombie {
						return true
					}
				}
				return false
			})
		}},
	}
	for _, tc := range cases {
		var pids []int
		t.Run(tc.name, func(t *testing.T) {
			p, a, started := ownerHold(t)
			pids = started
			tc.end(t, p, a, started[1])
		})
		ownerRequireNone(t, pids)
	}
}

// TestOracleA9SupervisorOwnershipFailure proves the cleanup runs when the test
// fails: it re-executes the probe, which fails on purpose while its provider is
// held, and requires that the probe failed and that neither its supervisor nor
// its provider remains.
func TestOracleA9SupervisorOwnershipFailure(t *testing.T) {
	unixOnly(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal("A9 owner probe: test executable")
	}
	root := t.TempDir()
	env, err := oracle.ChildEnv(oracle.EnvBase{
		Home: os.Getenv("HOME"), Path: os.Getenv("PATH"), Tmp: root,
	}, ownerProbeVar+"=1")
	if err != nil {
		t.Fatal("A9 owner probe: environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOracleA9SupervisorOwnershipProbe$", "-test.count=1")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatal("A9 owner probe: the probe must fail on purpose with exit code 1")
	}
	text := string(out)
	if !strings.Contains(text, "controlled failure") {
		t.Fatal("A9 owner probe: the probe did not reach its controlled failure")
	}
	if strings.Contains(text, "outlived") || strings.Contains(text, "A9 cleanup") {
		t.Fatal("A9 owner probe: the cleanup reported a leftover")
	}
	var pids []int
	for line := range strings.SplitSeq(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "A9PROBE pid: "); ok {
			pid, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatal("A9 owner probe: pid line")
			}
			pids = append(pids, pid)
		}
	}
	if len(pids) < 2 {
		t.Fatal("A9 owner probe: the probe did not report its supervisor and provider")
	}
	for _, pid := range pids {
		if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Fatal("A9 owner probe: a probe process survived the cleanup")
		}
	}
}

// TestOracleA9SupervisorOwnershipProbe is the failing test of
// TestOracleA9SupervisorOwnershipFailure; it does nothing unless its variable is
// set.
func TestOracleA9SupervisorOwnershipProbe(t *testing.T) {
	if os.Getenv(ownerProbeVar) == "" {
		t.Skip("probe of TestOracleA9SupervisorOwnershipFailure")
	}
	_, _, pids := ownerHold(t)
	var b bytes.Buffer
	for _, pid := range pids {
		b.WriteString("A9PROBE pid: " + strconv.Itoa(pid) + "\n")
	}
	_, _ = os.Stdout.Write(b.Bytes())
	t.Fatal("controlled failure")
}
