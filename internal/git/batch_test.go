package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestParseCatFileBatch_ContentMissingAndSubmodule(t *testing.T) {
	names := []string{"HEAD:a b.txt", "HEAD:gone", "HEAD:sub", "HEAD:x"}
	output := "1111 blob 5\nab\ncd\n" +
		"HEAD:gone missing\n" +
		"2222 submodule\n" +
		"3333 blob 0\n\n"
	got, err := parseCatFileBatch(names, []byte(output), true)
	if err != nil {
		t.Fatalf("parseCatFileBatch() error = %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[0].OID != "1111" || got[0].Type != "blob" || string(got[0].Content) != "ab\ncd" {
		t.Errorf("first = %+v", got[0])
	}
	if !got[1].Missing || got[1].Status != "missing" {
		t.Errorf("second = %+v, want missing", got[1])
	}
	if !got[2].Missing || got[2].Status != "submodule" || got[2].OID != "2222" {
		t.Errorf("third = %+v, want submodule", got[2])
	}
	if got[3].Size != 0 || len(got[3].Content) != 0 || got[3].Missing {
		t.Errorf("fourth = %+v, want empty blob", got[3])
	}
}

func TestParseCatFileBatch_RejectsPartialOrMalformedOutput(t *testing.T) {
	cases := map[string]struct {
		names       []string
		output      string
		withContent bool
	}{
		"missing answer":         {names: []string{"a", "b"}, output: "1111 blob 1\n"},
		"truncated content":      {names: []string{"a"}, output: "1111 blob 10\nshort\n", withContent: true},
		"unterminated content":   {names: []string{"a"}, output: "1111 blob 2\nabX", withContent: true},
		"malformed header":       {names: []string{"a"}, output: "fatal: something\n"},
		"bad size":               {names: []string{"a"}, output: "1111 blob -3\n"},
		"trailing bytes":         {names: []string{"a"}, output: "1111 blob 1\nextra\n"},
		"header without newline": {names: []string{"a"}, output: "1111 blob 1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := parseCatFileBatch(tc.names, []byte(tc.output), tc.withContent); err == nil {
				t.Fatalf("parseCatFileBatch() = %+v, want error", got)
			}
		})
	}
}

func TestCatFileBatch_RejectsUnbatchableNames(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	for _, name := range []string{"", "HEAD:a\nb", "HEAD:a\rb"} {
		if _, err := g.catFileBatch([]string{name}, false); err == nil {
			t.Errorf("catFileBatch(%q) error = nil", name)
		}
	}
}

func TestBlobOIDs_MatchesPerLookupSemantics(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	writeAndCommit(t, repoDir, "docs/one.md", "one\n")
	first := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	writeAndCommit(t, repoDir, "docs/one.md", "one changed\n")
	second := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	results, err := g.BlobOIDs([]BlobPath{
		{Revision: first, Path: "docs/one.md"},
		{Revision: second, Path: "docs/one.md"},
		{Revision: second, Path: "missing.md"},
		{Revision: "no-such-ref", Path: "docs/one.md"},
		{Revision: second, Path: "docs"},
		{Revision: "", Path: "docs/one.md"},
		{Revision: second, Path: "line\nbreak.md"},
	})
	if err != nil {
		t.Fatalf("BlobOIDs() error = %v", err)
	}
	if len(results) != 7 {
		t.Fatalf("len = %d, want 7", len(results))
	}
	for i, rev := range []string{first, second} {
		want := testhelpers.MustGit(t, repoDir, "rev-parse", rev+":docs/one.md")
		if results[i].Err != nil || results[i].OID != want {
			t.Errorf("results[%d] = %+v, want OID %s", i, results[i], want)
		}
	}
	if results[0].OID == results[1].OID {
		t.Error("distinct contents resolved to the same blob")
	}
	for i := 2; i < 7; i++ {
		if results[i].Err == nil || results[i].OID != "" {
			t.Errorf("results[%d] = %+v, want per-entry error", i, results[i])
		}
	}
	if !strings.Contains(results[4].Err.Error(), "not a blob") {
		t.Errorf("tree path error = %v, want blob-type rejection", results[4].Err)
	}

	// The single-entry wrapper reports the same answers.
	oid, err := g.BlobOID(second, "docs/one.md")
	if err != nil || oid != results[1].OID {
		t.Errorf("BlobOID() = %q, %v; want %q", oid, err, results[1].OID)
	}
	if _, err := g.BlobOID(second, "missing.md"); err == nil {
		t.Error("BlobOID(missing) error = nil")
	}
}

