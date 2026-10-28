package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type nameStatus struct {
	status byte
	path   string
}

// point is a commit that changes paths of a C package after its pre-C commit.
type point struct {
	sha, parent string
	changed     []nameStatus
	records     []change
}

// packageChain is the D56 history of one C package: the commit before its
// first C change and every later commit changing one of its paths.
type packageChain struct {
	dir, pre string
	points   []point
}

type history struct {
	packages map[string]*packageChain
	issues   map[string][]string
}

func (e *engine) parents(ctx context.Context, sha string) ([]string, error) {
	b, err := e.commands.git(ctx, e.repo, "rev-list", "--parents", "-n", "1", sha)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return nil, fmt.Errorf("missing commit %s", sha)
	}
	return fields[1:], nil
}

func (e *engine) recordsAdded(ctx context.Context, sha string, parents []string) ([]change, error) {
	args := []string{"show", "--format=", "--root", "--unified=0", sha, "--", ledgerPath}
	if len(parents) > 0 {
		args = []string{"diff", "--unified=0", parents[0], sha, "--", ledgerPath}
	}
	b, err := e.commands.git(ctx, e.repo, args...)
	if err != nil {
		return nil, err
	}
	var result []change
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			return nil, fmt.Errorf("ledger is not append-only at %s", sha)
		}
		if !strings.HasPrefix(line, "+changed ") {
			continue
		}
		if c, ok := parseChange(strings.Split(strings.TrimPrefix(line, "+"), " ")); ok {
			result = append(result, c)
		}
	}
	return result, nil
}

// ownedChanges lists the paths a commit changes that belong to package dir
// itself, not to a nested package.
func (e *engine) ownedChanges(ctx context.Context, parent, sha, dir string) ([]nameStatus, error) {
	b, err := e.commands.git(ctx, e.repo, "diff", "--name-status", "--no-renames", "-z", parent, sha, "--", dir)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
	var result []nameStatus
	for i := 0; i+1 < len(fields); i += 2 {
		if name := fields[i+1]; packageDir(name) == dir {
			result = append(result, nameStatus{fields[i][0], name})
		}
	}
	return result, nil
}

