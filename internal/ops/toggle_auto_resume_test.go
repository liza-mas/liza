package ops

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestC6ToggleAutoResumeInvertsAtomicallyAndReturnsCommittedValue(t *testing.T) {
	statePath, _ := testhelpers.SetupLizaDir(t, t.TempDir())
	state := testhelpers.CreateValidState()
	state.Config.AutoResume = false
	testhelpers.WriteInitialState(t, statePath, state)
	bb := db.For(statePath)
	before, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}

	for i, want := range []bool{true, false, true} {
		got, err := ToggleAutoResume(bb)
		if err != nil {
			t.Fatalf("toggle %d: %v", i, err)
		}
		if got != want {
			t.Fatalf("toggle %d returned %v, want %v", i, got, want)
		}
		persisted, err := bb.ReadSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if persisted.Config.AutoResume != want {
			t.Fatalf("toggle %d persisted %v, want the returned %v", i, persisted.Config.AutoResume, want)
		}
	}

	after, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after.MutationSequence != before.MutationSequence+3 {
		t.Fatalf("mutation sequence advanced by %d, want one per toggle", after.MutationSequence-before.MutationSequence)
	}
	after.Config.AutoResume = before.Config.AutoResume
	after.MutationSequence = before.MutationSequence
	if !reflect.DeepEqual(before, after) {
		t.Fatal("toggle changed state other than config.auto_resume (extra history or records)")
	}
}

func TestC6ToggleAutoResumeReturnsTransactionError(t *testing.T) {
	bb := db.For(filepath.Join(t.TempDir(), "missing", "state.yaml"))
	got, err := ToggleAutoResume(bb)
	if err == nil {
		t.Fatal("expected an error for an unreadable state")
	}
	if got {
		t.Fatal("a failed toggle must not report ON")
	}
}
