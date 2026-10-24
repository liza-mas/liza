package a9

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// signalCoderArgs is the coder run of every I-17 to I-21 scenario that claims
// task-1 through the supervisor.
func signalCoderArgs(cli string) []string {
	return []string{"agent", "coder", "task-1", "--cli", cli, "--agent-id", "coder-1"}
}

// signalAlerts returns alerts.log, empty when the run wrote none.
func signalAlerts(p *scenario) string {
	b, err := os.ReadFile(filepath.Join(p.dir, ".liza", "alerts.log"))
	if err != nil {
		return ""
	}
	return string(b)
}

// signalStart launches the CLI like scenario.start, but in its own process
// group, and registers the D43 hygiene: on every exit path, normal or failing,
// cleanup kills the supervisor, its providers (each in a group of its own) and
// their descendants, and fails the test if one outlives it.
func signalStart(t *testing.T, p *scenario, args ...string) *runningAgent {
	t.Helper()
	a := &runningAgent{done: make(chan error, 1)}
	a.cmd, a.cancel = p.command(p.cli, append([]string{"-C", p.dir}, args...)...)
	signalIsolate(a.cmd)
	a.cmd.Stdout, a.cmd.Stderr = &a.stdout, &a.stderr
	if err := a.cmd.Start(); err != nil {
		t.Fatalf("A9 %s run: agent start", p.name)
	}
	go func() { a.done <- a.cmd.Wait(); close(a.done) }()
	pid := a.cmd.Process.Pid
	t.Cleanup(func() {
		reaped := signalReap(pid)
		a.cancel()
		<-a.done
		if !reaped {
			t.Errorf("A9 %s cleanup: a started process outlived the test", p.name)
		}
	})
	return a
}

// signalRun is signalStart followed by waiting for the exit.
func signalRun(t *testing.T, p *scenario, args ...string) invocation {
	t.Helper()
	return p.finish(t, signalStart(t, p, args...), 100*time.Second)
}

// signalSeed copies the scenario's committed signal files into .liza and
// refreshes the before side of the runtime-file snapshots, so the signal files
// are part of the input state rather than a run effect.
func signalSeed(t testing.TB, p *scenario) {
	t.Helper()
	dir := filepath.Join("testdata", "fixtures", p.name, "signals")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("A9 %s setup: signal fixtures unavailable", p.name)
	}
	for _, entry := range entries {
		p.write(t, filepath.Join(".liza", entry.Name()), mustRead(t, filepath.Join(dir, entry.Name())))
	}
	p.beforeFiles = p.runtimeFiles(t)
}

func signalFiles(p *scenario) []string {
	paths, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "provider-*"))
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, filepath.Base(path))
	}
	return names
}

func TestOracleA9AuditAndCleanup(t *testing.T) {
	t.Parallel()
	t.Run("i17-degraded", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i17-degraded")
		r := signalRun(t, p, signalCoderArgs("codex")...)
		requireCode(t, p, r, 0)
		if !strings.Contains(signalAlerts(p), "PROVIDER AUDIT DEGRADED — codex: failed to record rollout items: thread not found") ||
			!bytes.Contains(p.state(t), []byte("type: provider_audit_degraded")) {
			t.Fatal("I-17 audit pattern must raise the alert and record the anomaly")
		}
		p.check(t, r)
	})
	t.Run("i17-clean", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i17-clean")
		r := signalRun(t, p, signalCoderArgs("codex")...)
		requireCode(t, p, r, 0)
		if signalAlerts(p) != "" || bytes.Contains(p.state(t), []byte("provider_audit_degraded")) {
			t.Fatal("I-17 output without the audit pattern must not degrade")
		}
		p.check(t, r)
	})
	t.Run("i18-cleanup", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i18-cleanup")
		for _, id := range []string{"ia-1", "ia-2"} {
			p.git(t, "worktree", "add", "-q", "-b", "task/"+id, filepath.Join(".worktrees", id))
		}
		a := signalStart(t, p, "agent", "code-reviewer", "--cli", "claude", "--agent-id", "code-reviewer-1")
		// The kept task precedes the clean one in the cleanup pass, so the clean
		// task's completion event proves the kept task was evaluated and left.
		p.await(t, "clean-terminal cleanup", func() bool { return bytes.Contains(p.state(t), []byte("trigger: completion")) })
		p.observeLaunch(t)
		if _, err := os.Stat(filepath.Join(p.dir, ".worktrees", "ia-1")); !os.IsNotExist(err) {
			t.Fatal("I-18 clean-terminal task worktree must be removed")
		}
		if _, err := os.Stat(filepath.Join(p.dir, ".worktrees", "ia-2")); err != nil || !bytes.Contains(p.state(t), []byte("worktree: .worktrees/ia-2")) {
			t.Fatal("I-18 task not at a clean state must keep its worktree")
		}
		if r := p.run(t, "stop"); r.code != 0 {
			t.Fatal("I-18 stop failed")
		}
		r := p.finish(t, a, 10*time.Second)
		requireCode(t, p, r, 0)
		p.check(t, r)
	})
}

