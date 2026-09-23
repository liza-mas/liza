package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// event is one record of the `go test -json` (test2json) stream.
type event struct {
	Time       time.Time `json:"Time"`
	Action     string    `json:"Action"`
	Package    string    `json:"Package"`
	ImportPath string    `json:"ImportPath"`
	Test       string    `json:"Test"`
	Elapsed    float64   `json:"Elapsed"`
	Output     string    `json:"Output"`
}

func isTerminal(action string) bool {
	return action == "pass" || action == "fail" || action == "skip"
}

func isTopLevel(test string) bool { return test != "" && !strings.Contains(test, "/") }

// result is the terminal state of one package or top-level test.
type result struct {
	Package string
	Test    string // empty for a package result
	Action  string
	Elapsed float64
	Shard   string
}

// readResults parses a -json stream and returns its package-level and
// top-level-test terminal results. Lines that are not JSON (for example a
// panic written straight to stdout) are ignored.
func readResults(r io.Reader, shard string) (pkgs, tests []result, err error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e event
		if json.Unmarshal(line, &e) != nil || !isTerminal(e.Action) || e.Package == "" {
			continue
		}
		res := result{Package: e.Package, Test: e.Test, Action: e.Action, Elapsed: e.Elapsed, Shard: shard}
		switch {
		case e.Test == "":
			pkgs = append(pkgs, res)
		case isTopLevel(e.Test):
			tests = append(tests, res)
		}
	}
	return pkgs, tests, sc.Err()
}

func readResultsFile(path, shard string) (pkgs, tests []result, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return readResults(f, shard)
}

// formatter turns the -json stream into a compact human log: build output,
// package-level output (ok/FAIL lines, panics outside tests), the full output
// of every failed test, and a periodic heartbeat naming the running tests so
// a stalled shard shows where it is stuck.
type formatter struct {
	mu      sync.Mutex
	out     io.Writer
	buf     map[string][]string // pkg\x00test -> buffered output
	running map[string]time.Time
	passed  int
	failed  int
	skipped int
	started time.Time
}

func newFormatter(out io.Writer) *formatter {
	return &formatter{out: out, buf: map[string][]string{}, running: map[string]time.Time{}, started: time.Now()}
}

func (f *formatter) handle(line []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(line) == 0 || line[0] != '{' {
		fmt.Fprintf(f.out, "%s\n", line)
		return
	}
	var e event
	if err := json.Unmarshal(line, &e); err != nil {
		fmt.Fprintf(f.out, "%s\n", line)
		return
	}
	key := e.Package + "\x00" + e.Test
	switch e.Action {
	case "build-output":
		fmt.Fprint(f.out, e.Output)
	case "run", "cont":
		if isTopLevel(e.Test) {
			f.running[key] = time.Now()
		}
	case "pause":
		// A parallel test waiting for its turn is not running.
		delete(f.running, key)
	case "output":
		if e.Test == "" {
			fmt.Fprint(f.out, e.Output)
			return
		}
		f.buf[key] = append(f.buf[key], e.Output)
	case "pass", "fail", "skip":
		if e.Test == "" {
			return
		}
		if e.Action == "fail" {
			fmt.Fprintf(f.out, "--- FAIL output: %s %s\n", e.Package, e.Test)
			fmt.Fprint(f.out, strings.Join(f.buf[key], ""))
		}
		delete(f.buf, key)
		if isTopLevel(e.Test) {
			delete(f.running, key)
			switch e.Action {
			case "pass":
				f.passed++
			case "fail":
				f.failed++
			default:
				f.skipped++
			}
		}
	}
}

func (f *formatter) heartbeat() {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.running))
	for k, since := range f.running {
		names = append(names, fmt.Sprintf("%s (%ds)", strings.Replace(k, "\x00", " ", 1), int(time.Since(since).Seconds())))
	}
	sort.Strings(names)
	if len(names) > 5 {
		names = append(names[:5], fmt.Sprintf("... %d more", len(names)-5))
	}
	fmt.Fprintf(f.out, "[testshard %s] top-level tests passed=%d failed=%d skipped=%d; running: %s\n",
		time.Since(f.started).Round(time.Second), f.passed, f.failed, f.skipped, strings.Join(names, ", "))
}

func runFormatter(in io.Reader, out io.Writer, every time.Duration) error {
	f := newFormatter(out)
	done := make(chan struct{})
	if every > 0 {
		go func() {
			t := time.NewTicker(every)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					f.heartbeat()
				}
			}
		}()
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 16*1024*1024), 16*1024*1024)
	for sc.Scan() {
		f.handle(sc.Bytes())
	}
	close(done)
	f.heartbeat()
	return sc.Err()
}

// writeSummary renders a Markdown step summary: totals, failures, and the
// slowest top-level tests and packages.
func writeSummary(out io.Writer, title string, pkgs, tests []result, top int) {
	count := func(rs []result, action string) int {
		n := 0
		for _, r := range rs {
			if r.Action == action {
				n++
			}
		}
		return n
	}
	fmt.Fprintf(out, "### %s\n\n", title)
	fmt.Fprintf(out, "Packages: %d pass, %d fail, %d skip. Top-level tests: %d pass, %d fail, %d skip.\n\n",
		count(pkgs, "pass"), count(pkgs, "fail"), count(pkgs, "skip"),
		count(tests, "pass"), count(tests, "fail"), count(tests, "skip"))
	var failed []string
	for _, r := range append(append([]result(nil), pkgs...), tests...) {
		if r.Action == "fail" {
			failed = append(failed, strings.TrimSpace(r.Package+" "+r.Test))
		}
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		fmt.Fprintf(out, "**Failed:**\n\n")
		for _, f := range failed {
			fmt.Fprintf(out, "- `%s`\n", f)
		}
		fmt.Fprintln(out)
	}
	table := func(heading string, rs []result, name func(result) string) {
		sorted := append([]result(nil), rs...)
		sort.SliceStable(sorted, func(a, b int) bool {
			if sorted[a].Elapsed != sorted[b].Elapsed {
				return sorted[a].Elapsed > sorted[b].Elapsed
			}
			return name(sorted[a]) < name(sorted[b])
		})
		if len(sorted) > top {
			sorted = sorted[:top]
		}
		fmt.Fprintf(out, "<details><summary>%s</summary>\n\n| # | seconds | result | shard | name |\n|---:|---:|---|---|---|\n", heading)
		for i, r := range sorted {
			fmt.Fprintf(out, "| %d | %.1f | %s | %s | `%s` |\n", i+1, r.Elapsed, r.Action, r.Shard, name(r))
		}
		fmt.Fprintf(out, "\n</details>\n\n")
	}
	table(fmt.Sprintf("%d slowest top-level tests", top), tests, func(r result) string {
		return shortPkg(r.Package) + "." + r.Test
	})
	table(fmt.Sprintf("%d slowest packages", top), pkgs, func(r result) string { return r.Package })
}

func shortPkg(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
