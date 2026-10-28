package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// gatedScript records each canonical run, then blocks until the test opens
// the gate, so a test can observe a submit while its batch runs.
const gatedScript = "echo run >> ../../runs\nwhile [ ! -f ../../gate ]; do sleep 0.02; done\n"

func shortSubmitHeartbeat(t *testing.T) {
	t.Helper()
	previous := submitHeartbeatInterval
	submitHeartbeatInterval = 20 * time.Millisecond
	t.Cleanup(func() { submitHeartbeatInterval = previous })
}

// captureStderr redirects os.Stderr for the test and returns a snapshot of
// what has been written so far.
func captureStderr(t *testing.T) func() string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = w
	var mu sync.Mutex
	var buf strings.Builder
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		chunk := make([]byte, 4096)
		for {
			n, err := r.Read(chunk)
			mu.Lock()
			buf.Write(chunk[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		os.Stderr = previous
		_ = w.Close()
		<-copied
		_ = r.Close()
	})
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

func waitForProgress(t *testing.T, stderr func() string, want string, count int) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for strings.Count(stderr(), want) < count {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("stderr has %d of %d %q lines:\n%s", strings.Count(stderr(), want), count, want, stderr())
		}
	}
}

func canonicalRuns(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "runs"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

func waitForRuns(t *testing.T, root string, want int) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for canonicalRuns(t, root) != want {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("canonical runs = %d, want %d", canonicalRuns(t, root), want)
		}
	}
}

type submitOutcome struct {
	result *SubmitForReviewResult
	err    error
}

// submitAsync starts a real submit. Cleanup opens the gate and waits for it,
// so a failing test does not leave it running over a removed directory.
func submitAsync(t *testing.T, scenario testhelpers.AcceptanceSubmitScenario) <-chan submitOutcome {
	t.Helper()
	done := make(chan submitOutcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
		done <- submitOutcome{result, err}
	}()
	t.Cleanup(func() {
		openGate(t, scenario.Root)
		select {
		case <-finished:
		case <-time.After(time.Minute):
			t.Error("submit still running at cleanup")
		}
	})
	return done
}

func openGate(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "gate"), nil, 0o644); err != nil {
		t.Error(err)
	}
}

func receive(t *testing.T, done <-chan submitOutcome) submitOutcome {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-time.After(30 * time.Second):
		t.Fatal("submit did not return")
		return submitOutcome{}
	}
}

func requireCompleted(t *testing.T, got submitOutcome) {
	t.Helper()
	if got.err != nil || got.result == nil || got.result.LifecycleOutcome.Outcome != models.LifecycleCompleted {
		t.Fatalf("submit = %+v, %v; want COMPLETED", got.result, got.err)
	}
}

var quietMarkers atomic.Int64

// stderrMarker writes a unique line to os.Stderr, the pipe production writes
// to, and waits until the capture holds it. The pipe is FIFO, so everything
// written before the marker is in the capture by then. It returns the captured
// text before the marker and up to and including it.
func stderrMarker(t testing.TB, stderr func() string) (before, through string) {
	t.Helper()
	marker := fmt.Sprintf("quiet-marker-%d\n", quietMarkers.Add(1))
	if _, err := os.Stderr.WriteString(marker); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		text := stderr()
		if i := strings.Index(text, marker); i >= 0 {
			return text[:i], text[:i+len(marker)]
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("stderr marker %q not captured within 5s (%d bytes captured)", strings.TrimSpace(marker), len(text))
		}
	}
}

// requireQuiet fails if anything reaches stderr after the submit returned:
// its reporter must be stopped and joined before the call returns.
func requireQuiet(t testing.TB, stderr func() string) {
	t.Helper()
	requireQuietDuring(t, stderr, func() { time.Sleep(5 * submitHeartbeatInterval) })
}

// requireQuietDuring fails if anything reaches stderr while window runs. Markers
// on both sides drain the capture, so output written before the call is never
// attributed to the window.
func requireQuietDuring(t testing.TB, stderr func() string, window func()) {
	t.Helper()
	_, settled := stderrMarker(t, stderr)
	window()
	before, _ := stderrMarker(t, stderr)
	if late := strings.TrimPrefix(before, settled); late != "" {
		t.Fatalf("progress written after the submit returned: %q", late)
	}
}

// D-188: a doer saw no output while its submit queued for a slot and then ran
// its batch, read the silence as a stall and interrupted it. Progress must
// repeat on stderr through both phases, naming which clock is running.
func TestSubmitProgressRepeatsWhileQueuedAndRunning(t *testing.T) {
	scenario := admissionSubmitScenario(t, gatedScript, 30, suiteAdmissionLimit)
	shortSubmitHeartbeat(t)
	stderr := captureStderr(t)
	held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)

	done := submitAsync(t, scenario)
	t.Cleanup(held.release) // Before submitAsync's cleanup waits for the submit.
	waitForProgress(t, stderr, "queued for admission class suite", 2)
	if !strings.Contains(stderr(), "the batch budget has not started") {
		t.Fatalf("queued progress does not say the budget clock is stopped:\n%s", stderr())
	}
	held.release()
	waitForProgress(t, stderr, "running canonical validation, batch budget 30s", 2)
	openGate(t, scenario.Root)
	requireCompleted(t, receive(t, done))
	requireQuiet(t, stderr)
}

