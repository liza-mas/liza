package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type regenerator func(context.Context, string, string, bool, []string) (map[string][]byte, error)

// Every entry-point child, including Git reads and cleanup, uses this constructor.
type commands struct{ home, path, tmp string }

func (c commands) command(ctx context.Context, dir, name string, extra []string, args ...string) (*exec.Cmd, error) {
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: c.home, Path: c.path, Tmp: c.tmp, Go: name == "go"}, extra...)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	return cmd, nil
}

func (c commands) output(ctx context.Context, dir, name string, input []byte, extra []string, args ...string) ([]byte, error) {
	cmd, err := c.command(ctx, dir, name, extra, args...)
	if err != nil {
		return nil, err
	}
	if input != nil {
		cmd.Stdin = strings.NewReader(string(input))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Diagnostics are bounded, and never disclose values from the policy
		// environment, even when a tool prints an absolute build path.
		detail := string(out)
		// Only policy entries can contain host values. Extras are explicit
		// constants such as ORACLE_WRITE=1; replacing "1" corrupts test names.
		for _, kv := range cmd.Env[:len(cmd.Env)-len(extra)] {
			name, value, _ := strings.Cut(kv, "=")
			if value != "" && name != "GOFLAGS" {
				detail = strings.ReplaceAll(detail, value, "<"+name+">")
			}
		}
		if len(detail) > 8192 {
			detail = "...\n" + detail[len(detail)-8192:]
		}
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(detail))
	}
	return out, nil
}

func (c commands) git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	return c.output(ctx, repo, "git", nil, nil, args...)
}

func (c commands) regenerate(ctx context.Context, dir, commit string, baseline bool, packages []string) (map[string][]byte, error) {
	files, err := readGoldens(dir)
	if err != nil {
		return nil, fmt.Errorf("error go-test %s: %w", commit, err)
	}
	for name := range files {
		if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return nil, err
		}
	}
	if _, err := c.output(ctx, dir, "go", nil, nil, "run", "./internal/brandrender/cmd/sync-embedded", "--repo-root", "."); err != nil {
		return nil, fmt.Errorf("error sync %s: %w", commit, err)
	}
	args := []string{"test", "-count=1"}
	if baseline {
		args = append(args, "-tags", "oraclestart")
	}
	args = append(args, packages...)
	if _, err := c.output(ctx, dir, "go", nil, []string{"ORACLE_WRITE=1"}, args...); err != nil {
		return nil, fmt.Errorf("error go-test %s: %w", commit, err)
	}
	return readGoldens(dir)
}

func readGoldens(repo string) (map[string][]byte, error) {
	result := map[string][]byte{}
	err := filepath.WalkDir(filepath.Join(repo, "internal/oracle"), func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(repo, name)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() || !goldenPath(rel) {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("golden is not a regular file: %s", rel)
		}
		data, err := os.ReadFile(name)
		if err == nil {
			result[rel] = data
		}
		return err
	})
	return result, err
}