func TestOracleA9QuotaRaise(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		dirty bool
	}{{"i19-dirty", true}, {"i19-clean", false}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := newScenario(t, c.name)
			r := signalRun(t, p, signalCoderArgs("claude")...)
			requireCode(t, p, r, 0)
			if got := signalFiles(p); len(got) != 1 || got[0] != "provider-quota-exhausted-claude" ||
				!strings.Contains(signalAlerts(p), "PROVIDER QUOTA EXHAUSTED — claude: You've hit your usage limit") ||
				bytes.Contains(p.state(t), []byte("assigned_to: coder-1")) || !bytes.Contains(p.state(t), []byte("event: claim_released")) {
				t.Fatal("I-19 quota line must raise the signal and alert and release the claim")
			}
			wip := strings.TrimSpace(p.git(t, "log", "--all", "--format=%s", "--grep=^WIP: provider quota exhausted"))
			if c.dirty != (wip != "") {
				t.Fatalf("I-19 WIP commit presence %v, want %v", wip != "", c.dirty)
			}
			p.check(t, r)
		})
	}
	t.Run("i19-other", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i19-other")
		a := signalStart(t, p, signalCoderArgs("codex")...)
		p.await(t, "second provider call", func() bool {
			calls, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
			return len(calls) >= 2
		})
		r := p.finish(t, a, 15*time.Second)
		requireCode(t, p, r, 0)
		if len(signalFiles(p)) != 0 || strings.Contains(signalAlerts(p), "QUOTA") {
			t.Fatal("I-19 another provider's quota line must not raise a signal")
		}
		p.check(t, r)
	})
}

func TestOracleA9UnavailableRaise(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, cli, signal, alert string }{
		{"i20-claude", "claude", "provider-unavailable-claude", "PROVIDER UNAVAILABLE — claude: Failed to authenticate: OAuth session expired and could not be refreshed"},
		{"i20-codex", "codex", "provider-unavailable-codex", "PROVIDER UNAVAILABLE — codex: thread/start failed: error creating thread: Codex cannot access session files under .codex/sessions (permission denied)"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := newScenario(t, c.name)
			r := signalRun(t, p, signalCoderArgs(c.cli)...)
			requireCode(t, p, r, 0)
			if got := signalFiles(p); len(got) != 1 || got[0] != c.signal || !strings.Contains(signalAlerts(p), c.alert) {
				t.Fatal("I-20 provider pattern must raise the signal and alert")
			}
			p.check(t, r)
		})
	}
	t.Run("i20-cross", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i20-cross")
		a := signalStart(t, p, signalCoderArgs("codex")...)
		p.await(t, "second provider call", func() bool {
			calls, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
			return len(calls) >= 2
		})
		r := p.finish(t, a, 15*time.Second)
		requireCode(t, p, r, 0)
		if len(signalFiles(p)) != 0 || strings.Contains(signalAlerts(p), "UNAVAILABLE") {
			t.Fatal("I-20 claude's pattern printed by a codex run must not raise a signal")
		}
		p.check(t, r)
	})
}

