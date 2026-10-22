package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// These tests set the package-level terminalDecodeTestHook, so none of them
// may run in parallel.

// transcriptText is a raw provider event that state hygiene rejects.
const transcriptText = `{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"x"}}`

type archivedWarmFixture struct {
	p       paths.LizaPaths
	tasks   []models.Task
	digests []string
	objects []string
}

// newArchivedWarmFixture publishes count archived terminal tasks; mutate may
// alter each task before archival.
func newArchivedWarmFixture(t *testing.T, count int, mutate func(*models.Task)) archivedWarmFixture {
	t.Helper()
	f := archivedWarmFixture{p: paths.New(t.TempDir())}
	rows := make([]any, 0, count)
	for i := range count {
		task := coldTerminalTask(models.TaskStatusMerged)
		task.ID = fmt.Sprintf("warm-%02d", i)
		if mutate != nil {
			mutate(&task)
		}
		digest, object, stub := writeColdTerminalTaskObject(t, f.p, task)
		f.tasks, f.digests, f.objects = append(f.tasks, task), append(f.digests, digest), append(f.objects, object)
		rows = append(rows, stub)
	}
	writeColdTerminalTaskState(t, f.p, rows, true)
	return f
}

// decodeRecorder counts archive objects decoded on a cache miss.
type decodeRecorder struct {
	mu      sync.Mutex
	digests []string
	onEach  func()
	decoded chan struct{} // one send per decode, never blocking
}

func recordDecodes(t *testing.T) *decodeRecorder {
	t.Helper()
	r := &decodeRecorder{decoded: make(chan struct{}, 64)}
	terminalDecodeTestHook = func(digest string) {
		r.mu.Lock()
		r.digests = append(r.digests, digest)
		onEach := r.onEach
		r.mu.Unlock()
		select {
		case r.decoded <- struct{}{}:
		default:
		}
		if onEach != nil {
			onEach()
		}
	}
	t.Cleanup(func() { terminalDecodeTestHook = nil })
	return r
}

func (r *decodeRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.digests)
}

func touchGoal(description string) func(*models.State) error {
	return func(state *models.State) error {
		state.Goal.Description = description
		return nil
	}
}

