package providersignal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
)

func quotaGenerationRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, paths.ProjectDirName()), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// GIVEN a quota signal
// WHEN it is read repeatedly, and then re-raised with the same announced reset
// THEN reads agree on one generation, the re-raise is a new generation, and
// the event carries the provider, detection and message.
func TestQuotaSignalEvent_GenerationIsTheWriteNotTheReset(t *testing.T) {
	root := quotaGenerationRoot(t)
	resetsAt := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	detected := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := writeQuotaSignal(root, "codex-acp", "limit hit", resetsAt, detected); err != nil {
		t.Fatal(err)
	}

	first, ok := QuotaSignalEvent(root, "codex")
	if !ok {
		t.Fatal("event not found for an existing signal")
	}
	again, _ := QuotaSignalEvent(root, "codex-acp")
	if first != again {
		t.Fatalf("reads disagree: %+v vs %+v", first, again)
	}
	if first.Provider != "codex" || !first.Detected.Equal(detected) || first.Message != "limit hit" || first.Generation == "" || first.Generation == QuotaEventUnreadable {
		t.Fatalf("event = %+v", first)
	}

	if err := writeQuotaSignal(root, "codex", "limit hit", resetsAt, detected.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	reraised, _ := QuotaSignalEvent(root, "codex")
	if reraised.Generation == first.Generation {
		t.Fatalf("a re-raise with the same reset kept generation %s", first.Generation)
	}
}

// GIVEN no signal, an unreadable signal, and a legacy signal without detected
// WHEN its event is read
// THEN absent is not found, unreadable is the unreadable generation with no
// detection, and legacy falls back to the file's mtime.
func TestQuotaSignalEvent_AbsentUnreadableAndLegacy(t *testing.T) {
	root := quotaGenerationRoot(t)
	if _, ok := QuotaSignalEvent(root, "codex"); ok {
		t.Fatal("event found without a signal")
	}

	if err := os.MkdirAll(filepath.Join(QuotaSignalPath(root, "claude"), "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable, ok := QuotaSignalEvent(root, "claude")
	if !ok || unreadable.Generation != QuotaEventUnreadable || !unreadable.Detected.IsZero() {
		t.Fatalf("unreadable event = %+v, %t", unreadable, ok)
	}

	legacy := QuotaSignalPath(root, "cursor")
	if err := os.WriteFile(legacy, []byte("provider: cursor\nmessage: old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2029, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(legacy, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	event, ok := QuotaSignalEvent(root, "cursor")
	if !ok || !event.Detected.Equal(mtime) || event.Message != "old" {
		t.Fatalf("legacy event = %+v, %t", event, ok)
	}
}
