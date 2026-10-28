package oracle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootOnlyBytes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const unchanged = "\t2026-10-23T18:40:00+02:00 2026-10-23 18:40:00.5 -0500\r\n" +
		"123e4567-e89b-12d3-a456-426614174000\n" +
		"0123456789abcdef0123456789abcdef01234567\n" +
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n" +
		"/tmp/other-case/a  /var/tmp/other-case/b\x1b[31mred\x1b[0m\xff\xfe"
	for _, ending := range []string{"", "\n", "\r\n"} {
		in := "root=" + root + "/project\nagain=(" + root + ")\nbackslash=" + root + `\project\file.txt` + unchanged + ending
		want := "root=<ROOT>/project\nagain=(<ROOT>)\nbackslash=<ROOT>\\project\\file.txt" + unchanged + ending
		if got := ReplaceRoot(in, root, "<ROOT>"); got != want {
			t.Errorf("root substitution changed unrelated bytes (ending %q): got %q, want %q", ending, got, want)
		}
	}
	// The helper matches the supplied spelling only, without separator cleanup.
	if got := ReplaceRoot(`C:\case\project\a`, `C:\case`, "<CASE>"); got != `<CASE>\project\a` {
		t.Errorf("literal Windows spelling: got %q", got)
	}
	if got := ReplaceRoot("/case/x /case", "/case", ""); got != "/x " {
		t.Errorf("empty replacement token: got %q", got)
	}
}

func TestRootOnlyAbsentAndEmpty(t *testing.T) {
	const root = "/case/root"
	for _, in := range []string{
		"", "/another/root", "prefix/case/root", "/case/rootSibling/a",
	} {
		if got := ReplaceRoot(in, root, "<ROOT>"); got != in {
			t.Errorf("nonmatching root: got %q, want %q", got, in)
		}
	}
	for _, name := range "aZ09_.-" {
		for _, in := range []string{string(name) + root, root + string(name), root + string(name) + "/child"} {
			if got := ReplaceRoot(in, root, "<ROOT>"); got != in {
				t.Errorf("glued name byte: got %q, want %q", got, in)
			}
		}
	}
	const mixed = "x/case/root /case/root-other /case/root/child /case/root"
	const want = "x/case/root /case/root-other <ROOT>/child <ROOT>"
	if got := ReplaceRoot(mixed, root, "<ROOT>"); got != want {
		t.Errorf("matches after rejected occurrences: got %q, want %q", got, want)
	}
	const input = "\xff /case/root\t2026-10-23T18:40:00Z\r\n"
	if got := ReplaceRoot(input, "", "<ROOT>"); got != input {
		t.Errorf("empty root must be a no-op: got %q", got)
	}
}

func TestRootOnlyGoldenComparison(t *testing.T) {
	t.Chdir(t.TempDir())
	const original = "root=<ROOT>/project\n2026-10-23T18:40:00+02:00\n0123456789abcdef0123456789abcdef01234567\n\tkeep  spaces\r\n\x1b[31mred\x1b[0m\xff\n"
	const input = "root=/case/first/project\n2026-10-23T18:40:00+02:00\n0123456789abcdef0123456789abcdef01234567\n\tkeep  spaces\r\n\x1b[31mred\x1b[0m\xff\n"
	t.Setenv(WriteEnv, "1")
	Check(t, Prompt, "root.txt", []byte(ReplaceRoot(input, "/case/first", "<ROOT>")))
	golden := filepath.Join("testdata", "golden", "prompt", "root.txt")
	assertGolden := func() {
		t.Helper()
		got, err := os.ReadFile(golden)
		if err != nil || string(got) != original {
			t.Fatalf("golden bytes changed: got %q, error %v", got, err)
		}
	}
	assertGolden()
	t.Setenv(WriteEnv, "")
	live := strings.ReplaceAll(input, "/case/first", "/case/second")
	Check(t, Prompt, "root.txt", []byte(ReplaceRoot(live, "/case/second", "<ROOT>")))
	for _, mutation := range []struct{ name, from, to string }{
		{"timestamp", "18:40:00", "18:40:01"},
		{"sha", "0123456789abcdef0123456789abcdef01234567", "0123456789abcdef0123456789abcdef01234568"},
		{"whitespace", "keep  spaces", "keep spaces"},
		{"final-newline", "red\x1b[0m\xff\n", "red\x1b[0m\xff"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			changed := strings.Replace(live, mutation.from, mutation.to, 1)
			msg := expectFailure(t, func(tb testing.TB) {
				Check(tb, Prompt, "root.txt", []byte(ReplaceRoot(changed, "/case/second", "<ROOT>")))
			})
			for _, want := range []string{"root.txt", "kind prompt", "differs at line"} {
				if !strings.Contains(msg, want) {
					t.Errorf("expected content mismatch, got %q", msg)
				}
			}
			assertGolden()
		})
	}
}
