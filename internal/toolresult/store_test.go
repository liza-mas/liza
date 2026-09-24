package toolresult

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir(), Config{ThresholdBytes: 2048, DigestBytes: 1024}, []string{"fake-known-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func parseDigest(t *testing.T, output string) Digest {
	t.Helper()
	var digest Digest
	if err := json.Unmarshal([]byte(output), &digest); err != nil {
		t.Fatalf("invalid digest: %v", err)
	}
	return digest
}

func TestStoreThresholdAndMetadata(t *testing.T) {
	s := testStore(t)
	for _, size := range []int{0, 2047, 2048} {
		input := strings.Repeat("x", size)
		out, err := s.Process(Result{Tool: "read", Command: "sed -n '1,40p' src/main.go", Content: input})
		if err != nil || out != input {
			t.Fatalf("size %d: small read changed: %v", size, err)
		}
	}
	exit := 1
	input := strings.Repeat("FAIL test_checkout\n", 15000) + "api_key='fixture sensitive value'\nfake-known-secret\n"
	out, err := s.Process(Result{Tool: "exec", Command: "pytest -q --token=fake-known-secret", ExitCode: &exit, Truncated: true, Content: input, TaskID: "task-778", AgentID: "coder-1", SessionID: "session-778"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 1024 {
		t.Fatalf("digest exceeds bound: %d", len(out))
	}
	d := parseDigest(t, out)
	if d.Tool != "exec" || d.ExitCode == nil || *d.ExitCode != 1 || !d.Truncated || !d.SourceTruncated || d.OriginalBytes != len(input) || d.Duplicate || d.ArtifactID != d.ContentHash {
		t.Fatalf("metadata lost: %+v", d)
	}
	full, err := os.ReadFile(d.ArtifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(full), "fake-known-secret") || strings.Contains(string(full), "fixture sensitive value") || strings.Contains(out, "fake-known-secret") {
		t.Fatal("secret escaped")
	}
	if fmt.Sprintf("%x", sha256.Sum256(full)) != d.ContentHash {
		t.Fatal("hash mismatch")
	}
	got, err := s.Read(d.ContentHash, 8, 40)
	if err != nil || got != string(full[8:48]) {
		t.Fatalf("targeted retrieval: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(s.path, "events"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		b, _ := os.ReadFile(filepath.Join(s.path, "events", entry.Name()))
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			t.Fatal(err)
		}
		if e.TaskID == "task-778" {
			found = e.AgentID == "coder-1" && e.SessionID == "session-778" && e.OriginalBytes == len(input)
		}
	}
	if !found {
		t.Fatal("usage join identity missing")
	}
}

func TestStoreDuplicateAcrossRestartAndLineEndings(t *testing.T) {
	s := testStore(t)
	one, err := s.Process(Result{Tool: "exec", Content: strings.Repeat("failure\r\n", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(s.path, s.config, nil)
	if err != nil {
		t.Fatal(err)
	}
	two, err := restarted.Process(Result{Tool: "read", Content: strings.Repeat("failure\n", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	d1, d2 := parseDigest(t, one), parseDigest(t, two)
	if d1.ContentHash != d2.ContentHash || !d2.Duplicate || d2.Excerpt != "" || d2.Tool != "read" {
		t.Fatalf("not deduplicated: %+v", d2)
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 2 || stats.ExternalizedCount != 2 || stats.DuplicateCount != 1 || stats.P95ResultBytes != 9000 || stats.DuplicateBytesPrevented != int64(8000-len(two)) {
		t.Fatalf("stats mismatch: %+v", stats)
	}
}

func TestStoreConcurrentPublication(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Process(Result{Tool: "exec", Content: strings.Repeat("failure\n", 1000)})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	stats, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Count != 8 || stats.DuplicateCount != 7 {
		t.Fatalf("concurrent publication: %+v", stats)
	}
}

func TestStoreFailClosedAndReadBounds(t *testing.T) {
	s := testStore(t)
	output, err := s.Process(Result{Tool: "exec", Command: strings.Repeat("x", 2000), Content: strings.Repeat("x", 4000)})
	if err != nil {
		t.Fatalf("oversized command rejected: %v", err)
	}
	digest := parseDigest(t, output)
	if !strings.HasSuffix(digest.Command, "…") || digest.ArtifactPath == "" {
		t.Fatalf("oversized command did not retain a marked digest: %+v", digest)
	}
	for _, hash := range []string{"../../outside", strings.Repeat("a", 64)} {
		if _, err := s.Read(hash, 0, 100); err == nil {
			t.Fatal("invalid read accepted")
		}
	}
	if _, err := s.Read(strings.Repeat("a", 64), 0, 65537); err == nil {
		t.Fatal("unbounded read accepted")
	}
	input := strings.Repeat("x", 4000)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
	if err := os.WriteFile(filepath.Join(s.path, hash+".txt"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Process(Result{Tool: "exec", Content: input}); err == nil {
		t.Fatal("corrupt artifact referenced")
	}
}

func TestDigestBoundsWithEscapingAndUnicode(t *testing.T) {
	s := testStore(t)
	out, err := s.Process(Result{Tool: "exec", Content: strings.Repeat("\"\n日本語🙂", 1000)})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 1024 {
		t.Fatal("escaped digest exceeded budget")
	}
	parseDigest(t, out)
}
