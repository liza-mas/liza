// Package persist freezes A1 persistence at its exported boundaries. Fixed
// timestamps and identifiers deliberately bypass blanket normalisation.
package persist

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type alertInput struct {
	At, Level, Category, Message, KeyKind, Task, Episode, Ask string
	Index                                                     int
}

type alertRecord struct {
	Accepted                     bool
	At, Level, Category, Message string
}

type usageInput struct {
	SchemaVersion                                                                   int
	RecordID, Task, Role, Agent, Run, Session, Provider, Started, Ended, Provenance string
	Fresh, CacheRead, CacheWrite, Output, Exit                                      int
	Warm                                                                            bool
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	return readBytes(t, filepath.Join("testdata", "fixtures", name))
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}
	return b
}

func writeBytes(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func decodeFixture(t *testing.T, name string, target any) {
	t.Helper()
	if err := json.Unmarshal(fixture(t, name), target); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

func fixedTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }

func jsonBytes(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

// rawStateCheck supplies AC1's local raw comparator. Neither side passes
// through the harness normaliser, which would erase the fixed C5 timestamps.
func rawStateCheck(t *testing.T, name string, before, after []byte) {
	t.Helper()
	got := append([]byte("# before\n"), before...)
	got = append(got, []byte("\n# after\n")...)
	got = append(got, after...)
	// The observation owns its final newline; adding one would both mask a
	// missing terminator and create an EOF-hook conflict for normal writes.
	path := filepath.Join("testdata", "golden", "state", name)
	if os.Getenv(oracle.WriteEnv) == "1" {
		writeBytes(t, path, got)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("golden %s missing or unreadable", path)
		return
	}
	if !bytes.Equal(want, got) {
		t.Errorf("golden %s differs (exact state bytes)", path)
	}
}

type fileObservation struct {
	Path      string
	Directory bool
	Content   string
}

// WalkDir sorts paths. Keep unexpected files and directories observable;
// exclude only the exact lock files named by the scenario's boundary.
func snapshot(t *testing.T, root string, locks ...string) []byte {
	t.Helper()
	entries := []fileObservation{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, lock := range locks {
			if rel == lock || rel == lock+".owner.json" {
				return nil
			}
		}
		entry := fileObservation{Path: rel, Directory: d.IsDir()}
		if !d.IsDir() {
			entry.Content = string(readBytes(t, path))
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return jsonBytes(t, entries)
}

func requireSame(t *testing.T, label string, before, after []byte) {
	t.Helper()
	if !bytes.Equal(before, after) {
		t.Fatalf("%s changed", label)
	}
}

func requireRefusal(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected refusal", operation)
	}
}

func readLines(b []byte) []string { return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") }
