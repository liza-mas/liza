package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testOpsPkg = "example.com/m/internal/ops"

// writeArtifacts plans every shard of both groups into root the way the CI
// shards do, and writes a test.json in which each shard ran exactly its
// assigned items.
func writeArtifacts(t *testing.T, root string, opsShards, restShards int) {
	t.Helper()
	weightsPath := filepath.Join(root, "weights.json")
	w := testWeights()
	w.OpsPackage = testOpsPkg
	if err := os.WriteFile(weightsPath, []byte(mustJSON(w)), 0o644); err != nil {
		t.Fatal(err)
	}
	opsList := filepath.Join(root, "ops-list.txt")
	names := makeNames(90, 11)
	if err := os.WriteFile(opsList, []byte(strings.Join(names, "\n")+"\nBenchmarkZ\nok  \t"+testOpsPkg+"\t0.1s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgList := filepath.Join(root, "pkgs.txt")
	pkgs := []string{testOpsPkg}
	for i := 0; i < 12; i++ {
		pkgs = append(pkgs, fmt.Sprintf("example.com/m/p%02d", i))
	}
	if err := os.WriteFile(pkgList, []byte(strings.Join(pkgs, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, g := range []struct {
		name, list string
		n          int
	}{{groupOps, opsList, opsShards}, {groupRest, pkgList, restShards}} {
		for i := 1; i <= g.n; i++ {
			dir := filepath.Join(root, "artifacts", fmt.Sprintf("windows-test-%s-%d", g.name, i))
			err := cmdPlan([]string{"-group", g.name, "-shards", fmt.Sprint(g.n), "-index", fmt.Sprint(i),
				"-weights", weightsPath, "-list", g.list, "-out", dir}, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			assigned, err := readLines(filepath.Join(dir, "assigned.txt"))
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, it := range assigned {
				if g.name == groupOps {
					lines = append(lines, ev("run", testOpsPkg, it, 0, ""), ev("pass", testOpsPkg, it, 0.1, ""))
				} else {
					lines = append(lines, ev("pass", it, "", 1, ""))
				}
			}
			if g.name == groupOps {
				lines = append(lines, ev("pass", testOpsPkg, "", 9, ""))
			}
			if err := os.WriteFile(filepath.Join(dir, "test.json"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func runReconcile(t *testing.T, root string) (string, error) {
	t.Helper()
	var out strings.Builder
	err := cmdReconcile([]string{"-dir", filepath.Join(root, "artifacts")}, &out)
	return out.String(), err
}

func TestReconcileAcceptsCompleteRun(t *testing.T) {
	root := t.TempDir()
	writeArtifacts(t, root, 3, 2)
	out, err := runReconcile(t, root)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"ops: 3 shards, 90 listed items, 90 executed exactly once", "rest: 2 shards, 12 listed items, 12 executed exactly once"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestReconcileRejectsCoverageGaps(t *testing.T) {
	shard := func(root, name string) string {
		return filepath.Join(root, "artifacts", "windows-test-"+name)
	}
	editJSON := func(t *testing.T, path string, edit func([]string) []string) {
		lines, err := readLines(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(edit(lines), "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := map[string]func(t *testing.T, root string){
		"never executed": func(t *testing.T, root string) {
			editJSON(t, filepath.Join(shard(root, "ops-2"), "test.json"), func(l []string) []string { return l[2:] })
		},
		"executed more than once": func(t *testing.T, root string) {
			editJSON(t, filepath.Join(shard(root, "rest-1"), "test.json"), func(l []string) []string { return append(l, l[0]) })
		},
		"produced no artifact": func(t *testing.T, root string) {
			if err := os.RemoveAll(shard(root, "ops-3")); err != nil {
				t.Fatal(err)
			}
		},
		"not assigned to any shard": func(t *testing.T, root string) {
			editJSON(t, filepath.Join(shard(root, "ops-1"), "assigned.txt"), func(l []string) []string { return l[1:] })
		},
		"args.txt differs": func(t *testing.T, root string) {
			editJSON(t, filepath.Join(shard(root, "rest-2"), "args.txt"), func(l []string) []string { return l[1:] })
		},
		"executed but not listed": func(t *testing.T, root string) {
			editJSON(t, filepath.Join(shard(root, "ops-1"), "test.json"), func(l []string) []string {
				return append(l, ev("pass", testOpsPkg, "TestSurprise", 0.1, ""))
			})
		},
		"test.json missing": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(shard(root, "rest-1"), "test.json")); err != nil {
				t.Fatal(err)
			}
		},
	}
	for want, breakIt := range cases {
		t.Run(want, func(t *testing.T) {
			root := t.TempDir()
			writeArtifacts(t, root, 3, 2)
			breakIt(t, root)
			out, err := runReconcile(t, root)
			if err == nil || !strings.Contains(out, want) {
				t.Fatalf("want failure mentioning %q, got err=%v\n%s", want, err, out)
			}
		})
	}
}
