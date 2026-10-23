package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readTree(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestLiveMissingGoldenFailsAndIsNotCreated(t *testing.T) {
	root := t.TempDir()
	msg := expectFailure(t, func(tb testing.TB) {
		compareGolden(tb, root, false, Human, "help.txt", []byte("x\n"))
	})
	for _, want := range []string{filepath.Join("testdata", "golden", "human", "help.txt"), "missing", "human"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if files := readTree(t, root); len(files) != 0 {
		t.Errorf("live mode created %v", files)
	}
}

func TestLiveMatchPasses(t *testing.T) {
	root := t.TempDir()
	compareGolden(t, root, true, JSON, "a/b.json", []byte("{}\n"))
	compareGolden(t, root, false, JSON, "a/b.json", []byte("{}\n"))
}

func TestLiveMismatchNamesPathKindAndFirstDifferingLine(t *testing.T) {
	root := t.TempDir()
	compareGolden(t, root, true, Prompt, "p.txt", []byte("one\ntwo\nthree\n"))
	msg := expectFailure(t, func(tb testing.TB) {
		compareGolden(tb, root, false, Prompt, "p.txt", []byte("one\nTWO\nthree\n"))
	})
	for _, want := range []string{"p.txt", "prompt", "line 2", `"two"`, `"TWO"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
}

func TestMismatchTruncatesLongLinesAndReportsEOF(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 500)
	compareGolden(t, root, true, Artifact, "a.bin", []byte(long+"\n"))
	msg := expectFailure(t, func(tb testing.TB) {
		compareGolden(tb, root, false, Artifact, "a.bin", []byte(long[:400]+"y\n"))
	})
	if strings.Contains(msg, strings.Repeat("x", 201)) {
		t.Errorf("line not truncated to 200 bytes: %d chars message", len(msg))
	}
	msg = expectFailure(t, func(tb testing.TB) {
		compareGolden(tb, root, false, Artifact, "a.bin", []byte(long))
	})
	if !strings.Contains(msg, "<EOF>") {
		t.Errorf("a missing final newline must report <EOF>, got %q", msg)
	}
}

func TestWriteModeCreatesAndOverwritesAndNeverFailsOnContent(t *testing.T) {
	root := t.TempDir()
	compareGolden(t, root, true, State, "deep/dir/s.yaml", []byte("first"))
	p := filepath.Join(root, "testdata", "golden", "state", "deep", "dir", "s.yaml")
	if b, err := os.ReadFile(p); err != nil || string(b) != "first" {
		t.Fatalf("write mode did not create the golden: %q, %v", b, err)
	}
	compareGolden(t, root, true, State, "deep/dir/s.yaml", []byte("second"))
	if b, _ := os.ReadFile(p); string(b) != "second" {
		t.Errorf("write mode did not overwrite, got %q", b)
	}
}

func TestGoldenNameAndKindValidation(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"", "../x", "a/../../x", "/abs", "a//b", "./a", `a\b`, "a/"} {
		msg := expectFailure(t, func(tb testing.TB) { compareGolden(tb, root, true, JSON, name, nil) })
		if !strings.Contains(msg, "golden name") {
			t.Errorf("name %q: unexpected message %q", name, msg)
		}
	}
	expectFailure(t, func(tb testing.TB) { compareGolden(tb, root, true, Kind("other"), "x", nil) })
	if files := readTree(t, root); len(files) != 0 {
		t.Errorf("rejected names created %v", files)
	}
}

func TestCheckUsesPackageDirectoryAndWriteEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	msg := expectFailure(t, func(tb testing.TB) { Check(tb, Human, "h.txt", []byte("a  b\n")) })
	if !strings.Contains(msg, "missing") {
		t.Errorf("live Check on a missing golden: %q", msg)
	}
	t.Setenv(WriteEnv, "1")
	Check(t, Human, "h.txt", []byte("a  b\n"))
	CheckState(t, "s.yaml", []byte("a: 1\n"), []byte("a: 2\n"))
	t.Setenv(WriteEnv, "")
	Check(t, Human, "h.txt", []byte("a b\n"))
	CheckState(t, "s.yaml", []byte("a: 1\n"), []byte("a: 2\n"))
	expectFailure(t, func(tb testing.TB) { Check(tb, Human, "h.txt", []byte("a c\n")) })
}
