package control

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestOracleCleanup(t *testing.T) {
	runCases(t, "cleanup", []controlCase{
		{name: "yes", fixture: "no-live-agent", args: []string{"cleanup", "--yes"}, worktree: true},
		{name: "refused-stdin", fixture: "no-live-agent", args: []string{"cleanup"}, code: 1, worktree: true},
	})
}

func runtimeFile(name string) bool {
	return strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".lock.owner.json") || name == "log.yaml" || strings.HasPrefix(name, "alerts.log")
}

func (p *project) manifest(t *testing.T) []byte {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(p.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(p.dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == ".git" {
			return filepath.SkipDir
		}
		if runtimeFile(entry.Name()) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		kind, hash := "f", ""
		switch {
		case entry.IsDir():
			kind, hash = "d", "-"
		case info.Mode()&os.ModeSymlink != 0:
			kind = "l"
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hash = string(p.normalise(t, "manifest symlink", []byte(target)))
		default:
			// D-4 precedes hashing: generated times and disposable paths must
			// not hide a content change behind a run-dependent digest.
			content := p.normalise(t, "manifest "+filepath.ToSlash(rel), mustRead(t, path))
			hash = fmt.Sprintf("%x", sha256.Sum256(content))
		}
		entries = append(entries, fmt.Sprintf("%s %s %04o %s", filepath.ToSlash(rel), kind, info.Mode().Perm(), hash))
		return nil
	})
	if err != nil {
		t.Fatal("walk cleanup project manifest")
	}
	sort.Strings(entries)
	git := p.git(t, "for-each-ref", "--format=%(refname) %(objectname)") + p.git(t, "worktree", "list", "--porcelain")
	git = string(p.normalise(t, "manifest git", []byte(git)))
	lines := strings.Split(strings.TrimSuffix(git, "\n"), "\n")
	sort.Strings(lines)
	return []byte(strings.Join(entries, "\n") + "\n# git\n" + strings.Join(lines, "\n") + "\n")
}
