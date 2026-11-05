package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testCLIPkg = "example.com/m/cmd/cli"

// splitWeights adds a split group "cli" that shards testCLIPkg's tests.
func splitWeights() *Weights {
	w := testWeights()
	w.OpsPackage = testOpsPkg
	w.Split = map[string]*SplitPackage{"cli": {Package: testCLIPkg, Tests: map[string]float64{"TestCLIHeavy": 50}}}
	return w
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeSplitArtifacts plans ops (1 shard), rest (1) and cli (cliShards) into
// root/artifacts and writes test.json files in which every shard ran exactly
// its assigned items. It returns the weights path.
func writeSplitArtifacts(t *testing.T, root string, cliShards int) string {
	t.Helper()
	weightsPath := writeFile(t, filepath.Join(root, "weights.json"), mustJSON(splitWeights()))
	opsList := writeFile(t, filepath.Join(root, "ops.txt"), "TestOpsA\nTestOpsB\n")
	cliNames := append(makeNames(30, 5), "TestCLIHeavy")
	cliList := writeFile(t, filepath.Join(root, "cli.txt"), strings.Join(cliNames, "\n")+"\n")
	pkgList := writeFile(t, filepath.Join(root, "pkgs.txt"), strings.Join([]string{testOpsPkg, testCLIPkg, "example.com/m/a", "example.com/m/b"}, "\n")+"\n")
	for _, g := range []struct {
		name, list, pkg string
		n               int
	}{{groupOps, opsList, testOpsPkg, 1}, {groupRest, pkgList, "", 1}, {"cli", cliList, testCLIPkg, cliShards}} {
		for i := 1; i <= g.n; i++ {
			dir := filepath.Join(root, "artifacts", fmt.Sprintf("windows-test-%s-%d", g.name, i))
			if err := cmdPlan([]string{"-group", g.name, "-shards", fmt.Sprint(g.n), "-index", fmt.Sprint(i),
				"-weights", weightsPath, "-list", g.list, "-out", dir}, io.Discard); err != nil {
				t.Fatal(err)
			}
			assigned, err := readLines(filepath.Join(dir, "assigned.txt"))
			if err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, it := range assigned {
				if g.pkg != "" {
					lines = append(lines, ev("run", g.pkg, it, 0, ""), ev("pass", g.pkg, it, 0.1, ""))
				} else {
					lines = append(lines, ev("pass", it, "", 1, ""))
				}
			}
			if g.pkg != "" {
				lines = append(lines, ev("pass", g.pkg, "", 9, ""))
			}
			writeFile(t, filepath.Join(dir, "test.json"), strings.Join(lines, "\n")+"\n")
		}
	}
	return weightsPath
}

// A split group shards its package by test-name range, and rest leaves that
// package out as it leaves out ops.
func TestSplitGroupShardsItsPackageByTestName(t *testing.T) {
	root := t.TempDir()
	writeSplitArtifacts(t, root, 2)
	art := filepath.Join(root, "artifacts")
	cliNames, err := readLines(filepath.Join(art, "windows-test-cli-1", "items.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		dir := filepath.Join(art, fmt.Sprintf("windows-test-cli-%d", i))
		args, _ := readLines(filepath.Join(dir, "args.txt"))
		if len(args) != 3 || args[0] != "-run" || args[2] != testCLIPkg {
			t.Fatalf("cli shard %d args = %q, want -run <range> %s", i, args, testCLIPkg)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
		var meta shardMeta
		if err := json.Unmarshal(data, &meta); err != nil || meta.Package != testCLIPkg {
			t.Fatalf("cli shard %d meta = %s (%v), want package %s", i, data, err, testCLIPkg)
		}
	}
	if len(cliNames) != 31 {
		t.Fatalf("cli items = %d, want 31", len(cliNames))
	}
	rest, _ := readLines(filepath.Join(art, "windows-test-rest-1", "items.txt"))
	if got := strings.Join(rest, ","); got != "example.com/m/a,example.com/m/b" {
		t.Fatalf("rest items = %s, want the packages no group splits", got)
	}
}

func TestPlanRejectsAGroupTheWeightsDoNotDefine(t *testing.T) {
	root := t.TempDir()
	weightsPath := writeSplitArtifacts(t, root, 1)
	err := cmdVerify([]string{"-group", "docs", "-shards", "1", "-weights", weightsPath, "-list", filepath.Join(root, "cli.txt")}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "cli, ops, rest") {
		t.Fatalf("err = %v, want the defined groups listed", err)
	}
}

func TestLoadWeightsRejectsInvalidSplitGroups(t *testing.T) {
	cases := []struct {
		name  string
		split map[string]*SplitPackage
		want  string
	}{
		{"rest name", map[string]*SplitPackage{groupRest: {Package: testCLIPkg}}, "is reserved"},
		{"ops name", map[string]*SplitPackage{groupOps: {Package: testCLIPkg}}, "is reserved"},
		{"no package", map[string]*SplitPackage{"cli": {}}, "needs a package"},
		{"ops package", map[string]*SplitPackage{"cli": {Package: testOpsPkg}}, "repeats package"},
		{"shared package", map[string]*SplitPackage{"cli": {Package: testCLIPkg}, "cli2": {Package: testCLIPkg}}, "repeats package"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := splitWeights()
			w.Split = c.split
			path := writeFile(t, filepath.Join(t.TempDir(), "w.json"), mustJSON(w))
			if _, err := loadWeights(path); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// With the weights, reconcile requires and checks every group they define.
func TestReconcileCoversSplitGroups(t *testing.T) {
	root := t.TempDir()
	weightsPath := writeSplitArtifacts(t, root, 2)
	art := filepath.Join(root, "artifacts")
	var out strings.Builder
	if err := cmdReconcile([]string{"-dir", art, "-weights", weightsPath}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if want := "cli: 2 shards, 31 listed items, 31 executed exactly once"; !strings.Contains(out.String(), want) {
		t.Fatalf("output lacks %q:\n%s", want, out.String())
	}

	for i := 1; i <= 2; i++ {
		if err := os.RemoveAll(filepath.Join(art, fmt.Sprintf("windows-test-cli-%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	if err := cmdReconcile([]string{"-dir", art, "-weights", weightsPath}, &out); err == nil || !strings.Contains(out.String(), "cli: no shard artifacts found") {
		t.Fatalf("missing cli group: err=%v\n%s", err, out.String())
	}
}

// weights records the tests of every split package, and only other packages
// as package entries.
func TestWeightsRecordsSplitPackageTests(t *testing.T) {
	dir := t.TempDir()
	base := writeFile(t, filepath.Join(dir, "base.json"), mustJSON(splitWeights()))
	run := filepath.Join(dir, "run", "windows-test-x-1")
	if err := os.MkdirAll(run, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(run, "test.json"), strings.Join([]string{
		ev("pass", testOpsPkg, "TestOpsA", 2, ""), ev("pass", testOpsPkg, "", 3, ""),
		ev("pass", testCLIPkg, "TestCLINew", 7, ""), ev("pass", testCLIPkg, "", 8, ""),
		ev("pass", "example.com/m/a", "TestA", 1, ""), ev("pass", "example.com/m/a", "", 4, ""),
	}, "\n")+"\n")
	var out strings.Builder
	if err := cmdWeights([]string{"-base", base, filepath.Join(dir, "run")}, &out); err != nil {
		t.Fatal(err)
	}
	var w Weights
	if err := json.Unmarshal([]byte(out.String()), &w); err != nil {
		t.Fatal(err)
	}
	cli := w.Split["cli"].Tests
	if w.Tests["TestOpsA"] != 2 || cli["TestCLINew"] != 7 || cli["TestCLIHeavy"] != 50 {
		t.Fatalf("tests: ops %v, cli %v", w.Tests, cli)
	}
	if _, ok := w.Packages[testCLIPkg]; ok {
		t.Fatalf("split package recorded as a package: %v", w.Packages)
	}
	if w.Packages["example.com/m/a"] != 4 {
		t.Fatalf("packages: %v", w.Packages)
	}
}
