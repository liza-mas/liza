package a9

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOracleA9Registration(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i01-success")
	r := p.run(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	requireCode(t, p, r, 0)
	launch := mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
	requireLeases(t, launch, 3, true)
	for _, value := range []string{"role: coder", "status: WORKING", "terminal: terminal-1", "provider: claude", "registered_at:", "lease_expires:"} {
		if !bytes.Contains(launch, []byte(value)) {
			t.Fatalf("I-01 at-launch entry missing %s", value)
		}
	}
	p.check(t, r)
}

func TestOracleA9RegistrationCollision(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i01-collision")
	r := p.run(t, "agent", "coder", "--cli", "claude", "--agent-id", "coder-1")
	requireCode(t, p, r, 1)
	if !strings.Contains(r.stderr, "agent ID collision") || !bytes.Equal(p.before, p.state(t)) || len(p.records(t)) != 0 {
		t.Fatal("I-01 collision must refuse before launch with byte-identical state")
	}
	p.check(t, r)
}

func TestOracleA9AutoAssignID(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i02-success")
		a := p.start(t, "agent", "coder", "--cli", "claude")
		p.await(t, "coder-2 registration", func() bool { return bytes.Contains(p.state(t), []byte("coder-2:")) })
		p.observeLaunch(t)
		if r := p.run(t, "stop"); r.code != 0 {
			t.Fatal("I-02 stop failed")
		}
		r := p.finish(t, a, 10*time.Second)
		requireCode(t, p, r, 0)
		if !bytes.Contains(p.state(t), []byte("coder-1:")) || bytes.Contains(p.state(t), []byte("coder-2:")) {
			t.Fatal("I-02 must retain peer and unregister auto-assigned agent")
		}
		p.check(t, r)
	})
	t.Run("capacity-refused", func(t *testing.T) {
		t.Parallel()
		p := newScenario(t, "i02-refused")
		r := p.run(t, "agent", "coder", "--cli", "claude")
		requireCode(t, p, r, 1)
		if !strings.Contains(r.stderr, "already has 1 live agent(s) (max 1)") || !bytes.Equal(p.before, p.state(t)) || len(p.records(t)) != 0 {
			t.Fatal("I-02 cap must refuse registration without state mutation or provider launch")
		}
		p.check(t, r)
	})
}

