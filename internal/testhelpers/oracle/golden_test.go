package oracle

import (
	"fmt"
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

// storageRel is the slash-separated storage name goldenPath gives a logical
// golden name.
func storageRel(t *testing.T, root string, kind Kind, name string) string {
	t.Helper()
	p, err := goldenPath(root, kind, name)
	if err != nil {
		t.Fatalf("goldenPath(%q): %v", name, err)
	}
	rel, err := filepath.Rel(filepath.Join(root, "testdata", "golden", string(kind)), p)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

func TestGoldenStorageNames(t *testing.T) {
	for _, tc := range []struct{ logical, storage string }{
		{"plain.txt", "plain.txt"},
		{"a/b-c_d.json", "a/b-c_d.json"},
		{"store/<ID>.txt", "store/%3CID%3E.txt"},
		{"store/events/<EVENT-1>.json", "store/events/%3CEVENT-1%3E.json"},
		{"<ID>.txt", "%3CID%3E.txt"},
		{"100%.txt", "100%25.txt"},
		{"%3CID%3E.txt", "%253CID%253E.txt"},
		{`a:b"c|d?e*f`, "a%3Ab%22c%7Cd%3Fe%2Af"},
		{"ctl\x01\x1f.txt", "ctl%01%1F.txt"},
		{"é/ü.txt", "é/ü.txt"},
	} {
		t.Run(tc.logical, func(t *testing.T) {
			got := storageRel(t, "r", JSON, tc.logical)
			if got != tc.storage {
				t.Errorf("storage name of %q = %q, want %q", tc.logical, got, tc.storage)
			}
			if problem := WindowsPathProblem(got); problem != "" {
				t.Errorf("storage name %q is not Windows-safe: %s", got, problem)
			}
		})
	}
}

func TestGoldenStorageNamesAreCollisionSafe(t *testing.T) {
	root := t.TempDir()
	const (
		logical = "<ID>.txt"
		literal = "%3CID%3E.txt"
	)
	logicalBytes, literalBytes := []byte("logical\n"), []byte("literal\n")
	compareGolden(t, root, true, Artifact, logical, logicalBytes)
	compareGolden(t, root, true, Artifact, literal, literalBytes)

	if a, b := storageRel(t, root, Artifact, logical), storageRel(t, root, Artifact, literal); a == b {
		t.Fatalf("%q and %q share the storage name %q", logical, literal, a)
	}
	compareGolden(t, root, false, Artifact, logical, logicalBytes)
	compareGolden(t, root, false, Artifact, literal, literalBytes)
	expectFailure(t, func(tb testing.TB) { compareGolden(tb, root, false, Artifact, logical, literalBytes) })
	expectFailure(t, func(tb testing.TB) { compareGolden(tb, root, false, Artifact, literal, logicalBytes) })

	dir := filepath.Join(root, "testdata", "golden", "artifact")
	if _, err := os.Stat(filepath.Join(dir, logical)); !os.IsNotExist(err) {
		t.Errorf("a write landed at the raw logical name %q (stat error: %v)", logical, err)
	}
	if files := readTree(t, root); len(files) != 2 {
		t.Errorf("want exactly two stored goldens, got %v", files)
	}
}

func TestGoldenStorageNamesRefuseUnmappableNames(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"con", "NUL.txt", "dir/aux.json", "COM1", "lpt9.x", "trail.", "trail ", "a./b"} {
		msg := expectFailure(t, func(tb testing.TB) { compareGolden(tb, root, true, JSON, name, nil) })
		if !strings.Contains(msg, "golden name") {
			t.Errorf("name %q: unexpected message %q", name, msg)
		}
	}
	if files := readTree(t, root); len(files) != 0 {
		t.Errorf("refused names created %v", files)
	}
}

func TestWindowsPathProblem(t *testing.T) {
	for _, tc := range []struct {
		path      string
		component string // offending component, "" when the path is valid
	}{
		{"a/b/c.txt", ""},
		{"COM0.txt", ""},
		{"COMX", ""},
		{"com10", ""},
		{"console.txt", ""},
		{"a.b", ""},
		{"é/ü.txt", ""},
		{"a//b", ""},
		{"x/<ID>.txt", "<ID>.txt"},
		{"x/a:b", "a:b"},
		{`x/a"b`, `a"b`},
		{"x/a|b", "a|b"},
		{"x/a?b", "a?b"},
		{"x/a*b", "a*b"},
		{`x/a\b`, `a\b`},
		{"x/a\x01b", "a\x01b"},
		{"x/a.", "a."},
		{"x/a ", "a "},
		{"CON", "CON"},
		{"d/nul.txt", "nul.txt"},
		{"Prn.tar.gz", "Prn.tar.gz"},
		{"x/aux", "aux"},
		{"com1", "com1"},
		{"d/LPT9.log", "LPT9.log"},
	} {
		got := WindowsPathProblem(tc.path)
		if (got == "") != (tc.component == "") {
			t.Errorf("WindowsPathProblem(%q) = %q, want a problem: %v", tc.path, got, tc.component != "")
			continue
		}
		if tc.component != "" && !strings.Contains(got, fmt.Sprintf("%q", tc.component)) {
			t.Errorf("WindowsPathProblem(%q) = %q, which does not name component %q", tc.path, got, tc.component)
		}
	}
}
