//go:build linux

package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type digest struct {
	ArtifactID   string `json:"artifact_id"`
	ContentHash  string `json:"content_hash"`
	ArtifactPath string `json:"artifact_path"`
	StoredBytes  int    `json:"stored_bytes"`
}

// Shared normalisation masks hex identifiers. Independently retain the stable
// content-addressing semantics, so changing a digest cannot pass under <ID>.
func (f *fixture) retained(result oracle.Result, content []byte) string {
	f.t.Helper()
	d := decode[digest](f.t, result.Stdout)
	want := fmt.Sprintf("%x", sha256.Sum256(content))
	if d.ArtifactID != want || d.ContentHash != want || d.StoredBytes != len(content) || d.ArtifactPath != filepath.Join(f.store, want+".txt") {
		f.t.Fatal("digest does not identify the complete fixture content")
	}
	stored, err := os.ReadFile(d.ArtifactPath)
	must(f.t, err)
	if !bytes.Equal(stored, content) {
		f.t.Fatal("stored bytes differ from committed fixture")
	}
	return d.ArtifactID
}

func TestOracleA2ToolResultFilter(t *testing.T) {
	for _, scenario := range []struct {
		name, file string
		kind       oracle.Kind
		extra      []string
	}{
		{"small", "small.txt", oracle.Human, nil},
		{"large", "large.txt", oracle.JSON, nil},
		{"envelope", "envelope.json", oracle.Human, []string{"--json-input"}},
		{"redaction", "redaction.txt", oracle.JSON, nil},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newFixture(t)
			args := f.resultArgs("filter", "--tool", "Read", "--command", "fixture read", "--session-id", "fixture-session")
			result := f.run(input(t, "tool-result/"+scenario.file), append(args, scenario.extra...)...)
			f.check("tool-result-filter/"+scenario.name, scenario.kind, result, 0)
			if scenario.name == "large" {
				f.retained(result, input(t, "tool-result/large.txt"))
			}
			if scenario.name == "redaction" {
				// This fixture is deliberately synthetic and contains no credential.
				token := []byte("sk-proj-" + strings.Repeat("ORACLEFIXTURE", 4))
				if bytes.Contains(result.Stdout, token) {
					t.Fatal("synthetic token reached stdout")
				}
				must(t, filepath.WalkDir(f.store, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if entry.IsDir() {
						return nil
					}
					b, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					if bytes.Contains(b, token) {
						t.Error("synthetic token reached store")
					}
					return nil
				}))
			}
			if scenario.name == "large" || scenario.name == "redaction" {
				f.fileEffects("tool-result-filter/"+scenario.name, map[string]string{"store": f.store})
			}
		})
	}
	t.Run("budget-refusal", func(t *testing.T) {
		f := newFixture(t)
		f.check("tool-result-filter/budget-refusal", oracle.Human, f.run(input(t, "tool-result/small.txt"), "tool-result", "--root", f.store, "--threshold-bytes", "1023", "--digest-bytes", "1024", "filter", "--tool", "Read"), 1)
		f.fileEffects("tool-result-filter/budget-refusal", map[string]string{"store": f.store})
	})
}

func (f *fixture) seedLarge() string {
	f.t.Helper()
	b := input(f.t, "tool-result/large.txt")
	result := f.run(b, f.resultArgs("filter", "--tool", "Read", "--command", "fixture read")...)
	if result.ExitCode != 0 {
		f.t.Fatal("large fixture seed failed")
	}
	return f.retained(result, b)
}

func TestOracleA2ToolResultRead(t *testing.T) {
	t.Run("range", func(t *testing.T) {
		f := newFixture(t)
		id := f.seedLarge()
		f.check("tool-result-read/range", oracle.Human, f.run(nil, f.resultArgs("read", id, "--offset", "8", "--limit", "64")...), 0)
	})
	t.Run("unknown", func(t *testing.T) {
		f := newFixture(t)
		f.seedLarge()
		// Well formed but absent: this reaches lookup, not hash validation.
		f.check("tool-result-read/unknown", oracle.Human, f.run(nil, f.resultArgs("read", strings.Repeat("0", 64), "--limit", "64")...), 1)
	})
}

func TestOracleA2ToolResultStats(t *testing.T) {
	f := newFixture(t)
	result := f.run(input(t, "tool-result/small.txt"), f.resultArgs("filter", "--tool", "Read")...)
	if result.ExitCode != 0 {
		t.Fatal("small fixture seed failed")
	}
	f.seedLarge()
	f.seedLarge()
	f.check("tool-result-stats/seeded", oracle.JSON, f.run(nil, f.resultArgs("stats")...), 0)
}
