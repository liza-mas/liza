package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
)

func TestReadSnapshotWhileWriterHoldsLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	reader, writer := New(path), New(path)
	if err := writer.Write(&models.State{Goal: models.Goal{Description: "published"}}); err != nil {
		t.Fatal(err)
	}
	// An independent instance/handle owns the lock, and its new state has not
	// been published yet. A snapshot must return the previous complete state.
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- writer.Modify(func(state *models.State) error {
			state.Goal.Description = "next"
			close(held)
			<-release
			return nil
		})
	}()
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not acquire lock")
	}
	result := make(chan error, 1)
	go func() {
		state, err := reader.ReadSnapshot()
		if err == nil && state.Goal.Description != "published" {
			err = fmt.Errorf("snapshot saw unpublished state: %q", state.Goal.Description)
		}
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot waited for the writer lock")
	}
}

func TestReadSnapshotFreshIndependentNormalizedAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	bb := New(path)
	if _, err := bb.ReadSnapshot(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing snapshot error = %v", err)
	}
	if err := os.WriteFile(path, []byte("tasks: ["), 0644); err != nil {
		t.Fatal(err)
	}
	var schemaErr *lizaerrors.StateSchemaError
	if _, err := bb.ReadSnapshot(); !errors.As(err, &schemaErr) {
		t.Fatalf("malformed snapshot error = %v", err)
	}
	if err := os.WriteFile(path, []byte("agents:\n  r1:\n    role: code_reviewer\ntasks:\n  - id: t1\n    attempted: [coder-1]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	locked, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot, locked) || snapshot.Agents["r1"].Role != "code-reviewer" {
		t.Fatal("snapshot and locked read normalization differ")
	}
	snapshot.Tasks[0].Description = "caller mutation"
	locked.Tasks[0].Description = "fresh publication"
	if err := bb.Write(locked); err != nil {
		t.Fatal(err)
	}
	fresh, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Tasks[0].Description != "fresh publication" || snapshot.Tasks[0].Description != "caller mutation" {
		t.Fatal("snapshot was cached or shared with another caller")
	}
}

func TestReadSnapshotConcurrentPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.yaml")
	writer := New(path).WithLockTimeout(time.Second)
	if err := writer.Write(&models.State{Goal: models.Goal{Description: "revision-0"}, Tasks: []models.Task{{ID: "t1"}}}); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader := New(path)
			<-start
			for range 100 {
				state, err := reader.ReadSnapshot()
				if err != nil {
					t.Error(err)
					return
				}
				if len(state.Tasks) != 1 || state.Goal.Description != fmt.Sprintf("revision-%d", state.Tasks[0].Iteration) {
					t.Error("snapshot mixed fields from different publications")
					return
				}
			}
		}()
	}
	close(start)
	for i := 1; i <= 40; i++ {
		if err := writer.Modify(func(state *models.State) error {
			state.Goal.Description = fmt.Sprintf("revision-%d", i)
			state.Tasks[0].Iteration = i
			return nil
		}); err != nil {
			t.Errorf("writer failed during inspection: %v", err)
			break
		}
	}
	wg.Wait()
	state, err := New(path).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if state.Tasks[0].Iteration != 40 {
		t.Fatalf("writer failed to progress: revision %d", state.Tasks[0].Iteration)
	}
}