func TestOracleA9SignalMatch(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, cli string
		blocked   bool
	}{
		{"i21-own-quota", "claude", true},
		{"i21-own-unavailable", "claude", true},
		{"i21-unavailable-codex-acp-run", "codex-acp", true},
		{"i21-unavailable-codex-run", "codex", false},
		{"i21-quota-codex-acp-run", "codex-acp", true},
		{"i21-quota-codex-run", "codex", false},
		{"i21-expired", "claude", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := newScenario(t, c.name)
			signalSeed(t, p)
			r := signalRun(t, p, signalCoderArgs(c.cli)...)
			requireCode(t, p, r, 0)
			claimed := bytes.Contains(p.state(t), []byte("event: claimed"))
			if c.blocked && (claimed || len(p.records(t)) != 0) {
				t.Fatal("I-21 an unexpired signal for the run's provider must exit without claim")
			}
			if !c.blocked && (!claimed || len(p.records(t)) != 1) {
				t.Fatal("I-21 without a matching live signal the run must claim and launch")
			}
			p.check(t, r)
		})
	}
}

// TestSignalCorrelationSuffix pins D43: the collision/capacity correlation
// suffix depends on host processes, so every variant normalises to one token.
func TestSignalCorrelationSuffix(t *testing.T) {
	t.Parallel()
	const want = "recorded namespace-local pid <V1>; <CORRELATION>"
	for _, suffix := range []string{
		"correlation unavailable",
		"observer-visible matching pid 4242",
		"observer-visible matching pids [4242 4243]",
	} {
		got := newA9Normaliser(nil).text("recorded namespace-local pid 77; " + suffix)
		if got != want {
			t.Fatalf("correlation suffix %q: got %q; want %q", suffix, got, want)
		}
	}
}

// TestSignalGroupReap shows the reap reaches a descendant that outlives its
// group leader, which the kit's direct-child cancellation does not.
func TestSignalGroupReap(t *testing.T) {
	t.Parallel()
	unixOnly(t)
	cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "/bin/sleep 300 >/dev/null 2>&1 & echo ready; wait")
	cmd.Env = []string{}
	signalIsolate(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal("A9 signal group: pipe unavailable")
	}
	if err := cmd.Start(); err != nil {
		t.Fatal("A9 signal group: start failed")
	}
	root := cmd.Process.Pid
	t.Cleanup(func() { signalReap(root) })
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		t.Fatal("A9 signal group: descendant not started")
	}
	below := signalDescendants(root)
	if len(below) != 1 {
		t.Fatal("A9 signal group: expected one descendant")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal("A9 signal group: leader kill failed")
	}
	_ = cmd.Wait()
	if !signalPidLive(below[0].pid) {
		t.Fatal("A9 signal group: killing only the leader must leave the descendant alive")
	}
	if !signalReap(root) || signalPidLive(below[0].pid) {
		t.Fatal("A9 signal group: reap must remove the descendant")
	}
}

// TestSignalProviderReap holds a real scenario stub provider, which the
// supervisor runs in a process group of its own, and ends the scenario while it
// is held: through the scenario cleanup, and through the cancellation the
// kit's command deadline performs before any cleanup. Supervisor and provider
// must be gone either way.
func TestSignalProviderReap(t *testing.T) {
	unixOnly(t)
	for _, cancelled := range []bool{false, true} {
		name := "cleanup"
		if cancelled {
			name = "cancelled"
		}
		var started []int
		t.Run(name, func(t *testing.T) {
			p := newScenario(t, "i19-held")
			a := signalStart(t, p, signalCoderArgs("claude")...)
			p.await(t, "held provider", func() bool {
				_, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml"))
				return err == nil
			})
			supervisor := a.cmd.Process.Pid
			started = append(started, supervisor)
			separate := false
			for _, proc := range signalDescendants(supervisor) {
				started = append(started, proc.pid)
				separate = separate || proc.pgid != supervisor
			}
			if !separate {
				t.Fatal("I-19 the held provider must run in a group of its own")
			}
			if cancelled {
				a.cancel()
				select {
				case <-a.done:
				case <-time.After(10 * time.Second):
					t.Fatal("I-19 cancelled supervisor did not exit")
				}
				signalAwaitGone(t, started)
			}
		})
		if len(started) < 2 {
			t.Fatalf("A9 signal provider %s: no provider observed", name)
		}
		signalAwaitGone(t, started)
	}
}

// signalAwaitGone fails the test unless none of the processes is live within a bound.
func signalAwaitGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		live := false
		for _, pid := range pids {
			live = live || signalPidLive(pid)
		}
		if !live {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("A9 signal provider: a started process outlived its scenario")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
