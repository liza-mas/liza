package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type engine struct {
	repo, root string
	commands   commands
	regenerate regenerator
	packages   []string
	trees      map[string]map[string][]byte
	worktrees  []string
}

func (e *engine) cleanup() error {
	// A cancelled regeneration still removes registered worktrees. Cleanup
	// has its own bounded context rather than inheriting the cancelled one.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var errs []error
	for i := len(e.worktrees) - 1; i >= 0; i-- {
		_, err := e.commands.git(ctx, e.repo, "worktree", "remove", "--force", e.worktrees[i])
		if err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		errs = append(errs, os.RemoveAll(e.root))
	}
	return errors.Join(errs...)
}

func (e *engine) materialise(ctx context.Context, source string, baseline bool) (string, error) {
	commit := source
	var l ledger
	var err error
	if baseline {
		data, readErr := e.oracleFile(ctx, source, ledgerPath)
		if readErr != nil {
			return "", fmt.Errorf("error overlay %s: %w", source, readErr)
		}
		l, err = parseLedger(data)
		if err != nil {
			return "", err
		}
		commit = l.start
	}
	dir := filepath.Join(e.root, fmt.Sprintf("%d", len(e.worktrees)))
	if _, err = e.commands.git(ctx, e.repo, "worktree", "add", "--detach", dir, commit); err != nil {
		return "", fmt.Errorf("error worktree %s: %w", commit, err)
	}
	e.worktrees = append(e.worktrees, dir)
	if !baseline {
		return dir, nil
	}
	for _, fix := range l.fixes {
		patch, err := e.commands.git(ctx, e.repo, "diff", "--binary", fix+"^1", fix, "--", ".", ":(exclude)internal/oracle")
		if err != nil {
			return "", fmt.Errorf("error fix-apply %s: %w", fix, err)
		}
		if len(patch) == 0 {
			continue
		}
		if _, err := e.commands.output(ctx, dir, "git", patch, nil, "apply", "--binary"); err != nil {
			return "", fmt.Errorf("error fix-apply %s: %w", fix, err)
		}
	}
	for _, name := range []string{"internal/oracle", "internal/testhelpers/oracle"} {
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.RemoveAll(dst); err != nil {
			return "", fmt.Errorf("error overlay %s: %w", source, err)
		}
		if source == "working" {
			src := filepath.Join(e.repo, filepath.FromSlash(name))
			if _, err := os.Stat(src); os.IsNotExist(err) {
				continue
			}
			if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
				return "", fmt.Errorf("error overlay working: %w", err)
			}
		} else {
			files, err := e.commands.git(ctx, e.repo, "ls-tree", "-r", "--name-only", source, "--", name)
			if err != nil {
				return "", fmt.Errorf("error overlay %s: %w", source, err)
			}
			if len(files) == 0 {
				continue
			}
			if _, err := e.commands.git(ctx, dir, "checkout", source, "--", name); err != nil {
				return "", fmt.Errorf("error overlay %s: %w", source, err)
			}
		}
	}
	return dir, nil
}

func (e *engine) oracleFile(ctx context.Context, source, name string) ([]byte, error) {
	if source == "working" {
		return os.ReadFile(filepath.Join(e.repo, filepath.FromSlash(name)))
	}
	return e.commands.git(ctx, e.repo, "show", source+":"+name)
}

func (e *engine) values(ctx context.Context, source string, baseline bool) (map[string][]byte, error) {
	key := fmt.Sprintf("%t/%s", baseline, source)
	if value, ok := e.trees[key]; ok {
		return value, nil
	}
	dir, err := e.materialise(ctx, source, baseline)
	if err != nil {
		return nil, err
	}
	value, err := e.regenerate(ctx, dir, source, baseline, e.packages)
	if err != nil {
		return nil, fmt.Errorf("error regeneration %s: %w", source, err)
	}
	for name := range value {
		if !goldenPath(name) {
			return nil, fmt.Errorf("error go-test %s: invalid golden path", source)
		}
		if !selected(name, e.packages) {
			delete(value, name)
		}
	}
	e.trees[key] = value
	return value, nil
}

// Package patterns follow Go's path convention: ... matches any substring;
// exact package paths do not select child packages.
func selected(golden string, packages []string) bool {
	dir, _, _ := strings.Cut(golden, "/testdata/golden/")
	for _, pattern := range packages {
		pattern = strings.TrimPrefix(pattern, "./")
		suffix := ""
		if strings.HasSuffix(pattern, "/...") {
			pattern = strings.TrimSuffix(pattern, "/...")
			suffix = "(/.*)?"
		}
		expression := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\.\.\.`, ".*") + suffix + "$"
		if match, _ := regexp.MatchString(expression, dir); match {
			return true
		}
	}
	return false
}