// modifyBehindHeldLock runs bb.Modify while another holder keeps the state
// lock until every expected decode happened, and returns the decodes observed
// before and after the holder released it.
func modifyBehindHeldLock(t *testing.T, bb *Blackboard, rec *decodeRecorder, expectWhileWaiting int, fn func(*models.State) error) (int, int, error) {
	t.Helper()
	held, release := make(chan struct{}), make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- filelock.New(bb.statePath).WithLockOperation("test-hold", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	for len(rec.decoded) > 0 {
		<-rec.decoded
	}
	before := rec.count()
	done := make(chan error, 1)
	go func() { done <- bb.Modify(fn) }()
	timeout := time.After(5 * time.Second)
	for waited := 0; waited < expectWhileWaiting; waited++ {
		select {
		case <-rec.decoded:
		case <-timeout:
			waited = expectWhileWaiting
		}
	}
	whileWaiting := rec.count() - before
	close(release)
	if err := <-holderDone; err != nil {
		t.Fatal(err)
	}
	err := <-done
	return whileWaiting, rec.count() - before - whileWaiting, err
}

func validatedDigests(bb *Blackboard) map[string]bool {
	bb.archive.mu.Lock()
	defer bb.archive.mu.Unlock()
	validated := make(map[string]bool)
	for digest, entry := range bb.archive.entries {
		if entry.validated {
			validated[digest] = true
		}
	}
	return validated
}

func TestModifyDecodesArchiveWhileWaitingForTheLock(t *testing.T) {
	f := newArchivedWarmFixture(t, 3, nil)
	rec := recordDecodes(t)
	bb := New(f.p.StatePath())

	waiting, locked, err := modifyBehindHeldLock(t, bb, rec, 3, touchGoal("first"))
	if err != nil {
		t.Fatal(err)
	}
	if waiting != 3 || locked != 0 {
		t.Fatalf("archive objects decoded while waiting=%d, under the lock=%d; want 3 and 0", waiting, locked)
	}
	if got := validatedDigests(bb); len(got) != 3 {
		t.Fatalf("warm-up validated %d objects, want 3", len(got))
	}
	state, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if state.Goal.Description != "first" {
		t.Fatal("the mutation was not published")
	}
	assertColdTerminalTaskRestored(t, state, f.tasks[0])
}

func TestModifyAfterWarmUpStillDetectsCorruptEvidence(t *testing.T) {
	f := newArchivedWarmFixture(t, 2, nil)
	bb := New(f.p.StatePath())
	bb.warmTerminalArchive()
	if got := validatedDigests(bb); len(got) != 2 {
		t.Fatalf("warm-up validated %d objects, want 2", len(got))
	}
	if err := os.WriteFile(f.objects[1], []byte(`{"format_version":2}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(f.p.StatePath())
	err := bb.Modify(touchGoal("must not publish"))
	var archiveErr *TerminalArchiveError
	if !errors.As(err, &archiveErr) || archiveErr.Reason != "digest mismatch" {
		t.Fatalf("Modify error = %v, want a digest-mismatch TerminalArchiveError", err)
	}
	after, _ := os.ReadFile(f.p.StatePath())
	if string(before) != string(after) {
		t.Fatal("a transaction over corrupt evidence published state")
	}
}

func TestArchivedPayloadFailingHygieneIsValidatedOnFirstUse(t *testing.T) {
	bad := func(task *models.Task) { task.Description = transcriptText }
	for name, prepare := range map[string]func(*Blackboard){
		"warm-up":           func(*Blackboard) {},
		"decoded by a read": func(bb *Blackboard) { bb.ReadSnapshot() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newArchivedWarmFixture(t, 1, bad)
			bb := New(f.p.StatePath())
			prepare(bb)
			err := bb.Modify(touchGoal("must not publish"))
			if err == nil || !strings.Contains(err.Error(), "state hygiene validation failed") {
				t.Fatalf("Modify error = %v, want a hygiene validation failure", err)
			}
			if got := validatedDigests(bb); len(got) != 0 {
				t.Fatal("an object that failed hygiene validation was marked validated")
			}
		})
	}
}

func TestChangedArchivedPayloadIsValidated(t *testing.T) {
	f := newArchivedWarmFixture(t, 2, nil)
	bb := New(f.p.StatePath())
	if err := bb.Modify(touchGoal("validated")); err != nil {
		t.Fatal(err)
	}
	if got := validatedDigests(bb); len(got) != 2 {
		t.Fatalf("validated %d objects, want 2", len(got))
	}
	before, _ := os.ReadFile(f.p.StatePath())
	err := bb.Modify(func(state *models.State) error {
		state.FindTask(f.tasks[0].ID).Description = transcriptText
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "state hygiene validation failed") {
		t.Fatalf("Modify error = %v, want a hygiene validation failure", err)
	}
	after, _ := os.ReadFile(f.p.StatePath())
	if string(before) != string(after) {
		t.Fatal("a changed archived payload that fails hygiene was published")
	}
}

func TestArchiveCacheRefusalKeepsValidationAndWrites(t *testing.T) {
	t.Cleanup(setTerminalArchiveCacheMaxBytesForTest(1))
	t.Run("hygiene still enforced", func(t *testing.T) {
		f := newArchivedWarmFixture(t, 1, func(task *models.Task) { task.Description = transcriptText })
		err := New(f.p.StatePath()).Modify(touchGoal("must not publish"))
		if err == nil || !strings.Contains(err.Error(), "state hygiene validation failed") {
			t.Fatalf("Modify error = %v, want a hygiene validation failure", err)
		}
	})
	t.Run("valid write succeeds", func(t *testing.T) {
		f := newArchivedWarmFixture(t, 2, nil)
		bb := New(f.p.StatePath())
		if err := bb.Modify(touchGoal("refused cache")); err != nil {
			t.Fatal(err)
		}
		state, err := bb.ReadSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if state.Goal.Description != "refused cache" {
			t.Fatal("the mutation was not published")
		}
		assertColdTerminalTaskRestored(t, state, f.tasks[1])
		if got := validatedDigests(bb); len(got) != 0 {
			t.Fatal("a refused object was marked validated")
		}
	})
}

func TestDerivedInstancesShareTheArchiveCache(t *testing.T) {
	f := newArchivedWarmFixture(t, 3, nil)
	rec := recordDecodes(t)
	base := New(f.p.StatePath())
	if err := base.Modify(touchGoal("base")); err != nil {
		t.Fatal(err)
	}
	decoded := rec.count()
	derived := map[string]*Blackboard{
		"retry context": base.WithLockRetryContext(context.Background()),
		"patient":       base.Patient(),
		"lock timeout":  base.WithLockTimeout(time.Second),
	}
	for name, bb := range derived {
		if err := bb.Modify(touchGoal(name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if extra := rec.count() - decoded; extra != 0 {
		t.Fatalf("derived instances decoded %d archive objects again", extra)
	}
}

func TestWarmUpCoversDigestsPublishedLater(t *testing.T) {
	t.Run("first archive after a warm-up with none", func(t *testing.T) {
		p := paths.New(t.TempDir())
		task := coldTerminalTask(models.TaskStatusMerged)
		writeColdTerminalTaskState(t, p, []any{task}, true)
		rec := recordDecodes(t)
		observer := New(p.StatePath())
		if err := observer.Modify(touchGoal("no archive yet")); err != nil {
			t.Fatal(err)
		}
		// Another process (separate cache) archives the task.
		archiver := New(p.StatePath())
		if err := archiver.Modify(func(state *models.State) error {
			_, err := archiver.ArchiveTerminalTask(state.FindTask(task.ID), time.Now())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		waiting, locked, err := modifyBehindHeldLock(t, observer, rec, 1, touchGoal("archived"))
		if err != nil {
			t.Fatal(err)
		}
		if waiting != 1 || locked != 0 {
			t.Fatalf("new object decoded while waiting=%d, under the lock=%d; want 1 and 0", waiting, locked)
		}
	})
	t.Run("rewritten terminal task", func(t *testing.T) {
		f := newArchivedWarmFixture(t, 2, nil)
		rec := recordDecodes(t)
		observer := New(f.p.StatePath())
		if err := observer.Modify(touchGoal("warm")); err != nil {
			t.Fatal(err)
		}
		writer := New(f.p.StatePath())
		if err := writer.Modify(func(state *models.State) error {
			state.FindTask(f.tasks[0].ID).Description = "rewritten after archival"
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		waiting, locked, err := modifyBehindHeldLock(t, observer, rec, 1, touchGoal("rewritten"))
		if err != nil {
			t.Fatal(err)
		}
		if waiting != 1 || locked != 0 {
			t.Fatalf("rewritten object decoded while waiting=%d, under the lock=%d; want 1 and 0", waiting, locked)
		}
	})
	t.Run("failed warm-up does not disable later ones", func(t *testing.T) {
		f := newArchivedWarmFixture(t, 2, nil)
		rec := recordDecodes(t)
		bb := New(f.p.StatePath())
		hidden := f.objects[1] + ".hidden"
		if err := os.Rename(f.objects[1], hidden); err != nil {
			t.Fatal(err)
		}
		var archiveErr *TerminalArchiveError
		if err := bb.Modify(touchGoal("missing")); !errors.As(err, &archiveErr) {
			t.Fatalf("Modify error = %v, want TerminalArchiveError for the missing object", err)
		}
		if err := os.Rename(hidden, f.objects[1]); err != nil {
			t.Fatal(err)
		}
		waiting, locked, err := modifyBehindHeldLock(t, bb, rec, 1, touchGoal("restored"))
		if err != nil {
			t.Fatal(err)
		}
		if waiting != 1 || locked != 0 {
			t.Fatalf("restored object decoded while waiting=%d, under the lock=%d; want 1 and 0", waiting, locked)
		}
	})
}

func TestWarmUpHonoursRetryContextCancellation(t *testing.T) {
	f := newArchivedWarmFixture(t, 3, nil)
	rec := recordDecodes(t)
	base := New(f.p.StatePath())
	called := false
	callback := func(state *models.State) error {
		called = true
		state.Goal.Description = "cancelled"
		return nil
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := base.WithLockRetryContext(cancelled).Modify(callback); !errors.Is(err, context.Canceled) {
		t.Fatalf("already-cancelled Modify error = %v, want context.Canceled", err)
	}
	if called || rec.count() != 0 {
		t.Fatalf("already-cancelled Modify ran its callback=%v or decoded %d objects", called, rec.count())
	}

	during, cancelDuring := context.WithCancel(context.Background())
	rec.onEach = cancelDuring
	if err := base.WithLockRetryContext(during).Modify(callback); !errors.Is(err, context.Canceled) {
		t.Fatalf("Modify cancelled during warm-up error = %v, want context.Canceled", err)
	}
	rec.onEach = nil
	if called || rec.count() != 1 {
		t.Fatalf("cancelled warm-up ran its callback=%v or decoded %d objects, want 1", called, rec.count())
	}
	if got := validatedDigests(base); len(got) > 1 {
		t.Fatalf("cancelled warm-up marked %d objects validated", len(got))
	}

	if err := base.WithLockRetryContext(context.Background()).Modify(touchGoal("healthy")); err != nil {
		t.Fatal(err)
	}
	if got := validatedDigests(base); len(got) != 3 {
		t.Fatalf("healthy Modify after cancellation validated %d objects, want 3", len(got))
	}
}
