package a9

import (
	"bytes"
	"os"
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
		r := p.run(t, signalCoderArgs("codex")...)
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
		r := p.run(t, signalCoderArgs("codex")...)
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
		a := p.start(t, "agent", "code-reviewer", "--cli", "claude", "--agent-id", "code-reviewer-1")
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
			r := p.run(t, signalCoderArgs("claude")...)
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
		a := p.start(t, signalCoderArgs("codex")...)
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
			r := p.run(t, signalCoderArgs(c.cli)...)
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
		a := p.start(t, signalCoderArgs("codex")...)
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
			r := p.run(t, signalCoderArgs(c.cli)...)
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
