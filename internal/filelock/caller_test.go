package filelock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerMetadataDistinguishesCallers(t *testing.T) {
	t.Parallel()
	lock := New(filepath.Join(t.TempDir(), "state.yaml"))
	lock.EnableMetrics()
	alpha := ownerCallerAlpha(t, lock)
	beta := ownerCallerBeta(t, lock)
	if !strings.Contains(alpha, "ownerCallerAlpha") || !strings.Contains(beta, "ownerCallerBeta") {
		t.Fatalf("owner records lack operation callers: alpha=%q, beta=%q", alpha, beta)
	}
	if alpha == beta {
		t.Fatal("distinct callers have identical attribution")
	}
	for _, metric := range lock.GetMetricsRecorder().GetMetrics() {
		if metric.Operation != "modify" {
			t.Fatalf("metric operation changed: %q", metric.Operation)
		}
	}
}

func ownerCallerAlpha(t *testing.T, lock *FileLock) string {
	t.Helper()
	var caller string
	if err := lock.WithLockOperation("modify", func() error {
		caller = readOwnerCaller(t, lock)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return caller
}

func ownerCallerBeta(t *testing.T, lock *FileLock) string {
	t.Helper()
	var caller string
	wantErr := errors.New("callback failed")
	err := lock.WithLockOperation("modify", func() error {
		caller = readOwnerCaller(t, lock)
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("callback error changed: %v", err)
	}
	return caller
}

func readOwnerCaller(t *testing.T, lock *FileLock) string {
	t.Helper()
	data, err := os.ReadFile(lock.ownerPath)
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Operation string `json:"operation"`
		Caller    string `json:"caller"`
		PID       int    `json:"pid"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Operation != "modify" || metadata.PID != os.Getpid() {
		t.Fatalf("owner operation or PID changed: %+v", metadata)
	}
	return metadata.Caller
}

func TestOwnerCallerIndependentInstances(t *testing.T) {
	for i := range 8 {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			lock := New(filepath.Join(t.TempDir(), "state.yaml"))
			if caller := ownerCallerAlpha(t, lock); !strings.Contains(caller, "ownerCallerAlpha") {
				t.Fatalf("missing caller: %q", caller)
			}
		})
	}
}
