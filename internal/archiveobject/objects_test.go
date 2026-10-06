package archiveobject

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestObjectsDurabilityRetryAndImmutableConflict(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "archive")
	data := []byte("immutable evidence\n")
	wantPath, err := Path(dir, Digest(data))
	if err != nil {
		t.Fatal(err)
	}
	barrierFailure := errors.New("directory barrier failed")
	if _, err := Write(dir, data, func(string) error { return barrierFailure }); !errors.Is(err, barrierFailure) {
		t.Fatalf("failed barrier = %v", err)
	}
	if saved, err := os.ReadFile(wantPath); err != nil || string(saved) != string(data) {
		t.Fatalf("installed unpublished object = %q, %v", saved, err)
	}
	var synced []string
	barrier := func(path string) error { synced = append(synced, path); return nil }
	if digest, err := Write(dir, data, barrier); err != nil || digest != Digest(data) {
		t.Fatalf("retry = %s, %v", digest, err)
	}
	if want := []string{filepath.Dir(wantPath), filepath.Join(dir, "objects"), dir, filepath.Dir(dir)}; !reflect.DeepEqual(synced, want) {
		t.Fatalf("retry barriers = %v, want %v", synced, want)
	}
	if err := os.WriteFile(wantPath, []byte("damaged evidence"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Write(dir, data, barrier); !errors.Is(err, ErrConflict) {
		t.Fatalf("existing differing object = %v", err)
	}
	if saved, err := os.ReadFile(wantPath); err != nil || string(saved) != "damaged evidence" {
		t.Fatalf("conflict overwrote evidence = %q, %v", saved, err)
	}
}

func TestObjectsPathRejectsUnsafeDigests(t *testing.T) {
	for _, digest := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), "../" + strings.Repeat("a", 61)} {
		if _, err := Path(t.TempDir(), digest); err == nil {
			t.Fatalf("accepted unsafe digest %q", digest)
		}
	}
}
