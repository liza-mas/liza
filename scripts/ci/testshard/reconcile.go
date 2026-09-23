package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}

// shardRun is one downloaded shard artifact.
type shardRun struct {
	dir      string
	meta     shardMeta
	items    []string
	assigned []string
	args     []string
	pkgs     []result
	tests    []result
	hasJSON  bool
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

func loadShardRun(dir string) (*shardRun, error) {
	s := &shardRun{dir: dir}
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.meta); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	for name, dst := range map[string]*[]string{"items.txt": &s.items, "assigned.txt": &s.assigned, "args.txt": &s.args} {
		if *dst, err = readLines(filepath.Join(dir, name)); err != nil {
			return nil, err
		}
	}
	jsonPath := filepath.Join(dir, "test.json")
	if _, err := os.Stat(jsonPath); err == nil {
		s.hasJSON = true
		label := fmt.Sprintf("%s-%d", s.meta.Group, s.meta.Index)
		if s.pkgs, s.tests, err = readResultsFile(jsonPath, label); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// reconcile proves, from the artifacts of every shard, that each group's
// shards form an exact partition of the item list and that every item was
// actually executed exactly once, by the shard it was assigned to.
func reconcile(runs []*shardRun, out io.Writer) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }
	byGroup := map[string][]*shardRun{}
	for _, r := range runs {
		byGroup[r.meta.Group] = append(byGroup[r.meta.Group], r)
	}
	for _, g := range []string{groupOps, groupRest} {
		if len(byGroup[g]) == 0 {
			add("%s: no shard artifacts found", g)
		}
	}
	groups := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	for _, g := range groups {
		rs := byGroup[g]
		sort.Slice(rs, func(a, b int) bool { return rs[a].meta.Index < rs[b].meta.Index })
		total := rs[0].meta.Total
		seenIdx := map[int]bool{}
		for _, r := range rs {
			if r.meta.Total != total {
				add("%s: shards disagree on the shard count (%d vs %d)", g, r.meta.Total, total)
			}
			if seenIdx[r.meta.Index] {
				add("%s: shard %d reported twice", g, r.meta.Index)
			}
			seenIdx[r.meta.Index] = true
			if strings.Join(r.items, "\n") != strings.Join(rs[0].items, "\n") {
				add("%s: shard %d listed a different item set than shard %d", g, r.meta.Index, rs[0].meta.Index)
			}
		}
		for i := 1; i <= total; i++ {
			if !seenIdx[i] {
				add("%s: shard %d/%d produced no artifact", g, i, total)
			}
		}
		items := rs[0].items
		shards := make([]Shard, 0, len(rs))
		for _, r := range rs {
			shards = append(shards, Shard{Index: r.meta.Index - 1, Items: r.assigned})
		}
		partErr := checkPartition(g, items, shards, rs[0].meta.OpsPackage, 1<<30)
		if partErr != nil {
			add("%s", partErr)
		}
		// The arguments each shard actually ran with must be the ones derived
		// from the assignments, and an ops pattern must select exactly the
		// shard's own names among all listed names.
		complete := len(rs) == total && partErr == nil
		for i, r := range rs {
			if !complete {
				break
			}
			if want := shardArgs(g, shards, i, r.meta.OpsPackage); strings.Join(r.args, "\n") != strings.Join(want, "\n") {
				add("%s shard %d: args.txt differs from the arguments derived from the assignments", g, r.meta.Index)
				continue
			}
			if g != groupOps {
				continue
			}
			re, err := regexp.Compile(r.args[1])
			if err != nil {
				add("ops shard %d: %v", r.meta.Index, err)
				continue
			}
			own := map[string]bool{}
			for _, it := range r.assigned {
				own[it] = true
			}
			for _, it := range items {
				if got := re.MatchString(it); got != own[it] {
					add("ops shard %d: -run pattern match(%q)=%v, want %v", r.meta.Index, it, got, own[it])
					break
				}
			}
		}
		// Execution: every item must have exactly one terminal result, from
		// the shard it was assigned to.
		owner := map[string]int{}
		for _, r := range rs {
			for _, it := range r.assigned {
				owner[it] = r.meta.Index
			}
		}
		ran := map[string][]int{}
		for _, r := range rs {
			if !r.hasJSON {
				add("%s shard %d: test.json missing", g, r.meta.Index)
				continue
			}
			if g == groupOps {
				for _, t := range r.tests {
					if t.Package == r.meta.OpsPackage {
						ran[t.Test] = append(ran[t.Test], r.meta.Index)
					}
				}
			} else {
				for _, p := range r.pkgs {
					ran[p.Package] = append(ran[p.Package], r.meta.Index)
				}
			}
		}
		var missing, extra, dup, wrong []string
		for _, it := range items {
			switch got := ran[it]; {
			case len(got) == 0:
				missing = append(missing, it)
			case len(got) > 1:
				dup = append(dup, fmt.Sprintf("%s%v", it, got))
			case ownerKnown(owner, it) && got[0] != owner[it]:
				wrong = append(wrong, fmt.Sprintf("%s (ran in %d, assigned to %d)", it, got[0], owner[it]))
			}
		}
		for it := range ran {
			if _, ok := owner[it]; !ok {
				extra = append(extra, it)
			}
		}
		sort.Strings(extra)
		for label, list := range map[string][]string{"never executed": missing, "executed more than once": dup, "executed by the wrong shard": wrong, "executed but not listed": extra} {
			if len(list) > 0 {
				add("%s: %d item(s) %s: %s", g, len(list), label, abbreviate(list, 20))
			}
		}
		executed := 0
		for _, it := range items {
			if len(ran[it]) == 1 {
				executed++
			}
		}
		fmt.Fprintf(out, "%s: %d shards, %d listed items, %d executed exactly once\n", g, len(rs), len(items), executed)
	}
	sort.Strings(problems)
	return problems
}

// ownerKnown reports whether it was assigned at all; unassigned items are
// already reported by the partition check.
func ownerKnown(owner map[string]int, it string) bool {
	_, ok := owner[it]
	return ok
}

func abbreviate(list []string, n int) string {
	if len(list) <= n {
		return strings.Join(list, ", ")
	}
	return strings.Join(list[:n], ", ") + fmt.Sprintf(", ... (%d more)", len(list)-n)
}

func findShardDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "meta.json" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return err
	})
	sort.Strings(dirs)
	return dirs, err
}

func cmdReconcile(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	dir := fs.String("dir", "", "directory holding the downloaded shard artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("-dir is required")
	}
	dirs, err := findShardDirs(*dir)
	if err != nil {
		return err
	}
	var runs []*shardRun
	for _, d := range dirs {
		r, err := loadShardRun(d)
		if err != nil {
			return err
		}
		runs = append(runs, r)
	}
	problems := reconcile(runs, out)
	for _, p := range problems {
		fmt.Fprintln(out, "FAIL:", p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d coverage problem(s)", len(problems))
	}
	fmt.Fprintln(out, "coverage reconciled: every listed test and package ran exactly once")
	return nil
}
