package archguard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAreasListsA1ToA9OnceInOrder(t *testing.T) {
	t.Parallel()
	if len(areas) != 9 {
		t.Fatalf("len(areas) = %d, want 9", len(areas))
	}
	seen := map[*Area]bool{}
	for i, a := range areas {
		want := AreaID(fmt.Sprintf("A%d", i+1))
		if a == nil || a.ID != want {
			t.Fatalf("areas[%d] = %+v, want ID %s", i, a, want)
		}
		if seen[a] {
			t.Errorf("%s listed twice", want)
		}
		seen[a] = true
	}
}

func TestAreaFileNamesAnExistingDataFile(t *testing.T) {
	t.Parallel()
	for _, a := range areas {
		rel := areaFile(a.ID)
		if !strings.HasPrefix(rel, "internal/testguard/archguard/area_a") || !strings.HasSuffix(rel, "_test.go") {
			t.Errorf("areaFile(%s) = %q, want an area_aN_test.go path", a.ID, rel)
		}
		if _, err := os.Stat(filepath.Join(repoRoot(), filepath.FromSlash(rel))); err != nil {
			t.Errorf("areaFile(%s) = %q does not exist", a.ID, rel)
		}
	}
}

func TestStartSnapshotShape(t *testing.T) {
	t.Parallel()
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(startCommit) {
		t.Errorf("startCommit = %q, want a full SHA", startCommit)
	}
	var files, funcs int
	for k, lines := range startSnapshot {
		if k.Func == "" {
			files++
		} else {
			funcs++
		}
		if lines <= threshold(k) {
			t.Errorf("snapshot %s = %d lines, not above its limit %d", k, lines, threshold(k))
		}
		if filepath.ToSlash(k.File) != k.File || filepath.IsAbs(k.File) || !strings.HasSuffix(k.File, ".go") || strings.HasSuffix(k.File, "_test.go") {
			t.Errorf("snapshot key %s is not a repo-relative production file path", k)
		}
	}
	if files != 33 || funcs != 93 {
		t.Fatalf("snapshot has %d file keys and %d function keys, want 33 and 93", files, funcs)
	}
}

func TestEveryRatchetKeyIsInTheSnapshotOnceAndPlacedInOneArea(t *testing.T) {
	t.Parallel()
	seen := map[Key]AreaID{}
	for _, a := range areas {
		for _, r := range a.Ratchet {
			if _, ok := startSnapshot[r.Key]; !ok {
				t.Errorf("%s: ratchet key %s is not in the start snapshot", areaFile(a.ID), r.Key)
			}
			if other, dup := seen[r.Key]; dup {
				t.Errorf("ratchet key %s is in %s and %s", r.Key, other, a.ID)
			}
			seen[r.Key] = a.ID
		}
	}
}

func TestModulePath(t *testing.T) {
	t.Parallel()
	got, err := modulePath(repoRoot())
	if err != nil {
		t.Fatalf("modulePath: %v", err)
	}
	if got == "" || strings.ContainsAny(got, " \t\"") {
		t.Errorf("modulePath = %q, want a bare non-empty module path", got)
	}
}

func TestModulePathParsing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, gomod, want string
		wantErr           bool
	}{
		{"plain", "module example.com/m\n\ngo 1.25\n", "example.com/m", false},
		{"leading comment", "// c\nmodule   example.com/m // trailing\n", "example.com/m", false},
		{"quoted", "module \"example.com/m\"\n", "example.com/m", false},
		{"missing", "go 1.25\n", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tc.gomod), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := modulePath(dir)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("modulePath = %q, %v; want %q, error=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
	if _, err := modulePath(t.TempDir()); err == nil {
		t.Error("modulePath accepted a directory without go.mod")
	}
}