func TestReadBlob_ExactBinaryBytesAndFallback(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	binary := "\x00\x01 lead\r\n\nmiddle\n\x00tail  \n\n"
	writeAndCommit(t, repoDir, "bin.dat", binary)
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	got, err := g.ReadBlob(head, "bin.dat")
	if err != nil {
		t.Fatalf("ReadBlob() error = %v", err)
	}
	if got != binary {
		t.Errorf("ReadBlob() = %q, want %q", got, binary)
	}
	// A name the batch stdin cannot carry takes the argument-based route and
	// still fails cleanly for a path that does not exist.
	if _, err := g.ReadBlob(head, "no\nsuch"); err == nil {
		t.Error("ReadBlob(newline path) error = nil")
	}
	if _, err := g.ReadBlob("no-such-ref", "bin.dat"); err == nil {
		t.Error("ReadBlob(bad ref) error = nil")
	}
}

func TestTreeEntryAtAndReadBlobObject(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	content := "entry body\n"
	writeAndCommit(t, repoDir, "dir/entry.md", content)
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")

	entry, present, err := g.TreeEntryAt(head, "dir/entry.md")
	if err != nil || !present {
		t.Fatalf("TreeEntryAt() = %+v, %v, %v", entry, present, err)
	}
	wantOID := testhelpers.MustGit(t, repoDir, "rev-parse", head+":dir/entry.md")
	if entry.Mode != "100644" || entry.Type != "blob" || entry.OID != wantOID || entry.Size != int64(len(content)) {
		t.Errorf("TreeEntryAt() = %+v, want 100644 blob %s %d", entry, wantOID, len(content))
	}
	body, err := g.ReadBlobObject(entry.OID)
	if err != nil || body != content {
		t.Errorf("ReadBlobObject() = %q, %v; want %q", body, err, content)
	}

	dir, present, err := g.TreeEntryAt(head, "dir")
	if err != nil || !present || dir.Type != "tree" || dir.Size != -1 {
		t.Errorf("TreeEntryAt(dir) = %+v, %v, %v; want unsized tree", dir, present, err)
	}
	if _, present, err := g.TreeEntryAt(head, "absent.md"); err != nil || present {
		t.Errorf("TreeEntryAt(absent) present=%v err=%v", present, err)
	}
	if _, _, err := g.TreeEntryAt("no-such-ref", "dir/entry.md"); err == nil {
		t.Error("TreeEntryAt(bad ref) error = nil")
	}
	if _, _, err := g.TreeEntryAt(head, ""); err == nil {
		t.Error("TreeEntryAt(empty path) error = nil")
	}
	if _, err := g.ReadBlobObject(dir.OID); err == nil {
		t.Error("ReadBlobObject(tree) error = nil")
	}
	for _, bad := range []string{"", "--batch"} {
		if _, err := g.ReadBlobObject(bad); err == nil {
			t.Errorf("ReadBlobObject(%q) error = nil", bad)
		}
	}
}

func writeAndCommit(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	full := filepath.Join(repoDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, repoDir, "add", "--", rel)
	testhelpers.MustGit(t, repoDir, "commit", "-m", "write "+rel)
}

func TestResolveCommits_MatchesResolveCommit(t *testing.T) {
	repoDir := setupTestRepo(t)
	g := New(repoDir)
	writeAndCommit(t, repoDir, "a.txt", "a\n")
	head := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD")
	tree := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD^{tree}")
	blob := testhelpers.MustGit(t, repoDir, "rev-parse", "HEAD:a.txt")

	refs := []string{head, "HEAD", "--help", "no-such-ref", "", blob, tree, "HEAD\nHEAD"}
	results, err := g.ResolveCommits(refs)
	if err != nil {
		t.Fatalf("ResolveCommits() error = %v", err)
	}
	if len(results) != len(refs) {
		t.Fatalf("len = %d, want %d", len(results), len(refs))
	}
	for i, ref := range refs {
		want, wantErr := g.ResolveCommit(ref)
		if (results[i].Err == nil) != (wantErr == nil) || results[i].OID != want {
			t.Errorf("ResolveCommits()[%d] for %q = %+v; ResolveCommit = %q, %v", i, ref, results[i], want, wantErr)
		}
	}
	// A tree's commit peel fails; a tree is not a commit.
	if results[6].Err == nil {
		t.Error("tree resolved as a commit")
	}
	if results[0].OID != head || results[1].OID != head {
		t.Errorf("head results = %+v, %+v; want %s", results[0], results[1], head)
	}
}

func TestCatFileBatch_ErrorCarriesGitStderr(t *testing.T) {
	_, err := New(t.TempDir()).catFileBatch([]string{"HEAD"}, false)
	if err == nil {
		t.Fatal("catFileBatch outside a repository succeeded")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error lacks git's stderr: %v", err)
	}
}
