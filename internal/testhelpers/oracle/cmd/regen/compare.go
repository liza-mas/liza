package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (e *engine) compare(ctx context.Context, l ledger, out io.Writer) (int, error) {
	h, err := e.resolveHistory(ctx, l)
	if err != nil {
		return 2, fmt.Errorf("error history HEAD: %w", err)
	}
	baseline, err := e.values(ctx, "working", true)
	if err != nil {
		return 2, err
	}
	current, err := readGoldens(e.repo)
	if err != nil {
		return 2, err
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
	for p := range h.chains {
		paths[p] = true
	}
	var sorted []string
	for p := range paths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	code := 0
	for _, p := range sorted {
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
		chain := h.chains[p]
		if len(chain) == 0 {
			if !bytes.Equal(got, want) {
				fmt.Fprintf(out, "unexplained %s: H1 reproduction differs\n", p)
				code = 1
			} else {
				fmt.Fprintf(out, "ok %s H1\n", p)
			}
			continue
		}
		issue, err := e.checkChain(ctx, p, got, chain)
		if err != nil {
			return 2, err
		}
		if issue != "" {
			fmt.Fprintf(out, "unexplained %s: %s\n", p, issue)
			code = 1
			continue
		}
		var links []string
		for _, point := range chain {
			links = append(links, point.reason, point.sha)
		}
		fmt.Fprintf(out, "ok %s chain pre %s %s\n", p, chain[0].parent, strings.Join(links, " "))
	}
	return code, nil
}

func (e *engine) checkChain(ctx context.Context, golden string, current []byte, chain []point) (string, error) {
	pre := chain[0].parent
	values, err := e.values(ctx, pre, true)
	if err != nil {
		return "", err
	}
	blob, err := e.oracleFile(ctx, pre, golden)
	if err != nil {
		return "pre-change blob absent " + pre, nil
	}
	value, ok := values[golden]
	if !ok || !bytes.Equal(value, blob) {
		return "pre-change reproduction differs " + pre, nil
	}
	for _, point := range chain {
		values, err := e.values(ctx, point.sha, false)
		if err != nil {
			return "", err
		}
		blob, err = e.oracleFile(ctx, point.sha, golden)
		if err != nil {
			return "change point blob absent " + point.sha, nil
		}
		value, ok := values[golden]
		if !ok || !bytes.Equal(value, blob) {
			return "change point reproduction differs " + point.sha, nil
		}
	}
	if !bytes.Equal(current, blob) {
		return "working tree differs from last change point " + chain[len(chain)-1].sha, nil
	}
	return "", nil
}

func (e *engine) writeNew(ctx context.Context, out io.Writer) error {
	values, err := e.values(ctx, "working", true)
	if err != nil {
		return err
	}
	var paths []string
	for p := range values {
		paths = append(paths, p)
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
