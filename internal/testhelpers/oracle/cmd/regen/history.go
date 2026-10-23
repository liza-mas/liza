package main

import (
	"context"
	"fmt"
	"strings"
)

type point struct{ sha, parent, reason string }
type history struct {
	chains map[string][]point
	issues map[string][]string
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
		fields := strings.Split(strings.TrimPrefix(line, "+"), " ")
		if len(fields) == 3 {
			result = append(result, change{fields[1], fields[2]})
		}
	}
	return result, nil
}

func (e *engine) resolveHistory(ctx context.Context, l ledger) (history, error) {
	h := history{chains: map[string][]point{}, issues: map[string][]string{}}
	chained := map[string]bool{}
	for _, c := range l.changes {
		if isC(c.reason) && selected(c.golden, e.packages) {
			chained[c.golden] = true
		}
	}
	// Check records independently of golden history: a record-only commit
	// would otherwise be invisible to git log -- <golden>.
	b, err := e.commands.git(ctx, e.repo, "log", "--format=%H", "HEAD", "--", ledgerPath)
	if err != nil {
		return h, err
	}
	for _, sha := range strings.Fields(string(b)) {
		parents, err := e.parents(ctx, sha)
		if err != nil {
			return h, err
		}
		added, err := e.recordsAdded(ctx, sha, parents)
		if err != nil {
			return h, err
		}
		for _, c := range added {
			if !selected(c.golden, e.packages) {
				continue
			}
			if len(parents) == 0 {
				continue
			}
			changed, err := e.commands.git(ctx, e.repo, "diff", "--name-only", parents[0], sha, "--", c.golden)
			if err != nil {
				return h, err
			}
			if len(changed) == 0 {
				h.issues[c.golden] = append(h.issues[c.golden], "record without change "+sha)
			}
		}
	}
	for golden := range chained {
		b, err := e.commands.git(ctx, e.repo, "log", "--reverse", "--format=%H", "HEAD", "--", golden)
		if err != nil {
			return h, err
		}
		started := false
		for _, sha := range strings.Fields(string(b)) {
			parents, err := e.parents(ctx, sha)
			if err != nil {
				return h, err
			}
			added, err := e.recordsAdded(ctx, sha, parents)
			if err != nil {
				return h, err
			}
			var records []change
			for _, c := range added {
				if c.golden == golden {
					records = append(records, c)
				}
			}
			for _, c := range records {
				if isC(c.reason) {
					started = true
				}
			}
			if !started {
				continue
			}
			switch {
			case len(parents) != 1:
				h.issues[golden] = append(h.issues[golden], "merge changes golden "+sha)
			case len(records) == 0:
				h.issues[golden] = append(h.issues[golden], "change without record "+sha)
			case len(records) != 1:
				h.issues[golden] = append(h.issues[golden], "change has multiple records "+sha)
			default:
				h.chains[golden] = append(h.chains[golden], point{sha, parents[0], records[0].reason})
			}
		}
		if !started {
			h.issues[golden] = append(h.issues[golden], "C record has no change point")
		}
	}
	return h, nil
}