// A project without admission limits still runs its batch silently for up to
// its budget, and still prepares (rebase, index refresh) before it.
func TestSubmitProgressWithoutAdmissionClass(t *testing.T) {
	scenario := admissionSubmitScenario(t, gatedScript, 30)
	shortSubmitHeartbeat(t)
	stderr := captureStderr(t)

	done := submitAsync(t, scenario)
	waitForProgress(t, stderr, "running canonical validation, batch budget 30s", 2)
	if !strings.Contains(stderr(), "preparing the submission") {
		t.Fatalf("no preparation progress before the batch:\n%s", stderr())
	}
	openGate(t, scenario.Root)
	requireCompleted(t, receive(t, done))
	requireQuiet(t, stderr)
}

// Output written before the submit returned may still sit in the capture pipe
// when the helper takes its snapshot; it must not be read as written after.
func TestRequireQuietAttributesPreReturnWrites(t *testing.T) {
	shortSubmitHeartbeat(t)
	stderr := captureStderr(t)

	// Larger than a pipe buffer, so the write returns while the capture still
	// holds unread bytes.
	if _, err := os.Stderr.WriteString("preparing the submission " + strings.Repeat("x", 128<<10) + "\n"); err != nil {
		t.Fatal(err)
	}
	requireQuiet(t, stderr)
}

// failureRecorder captures a fatal failure of a helper call without failing
// the enclosing test.
type failureRecorder struct {
	testing.TB
	message string
}

func (r *failureRecorder) Helper() {}
func (r *failureRecorder) Fatalf(format string, args ...any) {
	r.message = fmt.Sprintf(format, args...)
	runtime.Goexit()
}
func (r *failureRecorder) Fatal(args ...any) { r.Fatalf("%s", fmt.Sprint(args...)) }

// Draining must not hide a real late write: a line written inside the quiet
// window still fails with the original message.
func TestRequireQuietReportsWriteAfterReturn(t *testing.T) {
	stderr := captureStderr(t)
	const late = "running canonical validation, written late"

	rec := &failureRecorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		requireQuietDuring(rec, stderr, func() {
			if _, err := os.Stderr.WriteString(late + "\n"); err != nil {
				t.Error(err)
			}
		})
	}()
	<-done
	if !strings.Contains(rec.message, "progress written after the submit returned") || !strings.Contains(rec.message, late) {
		t.Fatalf("recorded failure = %q, want the late-write message naming %q", rec.message, late)
	}
}

// D-188 (coder-8): a second submit for a task whose submit is still queued or
// running took its own queue place and could run a second batch. While the
// same caller's submit is in flight, a duplicate queues and prepares nothing
// and is refused at once, like a duplicate of a pending preparation.
func TestDuplicateSubmitIsRefusedWhileTheFirstIsQueuedOrRunning(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limits []models.AdmissionLimit
	}{
		{name: "first queued behind a class", limits: []models.AdmissionLimit{suiteAdmissionLimit}},
		{name: "no matching class"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scenario := admissionSubmitScenario(t, gatedScript, 30, tc.limits...)
			duplicate := func(stage string) {
				t.Helper()
				result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
				var lifecycleErr *LifecycleError
				if result != nil || !errors.As(err, &lifecycleErr) {
					t.Fatalf("duplicate while the first is %s = %+v, %v; want a lifecycle refusal", stage, result, err)
				}
				if outcome := lifecycleErr.Outcome; outcome.Outcome != models.LifecycleStateChanged || outcome.SafeAction != "requery" || outcome.Effects != "unknown" {
					t.Fatalf("duplicate while the first is %s: outcome %+v, want STATE_CHANGED/requery/unknown", stage, outcome)
				}
			}
			var held *admissionSlots
			if len(tc.limits) > 0 {
				held = holdAdmissionSlots(t, scenario.Root, tc.limits...)
			}

			first := submitAsync(t, scenario)
			if held != nil {
				t.Cleanup(held.release) // Before submitAsync's cleanup waits for the submit.
				waitForTickets(t, scenario.Root, suiteAdmissionLimit.Class, 1)
				duplicate("queued")
				if got := queuedTickets(t, scenario.Root, suiteAdmissionLimit.Class); got != 1 {
					t.Fatalf("class queue has %d tickets, want only the first submit's", got)
				}
				held.release()
			}
			waitForRuns(t, scenario.Root, 1)
			duplicate("running")
			if held != nil {
				if got := queuedTickets(t, scenario.Root, suiteAdmissionLimit.Class); got != 0 {
					t.Fatalf("class queue has %d tickets while the first submit runs, want none", got)
				}
			}

			openGate(t, scenario.Root)
			requireCompleted(t, receive(t, first))
			if got := canonicalRuns(t, scenario.Root); got != 1 {
				t.Fatalf("canonical runs = %d, want only the first submit's batch", got)
			}
		})
	}
}