// cPackages lists the selected packages holding a C1-C6 record, sorted.
func (e *engine) cPackages(l ledger) []string {
	seen := map[string]bool{}
	var dirs []string
	for _, c := range l.changes {
		if dir := packageDir(c.path); isC(c.reason) && selected(c.path, e.packages) && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs
}

// attributable reports whether the attributed record c, added by ledger commit
// y, may stand as a same-commit record of the commit it names: an ancestor of
// y (never y itself, which cannot name its own hash), with one parent,
// changing the record's path. An unknown commit is not attributable; only
// cancellation and a failed diff are errors.
func (e *engine) attributable(ctx context.Context, y string, c change) (bool, error) {
	parents, err := e.parents(ctx, c.commit)
	if err != nil || len(parents) != 1 {
		return false, ctx.Err()
	}
	if _, err := e.commands.git(ctx, e.repo, "merge-base", "--is-ancestor", c.commit, y); err != nil {
		return false, ctx.Err()
	}
	changed, err := e.commands.git(ctx, e.repo, "diff", "--name-only", parents[0], c.commit, "--", c.path)
	return len(changed) > 0, err
}

// scanRecords reads every ledger commit independently of golden history: a
// record-only commit would otherwise be invisible to git log -- <path>. A
// record is keyed to the commit it must change: the ledger commit adding it,
// or, when attributed, the commit it names.
func (e *engine) scanRecords(ctx context.Context, issues map[string][]string) (map[string][]change, error) {
	byCommit := map[string][]change{}
	b, err := e.commands.git(ctx, e.repo, "log", "--format=%H", "HEAD", "--", ledgerPath)
	if err != nil {
		return nil, err
	}
	for _, sha := range strings.Fields(string(b)) {
		parents, err := e.parents(ctx, sha)
		if err != nil {
			return nil, err
		}
		added, err := e.recordsAdded(ctx, sha, parents)
		if err != nil {
			return nil, err
		}
		for _, c := range added {
			if c.commit != "" {
				if !selected(c.path, e.packages) {
					continue
				}
				ok, err := e.attributable(ctx, sha, c)
				if err != nil {
					return nil, err
				}
				if !ok {
					issues[c.path] = append(issues[c.path], "attributed record invalid "+c.commit)
					continue
				}
				byCommit[c.commit] = append(byCommit[c.commit], c)
				continue
			}
			byCommit[sha] = append(byCommit[sha], c)
			if !selected(c.path, e.packages) || len(parents) == 0 {
				continue
			}
			changed, err := e.commands.git(ctx, e.repo, "diff", "--name-only", parents[0], sha, "--", c.path)
			if err != nil {
				return nil, err
			}
			if len(changed) == 0 {
				issues[c.path] = append(issues[c.path], "record without change "+sha)
			}
		}
	}
	return byCommit, nil
}

func (e *engine) resolveHistory(ctx context.Context, l ledger) (history, error) {
	h := history{packages: map[string]*packageChain{}, issues: map[string][]string{}}
	byCommit, err := e.scanRecords(ctx, h.issues)
	if err != nil {
		return h, err
	}
	for _, dir := range e.cPackages(l) {
		pc, err := e.packageChain(ctx, dir, l, byCommit, h.issues)
		if err != nil {
			return h, err
		}
		if pc != nil {
			h.packages[dir] = pc
		}
	}
	return h, nil
}

// packageChain finds the first C point of a package, its pre-C commit and the
// points after it. Commits that cannot be reproduced become path issues.
func (e *engine) packageChain(ctx context.Context, dir string, l ledger, byCommit map[string][]change, issues map[string][]string) (*packageChain, error) {
	b, err := e.commands.git(ctx, e.repo, "log", "--topo-order", "--reverse", "--format=%H", "HEAD", "--", dir)
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(string(b))
	records := func(sha string) []change {
		var result []change
		for _, c := range byCommit[sha] {
			if packageDir(c.path) == dir {
				result = append(result, c)
			}
		}
		return result
	}
	first := -1
	for i, sha := range shas {
		for _, c := range records(sha) {
			if isC(c.reason) {
				first = i
			}
		}
		if first >= 0 {
			break
		}
	}
	parents := []string(nil)
	if first >= 0 {
		if parents, err = e.parents(ctx, shas[first]); err != nil {
			return nil, err
		}
	}
	if len(parents) == 0 {
		for _, c := range l.changes {
			if isC(c.reason) && packageDir(c.path) == dir {
				issues[c.path] = append(issues[c.path], "C record has no change point")
			}
		}
		return nil, nil
	}
	pc := &packageChain{dir: dir, pre: parents[0]}
	for _, sha := range shas[first:] {
		parents, err := e.parents(ctx, sha)
		if err != nil {
			return nil, err
		}
		changed, err := e.ownedChanges(ctx, parents[0], sha, dir)
		if err != nil {
			return nil, err
		}
		if len(changed) == 0 {
			continue
		}
		recs := records(sha)
		if len(parents) != 1 {
			kind := "oracle files"
			for _, ch := range changed {
				if goldenPath(ch.path) {
					kind = "golden"
				}
			}
			for _, ch := range changed {
				issues[ch.path] = append(issues[ch.path], "merge changes "+kind+" "+sha)
			}
			continue
		}
		for _, ch := range changed {
			n := 0
			for _, r := range recs {
				if r.path == ch.path {
					n++
				}
			}
			switch {
			case len(recs) == 0 || (n == 0 && goldenPath(ch.path)):
				issues[ch.path] = append(issues[ch.path], "change without record "+sha)
			case n > 1 && goldenPath(ch.path):
				issues[ch.path] = append(issues[ch.path], "change has multiple records "+sha)
			}
		}
		pc.points = append(pc.points, point{sha, parents[0], changed, recs})
	}
	return pc, nil
}
