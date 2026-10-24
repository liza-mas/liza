//go:build linux

package a9

// D53 (specs/goals/20261023-architecture-consolidation-decisions.md): these A9
// scenarios freeze behavior that depends on Linux process facilities, so they run
// on Linux only.
//   - I-01 collision and I-02 capacity refusal assert wording that
//     procscan/process_status.go produces from procfs reads ("raw state mismatched
//     from procfs"); other hosts report a different liveness cause.
//   - I-05 authority loss: `delete agent --force` checks the target's identity from
//     /proc/<pid>/cmdline before it signals the supervisor.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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

func oracleA9CapacityRefused(t *testing.T) {
	t.Parallel()
	p := newScenario(t, "i02-refused")
	r := p.run(t, "agent", "coder", "--cli", "claude")
	requireCode(t, p, r, 1)
	if !strings.Contains(r.stderr, "already has 1 live agent(s) (max 1)") || !bytes.Equal(p.before, p.state(t)) || len(p.records(t)) != 0 {
		t.Fatal("I-02 cap must refuse registration without state mutation or provider launch")
	}
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
