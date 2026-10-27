package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func (e *engine) compare(ctx context.Context, l ledger, out io.Writer) (int, error) {
	h, err := e.resolveHistory(ctx, l)
	if err != nil {
		return 2, fmt.Errorf("error history HEAD: %w", err)
	}
	// Only a package whose chain resolved is reproduced from history; any other
	// package runs under H1, where its unexplained paths keep their issues.
	var chained []string
	for _, dir := range e.excluded {
		if h.packages[dir] != nil {
			chained = append(chained, dir)
		}
	}
	baseline, err := e.values(ctx, "working", true, e.packages, dotted(chained))
	if err != nil {
		return 2, err
	}
	current, err := readGoldens(e.repo)
	if err != nil {
		return 2, err
	}
	r, err := e.packageRuns(ctx, h)
	if err != nil {
		return 2, err
	}
	reports := map[string]string{}
	var packageLines []string
	for _, dir := range chained {
		pc := h.packages[dir]
		lines, err := e.checkPackage(ctx, pc, current, h.issues, r)
		if err != nil {
			return 2, err
		}
		ok := true
		for p, line := range lines {
			reports[p] = line
			ok = ok && strings.HasPrefix(line, "ok ")
		}
		if ok {
			packageLines = append(packageLines, pc.line())
		}
	}
	paths := map[string]bool{}
	for p := range current {
		if selected(p, e.packages) {
			paths[p] = true
		}
	}
	for p := range baseline {
		paths[p] = true
	}
	for p := range h.issues {
		paths[p] = true
	}
	for p := range reports {
		paths[p] = true
	}
	var sorted []string
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	code := 0
	for _, p := range sorted {
		if line, ok := reports[p]; ok {
			fmt.Fprintln(out, line)
			if !strings.HasPrefix(line, "ok ") {
				code = 1
			}
			continue
		}
		if h.packages[packageDir(p)] != nil {
			continue
		}
		issues := h.issues[p]
		got, exists := current[p]
		want, generated := baseline[p]
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Fprintf(out, "unexplained %s: %s\n", p, issue)
			}
			code = 1
			continue
		}
		if !exists {
			fmt.Fprintf(out, "missing %s\n", p)
			code = 1
			continue
		}
		if !generated {
			fmt.Fprintf(out, "extra %s\n", p)
			code = 1
			continue
		}
		if !bytes.Equal(got, want) {
			fmt.Fprintf(out, "unexplained %s: H1 reproduction differs\n", p)
			code = 1
		} else {
			fmt.Fprintf(out, "ok %s H1\n", p)
		}
	}
	for _, line := range packageLines {
		fmt.Fprintln(out, line)
	}
	return code, nil
}

func (e *engine) writeNew(ctx context.Context, out io.Writer) error {
	values, err := e.values(ctx, "working", true, e.packages, dotted(e.excluded))
	if err != nil {
		return err
	}
	var paths []string
	for p := range values {
		// A C package is reproduced from its history, never written from H1.
		if !slices.Contains(e.excluded, packageDir(p)) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		dst := filepath.Join(e.repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		// O_EXCL also protects an existing file if it appears after discovery.
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := f.Write(values[p])
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(out, "wrote %s\n", p)
	}
	return nil
}
