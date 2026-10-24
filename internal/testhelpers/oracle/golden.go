package oracle

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// WriteEnv is the environment variable that switches the harness from live
// mode (compare with the committed golden, never write) to write mode.
const WriteEnv = "ORACLE_WRITE"

const maxLineReport = 200

// goldenPath is the storage path of golden <name>: testdata/golden/<kind>/ plus
// the name mapped by goldenStorageName, under root. Write and live read share it.
func goldenPath(root string, kind Kind, name string) (string, error) {
	switch kind {
	case Prompt, Artifact, JSON, Human, State:
	default:
		return "", fmt.Errorf("oracle: unknown golden kind %q", kind)
	}
	if name == "" || name != path.Clean(name) || path.IsAbs(name) || strings.Contains(name, `\`) {
		return "", fmt.Errorf("oracle: golden name %q is not a clean relative slash path", name)
	}
	for _, elem := range strings.Split(name, "/") {
		if elem == ".." {
			return "", fmt.Errorf("oracle: golden name %q contains a parent element", name)
		}
	}
	storage := goldenStorageName(name)
	if problem := WindowsPathProblem(storage); problem != "" {
		return "", fmt.Errorf("oracle: golden name %q has no filesystem-safe storage name: %s", name, problem)
	}
	return filepath.Join(root, "testdata", "golden", string(kind), filepath.FromSlash(storage)), nil
}

// goldenStorageName maps a logical golden name to the name its file is stored
// under: %, the characters <>:"|?* and bytes below 0x20 become % plus two
// uppercase hex digits. The mapping is injective, so a logical name and a
// literal escape sequence never share a file. See golden-names.md.
func goldenStorageName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if c := name[i]; c == '%' || c < 0x20 || strings.IndexByte(`<>:"|?*`, c) >= 0 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Check normalises got per kind and compares it with the golden
// testdata/golden/<kind>/<name> of the calling package. Live mode fails on a
// missing golden or a difference and never writes; with ORACLE_WRITE=1 it
// writes the normalised bytes instead.
func Check(t testing.TB, kind Kind, name string, got []byte) {
	t.Helper()
	NewNormaliser().Check(t, kind, name, got)
}

// CheckState is the state-kind Check: a before/after pair of state.yaml.
func CheckState(t testing.TB, name string, before, after []byte) {
	t.Helper()
	NewNormaliser().CheckState(t, name, before, after)
}

// Check is the package-level Check with this normaliser.
func (n *Normaliser) Check(t testing.TB, kind Kind, name string, got []byte) {
	t.Helper()
	form, err := n.Form(kind, got)
	if err != nil {
		t.Fatal(err)
	}
	compareGolden(t, ".", os.Getenv(WriteEnv) == "1", kind, name, form)
}

// CheckState is the package-level CheckState with this normaliser.
func (n *Normaliser) CheckState(t testing.TB, name string, before, after []byte) {
	t.Helper()
	compareGolden(t, ".", os.Getenv(WriteEnv) == "1", State, name, n.StateForm(before, after))
}

func compareGolden(t testing.TB, root string, write bool, kind Kind, name string, form []byte) {
	t.Helper()
	p, err := goldenPath(root, kind, name)
	if err != nil {
		t.Fatal(err)
	}
	if write {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("oracle: create golden directory for %s: %v", p, err)
		}
		if err := os.WriteFile(p, form, 0o644); err != nil {
			t.Fatalf("oracle: write golden %s: %v", p, err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		t.Fatalf("oracle: golden %s (kind %s) is missing; live mode never creates goldens (generate with %s=1 on the starting commit)", p, kind, WriteEnv)
	}
	if err != nil {
		t.Fatalf("oracle: read golden %s: %v", p, err)
	}
	if !bytes.Equal(want, form) {
		line, wantLine, gotLine := firstDifference(want, form)
		t.Fatalf("oracle: golden %s (kind %s) differs at line %d:\n  golden: %s\n  actual: %s", p, kind, line, wantLine, gotLine)
	}
}

// firstDifference returns the 1-based number of the first differing line and
// both lines, truncated; a missing line reads <EOF>.
func firstDifference(want, got []byte) (int, string, string) {
	wl := bytes.Split(want, []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g []byte
		hasW, hasG := i < len(wl), i < len(gl)
		if hasW {
			w = wl[i]
		}
		if hasG {
			g = gl[i]
		}
		if hasW && hasG && bytes.Equal(w, g) {
			continue
		}
		return i + 1, describeLine(w, hasW), describeLine(g, hasG)
	}
	return 0, "", ""
}

func describeLine(line []byte, present bool) string {
	if !present {
		return "<EOF>"
	}
	if len(line) > maxLineReport {
		return fmt.Sprintf("%q...", line[:maxLineReport])
	}
	return fmt.Sprintf("%q", line)
}
