// Command testshard splits the Windows CI test run into balanced shards and
// checks that the shards together run every test exactly once.
//
// It is CI tooling for this repository only and is not part of the product.
//
//	testshard plan      -group ops|rest -shards N -index I -weights W -list L -out DIR
//	testshard verify    -group ops|rest -shards N -weights W -list L
//	testshard reconcile -dir ARTIFACTS
//	testshard fmt       [-heartbeat 60s]            < go-test-json
//	testshard summary   [-title T] [-top 30] PATH... (test.json files or artifact dirs)
//	testshard weights   -base W PATH...             (refresh weights from CI artifacts)
//	testshard sysinfo
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// defaultMaxLen keeps each shard's command line far below the 32,767-char
// Windows CreateProcess limit (go test passes the pattern on to the test
// binary again as -test.run).
const defaultMaxLen = 8000

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: testshard plan|verify|reconcile|fmt|summary|weights|sysinfo [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = cmdPlan(os.Args[2:], os.Stdout)
	case "verify":
		err = cmdVerify(os.Args[2:], os.Stdout)
	case "reconcile":
		err = cmdReconcile(os.Args[2:], os.Stdout)
	case "fmt":
		err = cmdFmt(os.Args[2:])
	case "summary":
		err = cmdSummary(os.Args[2:], os.Stdout)
	case "weights":
		err = cmdWeights(os.Args[2:], os.Stdout)
	case "sysinfo":
		cmdSysinfo(os.Stdout)
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "testshard:", err)
		os.Exit(1)
	}
}

type partitionFlags struct {
	group   string
	shards  int
	weights string
	list    string
	maxLen  int
}

func (p *partitionFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&p.group, "group", "", "ops (top-level tests of the ops package) or rest (all other packages)")
	fs.IntVar(&p.shards, "shards", 0, "number of shards in the group")
	fs.StringVar(&p.weights, "weights", "", "weights JSON file")
	fs.StringVar(&p.list, "list", "", "`go test -list .` output (ops) or `go list ./...` output (rest)")
	fs.IntVar(&p.maxLen, "max-len", defaultMaxLen, "maximum command-line length per shard")
}

// load parses the inputs and returns the item list, the full partition, and
// the weights. It fails if the partition violates any coverage invariant.
func (p *partitionFlags) load() ([]string, []Shard, *Weights, error) {
	if p.group != groupOps && p.group != groupRest {
		return nil, nil, nil, fmt.Errorf("-group must be %q or %q", groupOps, groupRest)
	}
	if p.shards < 1 {
		return nil, nil, nil, errors.New("-shards must be >= 1")
	}
	w, err := loadWeights(p.weights)
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := os.Open(p.list)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	var items []string
	if p.group == groupOps {
		items, err = parseTestList(f)
	} else {
		items, err = parsePackageList(f, w.OpsPackage)
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if len(items) == 0 {
		return nil, nil, nil, fmt.Errorf("%s: no items parsed from %s", p.group, p.list)
	}
	shards := partitionFor(p.group, items, w, p.shards)
	if err := checkPartition(p.group, items, shards, w.OpsPackage, p.maxLen); err != nil {
		return nil, nil, nil, err
	}
	return items, shards, w, nil
}

func printTable(out io.Writer, group string, items []string, shards []Shard, opsPackage string, current int) {
	fmt.Fprintf(out, "%s: %d items in %d shards (coverage check passed)\n", group, len(items), len(shards))
	fmt.Fprintf(out, "  %-8s %6s %10s %8s\n", "shard", "items", "est-secs", "cmd-len")
	for i, s := range shards {
		mark := ""
		if s.Index == current {
			mark = "  <- this shard"
		}
		fmt.Fprintf(out, "  %-8s %6d %10.1f %8d%s\n", fmt.Sprintf("%s-%d", group, s.Index+1),
			len(s.Items), s.Weight, commandLength(shardArgs(group, shards, i, opsPackage)), mark)
	}
}

func cmdVerify(args []string, out io.Writer) error {
	var p partitionFlags
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	p.register(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	items, shards, w, err := p.load()
	if err != nil {
		return err
	}
	printTable(out, p.group, items, shards, w.OpsPackage, -1)
	return nil
}

// shardMeta is written next to each shard's artifacts for reconcile.
type shardMeta struct {
	Group      string `json:"group"`
	Index      int    `json:"index"` // 1-based
	Total      int    `json:"total"`
	OpsPackage string `json:"ops_package"`
}

func cmdPlan(args []string, out io.Writer) error {
	var p partitionFlags
	var index int
	var dir string
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	p.register(fs)
	fs.IntVar(&index, "index", 0, "1-based shard index")
	fs.StringVar(&dir, "out", "", "directory for items.txt, assigned.txt, args.txt, meta.json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if dir == "" {
		return errors.New("-out is required")
	}
	items, shards, w, err := p.load()
	if err != nil {
		return err
	}
	if index < 1 || index > len(shards) {
		return fmt.Errorf("-index must be in 1..%d", len(shards))
	}
	s := shards[index-1]
	if len(s.Items) == 0 {
		return fmt.Errorf("%s shard %d is empty; reduce the shard count", p.group, index)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	meta := shardMeta{Group: p.group, Index: index, Total: len(shards), OpsPackage: w.OpsPackage}
	files := map[string]string{
		"items.txt":    strings.Join(items, "\n") + "\n",
		"assigned.txt": strings.Join(s.Items, "\n") + "\n",
		"args.txt":     strings.Join(shardArgs(p.group, shards, index-1, w.OpsPackage), "\n") + "\n",
		"meta.json":    mustJSON(meta),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	printTable(out, p.group, items, shards, w.OpsPackage, index-1)
	return nil
}

func cmdFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	every := fs.Duration("heartbeat", time.Minute, "interval of the progress line (0 disables)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runFormatter(os.Stdin, os.Stdout, *every)
}

// collectJSON expands paths (files or directories searched recursively for
// test.json) into (file, shard label) pairs.
func collectJSON(paths []string) ([][2]string, error) {
	var found [][2]string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			found = append(found, [2]string{p, filepath.Base(filepath.Dir(p))})
			continue
		}
		err = filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "test.json" {
				found = append(found, [2]string{path, filepath.Base(filepath.Dir(path))})
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(found, func(a, b int) bool { return found[a][0] < found[b][0] })
	return found, nil
}

func cmdSummary(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("summary", flag.ContinueOnError)
	title := fs.String("title", "Windows tests", "summary heading")
	top := fs.Int("top", 30, "number of slowest tests and packages to list")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := collectJSON(fs.Args())
	if err != nil {
		return err
	}
	var pkgs, tests []result
	for _, f := range files {
		p, t, err := readResultsFile(f[0], f[1])
		if err != nil {
			return err
		}
		pkgs, tests = append(pkgs, p...), append(tests, t...)
	}
	writeSummary(out, *title, pkgs, tests, *top)
	return nil
}

func cmdSysinfo(out io.Writer) {
	fmt.Fprintf(out, "GOOS/GOARCH=%s/%s NumCPU=%d GOMAXPROCS=%d go=%s\n",
		runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.GOMAXPROCS(0), runtime.Version())
	for _, k := range []string{"GOMAXPROCS", "GOCACHE", "GOMODCACHE", "GOTMPDIR", "TEMP", "TMP", "NUMBER_OF_PROCESSORS"} {
		fmt.Fprintf(out, "%s=%s\n", k, os.Getenv(k))
	}
}
