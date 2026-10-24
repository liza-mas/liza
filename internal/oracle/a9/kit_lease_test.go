package a9

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

const defaultLease = 1800 * time.Second

var (
	leaseExpiryRE = regexp.MustCompile(`(?m)^\s*lease_expires: "?(\S+?)"?$`)
	heartbeatRE   = regexp.MustCompile(`(?m)^\s*heartbeat: (\S+)$`)
	claimedRE     = regexp.MustCompile(`(?m)^\s*- time: (\S+)\n\s*event: claimed$`)
)

// leaseError checks the raw at-launch state, before normalisation: the
// normaliser cannot relate an expiry that precedes its base in the document, so
// the duration is asserted here. Every lease_expires (task, claim receipt, agent)
// must equal the agent heartbeat plus the default 1800 s, and the heartbeat must
// be the claim instant when the run claimed a task.
func leaseError(raw []byte, wantLeases int, claimed bool) error {
	beat := heartbeatRE.FindSubmatch(raw)
	if beat == nil {
		return fmt.Errorf("no agent heartbeat")
	}
	base, err := time.Parse(time.RFC3339Nano, string(beat[1]))
	if err != nil {
		return fmt.Errorf("heartbeat not a timestamp")
	}
	expiries := leaseExpiryRE.FindAllSubmatch(raw, -1)
	if len(expiries) != wantLeases {
		return fmt.Errorf("%d lease expiries, want %d", len(expiries), wantLeases)
	}
	for _, m := range expiries {
		at, err := time.Parse(time.RFC3339Nano, string(m[1]))
		if err != nil {
			return fmt.Errorf("lease expiry not a timestamp")
		}
		if got := at.Sub(base); got != defaultLease {
			return fmt.Errorf("lease %s after heartbeat, want %s", got, defaultLease)
		}
	}
	if claimed {
		claim := claimedRE.FindSubmatch(raw)
		if claim == nil {
			return fmt.Errorf("no claim history entry")
		}
		if at, err := time.Parse(time.RFC3339Nano, string(claim[1])); err != nil || !at.Equal(base) {
			return fmt.Errorf("claim instant differs from heartbeat")
		}
	}
	return nil
}

func requireLeases(t testing.TB, raw []byte, wantLeases int, claimed bool) {
	t.Helper()
	if err := leaseError(raw, wantLeases, claimed); err != nil {
		t.Fatalf("A9 default lease: %v", err)
	}
}

// TestOracleA9LeaseAssertion proves the lease check rejects altered durations
// and a missing base, so the I-01/I-04 assertions cannot pass on presence alone.
func TestOracleA9LeaseAssertion(t *testing.T) {
	t.Parallel()
	const good = "tasks:\n  - history:\n      - time: 2026-01-01T00:00:00.5Z\n        event: claimed\n    lease_expires: 2026-01-01T00:30:00.5Z\nagents:\n  a:\n    lease_expires: 2026-01-01T00:30:00.5Z\n    heartbeat: 2026-01-01T00:00:00.5Z\n"
	cases := []struct {
		name, raw string
		leases    int
		claimed   bool
		ok        bool
	}{
		{"default", good, 2, true, true},
		{"shortened", "tasks:\n  - history:\n      - time: 2026-01-01T00:00:00.5Z\n        event: claimed\n    lease_expires: 2026-01-01T00:10:00.5Z\nagents:\n  a:\n    lease_expires: 2026-01-01T00:30:00.5Z\n    heartbeat: 2026-01-01T00:00:00.5Z\n", 2, true, false},
		{"configured 600", "agents:\n  a:\n    lease_expires: 2026-01-01T00:10:00Z\n    heartbeat: 2026-01-01T00:00:00Z\n", 1, false, false},
		{"missing lease", "agents:\n  a:\n    heartbeat: 2026-01-01T00:00:00Z\n", 1, false, false},
		{"claim before beat", "agents:\n  a:\n    lease_expires: 2026-01-01T00:30:01Z\n    heartbeat: 2026-01-01T00:00:01Z\n  t:\n    - time: 2026-01-01T00:00:00Z\n      event: claimed\n", 1, true, false},
	}
	for _, tc := range cases {
		if err := leaseError([]byte(tc.raw), tc.leases, tc.claimed); (err == nil) != tc.ok {
			t.Fatalf("lease check %s: error %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// TestOracleA9ArtifactSet proves a committed runtime-file golden whose artifact
// is no longer produced is reported, for the usage and alert files.
func TestOracleA9ArtifactSet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"alerts.log", "usage-records-1.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal("A9 setup: artifact golden")
		}
	}
	both := map[string]bool{"alerts.log": true, "usage-records-1.jsonl": true}
	if got := missingArtifacts(dir, both); len(got) != 0 {
		t.Fatalf("produced artifacts reported missing: %v", got)
	}
	if got := missingArtifacts(dir, map[string]bool{"alerts.log": true}); len(got) != 1 || got[0] != "usage-records-1.jsonl" {
		t.Fatalf("missing usage artifact not reported: %v", got)
	}
	if got := missingArtifacts(dir, map[string]bool{"usage-records-1.jsonl": true}); len(got) != 1 || got[0] != "alerts.log" {
		t.Fatalf("missing alert artifact not reported: %v", got)
	}
	if got := missingArtifacts(filepath.Join(dir, "absent"), nil); len(got) != 0 {
		t.Fatalf("scenario without runtime goldens reported missing: %v", got)
	}
}