func TestOracleA9DoerClaim(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i04-success")
	r := p.run(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	requireCode(t, p, r, 0)
	launch := mustRead(t, filepath.Join(p.recordDir, "launch-1", "state.yaml"))
	requireLeases(t, launch, 3, true)
	for _, value := range []string{"status: IMPLEMENTING_CODE", "assigned_to: coder-1", "current_task: task-1", "operation: claim-task", "base_commit:"} {
		if !bytes.Contains(launch, []byte(value)) {
			t.Fatalf("I-04 launch missing %s", value)
		}
	}
	after := p.state(t)
	if bytes.Contains(after, []byte("coder-1:")) || bytes.Contains(after, []byte("assigned_to: coder-1")) || !bytes.Contains(after, []byte("event: claim_released")) {
		t.Fatal("I-05 exit must release the claim and unregister")
	}
	p.check(t, r)
}

func TestOracleA9DoerClaimRefused(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i04-refused")
	// The starting binary does not exit on a named task its role cannot claim: the
	// supervisor registers and idles, so the refusal is the unclaimed task and the
	// absent provider call. The test ends the run with `liza stop`.
	a := p.start(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	p.await(t, "idle registration", func() bool {
		s := p.state(t)
		return bytes.Contains(s, []byte("coder-1:")) && bytes.Contains(s, []byte("status: IDLE"))
	})
	p.observeLaunch(t)
	if len(p.records(t)) != 0 || bytes.Contains(p.state(t), []byte("event: claimed")) || bytes.Contains(p.state(t), []byte("assigned_to: coder-1")) {
		t.Fatal("I-04 wrong-role named task must not claim or launch")
	}
	if r := p.run(t, "stop"); r.code != 0 {
		t.Fatal("I-04 stop failed")
	}
	r := p.finish(t, a, 10*time.Second)
	requireCode(t, p, r, 0)
	p.check(t, r)
}

// TestOracleA9AuthorityLost is not parallel: its supervisor carries the
// canonical binary name, which process scans of other scenarios would match.
func TestOracleA9AuthorityLost(t *testing.T) {
	p := newScenario(t, "i05-authority-lost")
	a := p.startNamed(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	p.await(t, "held provider launch", func() bool {
		_, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml"))
		return err == nil
	})
	start := time.Now()
	if r := p.run(t, "delete", "agent", "coder-1", "--force"); r.code != 0 {
		t.Fatal("I-05 force deletion failed")
	}
	r := p.finish(t, a, 10*time.Second)
	requireCode(t, p, r, 0)
	if time.Since(start) >= 10*time.Second || bytes.Contains(p.state(t), []byte("coder-1:")) || !bytes.Contains(p.state(t), []byte("event: claim_released")) {
		t.Fatal("I-05 authority loss must stop before heartbeat and release claim")
	}
	p.check(t, r)
}

func TestOracleA9Restart42(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i06-restart")
	r := p.run(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	requireCode(t, p, r, 0)
	if len(p.records(t)) != 2 || !bytes.Contains(p.state(t), []byte("exit42_restart_count: 1")) {
		t.Fatal("I-06 must restart exactly once and re-claim")
	}
	second := mustRead(t, filepath.Join(p.recordDir, "launch-2", "state.yaml"))
	if !bytes.Contains(second, []byte("status: IMPLEMENTING_CODE")) || bytes.Count(second, []byte("operation: claim-task")) != 2 || bytes.Contains(second, []byte("status: BLOCKED")) {
		t.Fatal("I-06 re-claim must happen without spin block below threshold")
	}
	p.check(t, r)
}

// TestOracleA9Restart42Limit pins what the starting binary does with two plain
// exit-42 calls at threshold 1 (decision D40,
// specs/goals/20261023-architecture-consolidation-decisions.md): the supervisor
// releases the claim before it counts the restart, so the task returns to its
// initial status, the count passes the threshold and no restart-loop block is
// recorded. The BLOCKED branch is not pinned: no recipe reaches it at the
// starting commit without a production change or a progress-signature change.
func TestOracleA9Restart42Limit(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i06-limit")
	a := p.start(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	p.await(t, "second exit-42 restart", func() bool {
		calls, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
		return len(calls) >= 2 && bytes.Contains(p.state(t), []byte("exit42_restart_count: 2"))
	})
	after := p.state(t)
	if len(p.records(t)) != 2 || bytes.Contains(after, []byte("status: BLOCKED")) || bytes.Contains(after, []byte("restart loop")) ||
		bytes.Contains(after, []byte("assigned_to: coder-1")) || bytes.Count(after, []byte("event: claim_released")) != 2 {
		t.Fatal("I-06 threshold 1 must release the claim and record no restart-loop block")
	}
	if r := p.run(t, "stop"); r.code != 0 {
		t.Fatal("I-06 stop failed")
	}
	r := p.finish(t, a, 10*time.Second)
	requireCode(t, p, r, 0)
	p.check(t, r)
}

func TestOracleA9SpinBlock(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i07-spin")
	a := p.start(t, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
	// Distinct launch seconds prevent timestamp-named files from colliding.
	// Each barrier is released only after its saved prompt's second has passed;
	// the default spin threshold and all state values remain unchanged.
	for i := 1; i <= 10; i++ {
		index := i
		p.await(t, fmt.Sprintf("spin call %d", i), func() bool {
			paths, _ := filepath.Glob(filepath.Join(p.recordDir, "call-*.json"))
			return len(paths) >= index
		})
		prompts, _ := filepath.Glob(filepath.Join(p.dir, ".liza", "agent-prompts", "*.txt"))
		if len(prompts) == 0 {
			t.Fatal("I-07 provider launched without saved prompt")
		}
		stamp := a9StampRE.FindString(filepath.Base(prompts[len(prompts)-1]))
		p.await(t, "next prompt second", func() bool {
			return time.Now().UTC().Format("20060102-150405") != stamp
		})
		p.write(t, fmt.Sprintf(".liza/oracle-release-%d", i), []byte("release\n"))
	}
	p.await(t, "spin block", func() bool { return bytes.Contains(p.state(t), []byte("status: BLOCKED")) })
	// The guard counts an execution before launching it: the eleventh is refused,
	// so the provider runs ten times.
	if len(p.records(t)) != 10 || !bytes.Contains(p.state(t), []byte("spinning detected: 11 consecutive executions")) {
		t.Fatal("I-07 default spin guard must block the eleventh execution before launch")
	}
	if r := p.run(t, "stop"); r.code != 0 {
		t.Fatal("I-07 stop failed")
	}
	r := p.finish(t, a, 10*time.Second)
	requireCode(t, p, r, 0)
	p.check(t, r)
}

func TestOracleA9SurplusRetire(t *testing.T) {
	t.Parallel()
	for _, held := range []bool{false, true} {
		name := "i16-idle"
		if held {
			name = "i16-held"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := newScenario(t, name)
			args := []string{"agent", "coder", "--cli", "claude", "--agent-id", "coder-2"}
			if held {
				args = append(args, "task-1")
			}
			a := p.start(t, args...)
			if held {
				p.await(t, "held claim", func() bool {
					_, err := os.Stat(filepath.Join(p.recordDir, "launch-1", "state.yaml"))
					return err == nil
				})
			} else {
				p.await(t, "idle registration", func() bool { return bytes.Contains(p.state(t), []byte("coder-2:")) })
				p.observeLaunch(t)
			}
			if r := p.run(t, "config", "set", "config.max_instances", "1", "--replace", "--reason", "oracle"); r.code != 0 {
				t.Fatal("I-16 lower cap failed")
			}
			if held {
				// A command round-trip observes the held claim after the lower cap
				// committed; the provider remains behind its explicit barrier.
				if r := p.run(t, "get", "task-1", "--json"); r.code != 0 || !strings.Contains(r.stdout, "IMPLEMENTING_CODE") || !bytes.Contains(p.state(t), []byte("coder-2:")) {
					t.Fatal("I-16 cap must preserve in-flight ownership")
				}
				p.write(t, ".liza/oracle-release", []byte("release\n"))
			}
			r := p.finish(t, a, 10*time.Second)
			requireCode(t, p, r, 0)
			if bytes.Contains(p.state(t), []byte("coder-2:")) || !bytes.Contains(p.state(t), []byte("coder-1:")) {
				t.Fatal("I-16 retire/unregister must preserve peer capacity")
			}
			p.check(t, r)
		})
	}
}

func (p *scenario) observeLaunch(t testing.TB) {
	t.Helper()
	path := filepath.Join(p.recordDir, "launch-1")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal("A9 observation snapshot parent")
	}
	if err := os.WriteFile(filepath.Join(path, "state.yaml"), p.state(t), 0o644); err != nil {
		t.Fatal("A9 observation snapshot")
	}
}
func requireCode(t testing.TB, p *scenario, r invocation, want int) {
	t.Helper()
	if r.code != want {
		n := p.harness(t, p.recipeSHAs)
		t.Fatalf("A9 %s run: exit %d, want %d: %s", p.name, r.code, want, n.Text(r.stderr))
	}
}
