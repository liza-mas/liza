package main

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// runs holds the reproductions of every C package: its pre-C commit (tagged
// baseline) and each point (untagged), keyed by commit.
type runs struct{ pre, points map[string]map[string][]byte }

func dotted(dirs []string) []string {
	var result []string
	for _, dir := range dirs {
		result = append(result, "./"+dir)
	}
	return result
}

// line is the package report: every point with the reasons of the package
// records it adds.
func (pc *packageChain) line() string {
	fields := []string{"ok", pc.dir, "package", "pre", pc.pre}
	for _, pt := range pc.points {
		var reasons []string
		for _, r := range pt.records {
			if !slices.Contains(reasons, r.reason) {
				reasons = append(reasons, r.reason)
			}
		}
		fields = append(fields, strings.Join(reasons, ","), pt.sha)
	}
	return strings.Join(fields, " ")
}

// packageRuns reproduces each distinct pre-C commit and each distinct point
// once, over the C packages they belong to.
func (e *engine) packageRuns(ctx context.Context, h history) (runs, error) {
	// Packages and their points are visited in order, so calls are stable.
	var dirs []string
	for dir := range h.packages {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	var pre, points commitSet
	for _, dir := range dirs {
		pre.add(h.packages[dir].pre, dir)
		for _, pt := range h.packages[dir].points {
			points.add(pt.sha, dir)
		}
	}
	r := runs{pre: map[string]map[string][]byte{}, points: map[string]map[string][]byte{}}
	for _, step := range []struct {
		commits  commitSet
		baseline bool
		into     map[string]map[string][]byte
	}{{pre, true, r.pre}, {points, false, r.points}} {
		for _, commit := range step.commits.order {
			values, err := e.values(ctx, commit, step.baseline, dotted(step.commits.dirs[commit]), nil)
			if err != nil {
				return r, err
			}
			step.into[commit] = values
		}
	}
	return r, nil
}

// commitSet maps commits, in first-seen order, to the packages they serve.
type commitSet struct {
	order []string
	dirs  map[string][]string
}

func (c *commitSet) add(commit, dir string) {
	if c.dirs == nil {
		c.dirs = map[string][]string{}
	}
	if _, ok := c.dirs[commit]; !ok {
		c.order = append(c.order, commit)
	}
	c.dirs[commit] = append(c.dirs[commit], dir)
}

// owned keeps the goldens of one package, not of its nested packages.
func owned(dir string, values map[string][]byte) map[string][]byte {
	result := map[string][]byte{}
	for p, b := range values {
		if packageDir(p) == dir && goldenPath(p) {
			result[p] = b
		}
	}
	return result
}

func (e *engine) committedGoldens(ctx context.Context, commit, dir string) (map[string][]byte, error) {
	names, err := e.commands.git(ctx, e.repo, "ls-tree", "-r", "--name-only", commit, "--", dir)
	if err != nil {
		return nil, err
	}
	result := map[string][]byte{}
	for _, name := range strings.Split(string(names), "\n") {
		if packageDir(name) == dir && goldenPath(name) {
			if result[name], err = e.oracleFile(ctx, commit, name); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func recordReason(records []change, name string) string {
	for _, r := range records {
		if r.path == name {
			return r.reason
		}
	}
	return ""
}

// checkPackage applies D56 to one C package: its goldens at the pre-C commit
// must be reproduced from the starting baseline, every point must reproduce
// the goldens committed there, and the working tree must equal HEAD. It
// returns the report line of each path that needs one.
func (e *engine) checkPackage(ctx context.Context, pc *packageChain, current map[string][]byte, issues map[string][]string, r runs) (map[string]string, error) {
	committed, err := e.committedGoldens(ctx, pc.pre, pc.dir)
	if err != nil {
		return nil, err
	}
	state := map[string][]byte{}
	names := map[string]bool{}
	for g, b := range committed {
		state[g], names[g] = b, true
	}
	fail := map[string]string{}
	setFail := func(g, msg string) {
		if fail[g] == "" {
			fail[g] = msg
		}
	}
	// A golden first committed after the pre-C commit has no reproducible
	// pre-change blob.
	for _, pt := range pc.points {
		for _, ch := range pt.changed {
			if _, ok := committed[ch.path]; ch.status == 'A' && goldenPath(ch.path) && !ok {
				setFail(ch.path, "pre-change blob absent "+pc.pre)
			}
		}
	}
	pre := owned(pc.dir, r.pre[pc.pre])
	for g := range pre {
		names[g] = true
		if _, ok := committed[g]; !ok {
			setFail(g, "pre-change reproduction differs "+pc.pre)
		}
	}
	for g, want := range committed {
		if got, ok := pre[g]; !ok || !bytes.Equal(got, want) {
			setFail(g, "pre-change reproduction differs "+pc.pre)
		}
	}
	links := map[string][]string{}
	last := map[string]string{}
	deleted := map[string]string{}
	for _, pt := range pc.points {
		removed := map[string]bool{}
		for _, ch := range pt.changed {
			g := ch.path
			if !goldenPath(g) {
				continue
			}
			names[g] = true
			links[g] = append(links[g], recordReason(pt.records, g), pt.sha)
			last[g] = pt.sha
			if ch.status == 'D' {
				delete(state, g)
				removed[g], deleted[g] = true, recordReason(pt.records, g)
				continue
			}
			delete(deleted, g)
			if state[g], err = e.oracleFile(ctx, pt.sha, g); err != nil {
				setFail(g, "change point blob absent "+pt.sha)
			}
		}
		values := owned(pc.dir, r.points[pt.sha])
		for g, want := range state {
			if got, ok := values[g]; !ok || !bytes.Equal(got, want) {
				setFail(g, "change point reproduction differs "+pt.sha)
			}
		}
		for g := range values {
			names[g] = true
			if _, ok := state[g]; !ok {
				if removed[g] {
					setFail(g, "deletion not reproduced "+pt.sha)
				} else {
					setFail(g, "change point reproduction differs "+pt.sha)
				}
			}
		}
	}
	for g := range owned(pc.dir, current) {
		names[g] = true
	}
	for p := range issues {
		if packageDir(p) == pc.dir && goldenPath(p) {
			names[p] = true
		}
	}
	lines := map[string]string{}
	tail := pc.pre
	if len(pc.points) > 0 {
		tail = pc.points[len(pc.points)-1].sha
	}
	for g := range names {
		got, exists := current[g]
		want, inHead := state[g]
		switch {
		case len(issues[g]) > 0:
		case fail[g] != "":
			lines[g] = fmt.Sprintf("unexplained %s: %s", g, fail[g])
		case inHead && !exists:
			lines[g] = "missing " + g
		case inHead && !bytes.Equal(got, want):
			sha := last[g]
			if sha == "" {
				sha = tail
			}
			lines[g] = fmt.Sprintf("unexplained %s: working tree differs from last change point %s", g, sha)
		case inHead:
			lines[g] = reportLinks(g, pc.pre, links[g], "")
		case exists:
			lines[g] = "extra " + g
		case isC(deleted[g]):
			lines[g] = reportLinks(g, pc.pre, links[g], " deleted")
		default:
			lines[g] = "missing " + g
		}
	}
	dirty, err := e.commands.git(ctx, e.repo, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all", "--", pc.dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range strings.Split(string(dirty), "\x00") {
		if len(entry) > 3 && packageDir(entry[3:]) == pc.dir && !goldenPath(entry[3:]) {
			issues[entry[3:]] = append(issues[entry[3:]], "working tree differs from HEAD")
		}
	}
	for p, list := range issues {
		if packageDir(p) != pc.dir {
			continue
		}
		var text []string
		for _, issue := range list {
			text = append(text, fmt.Sprintf("unexplained %s: %s", p, issue))
		}
		lines[p] = strings.Join(text, "\n")
	}
	return lines, nil
}

func reportLinks(golden, pre string, links []string, suffix string) string {
	return strings.Join(append([]string{"ok", golden, "chain", "pre", pre}, links...), " ") + suffix
}
