package embedded

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// treeSnapshot maps every path under root to its mode and content (a symlink
// to its target), so a test can assert nothing under root changed.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot[rel] = "link:" + target
		case info.IsDir():
			snapshot[rel] = "dir"
		default:
			content, err := os.ReadFile(path)
			if os.IsPermission(err) {
				snapshot[rel] = fmt.Sprintf("%v:<unreadable>", info.Mode().Perm())
				return nil
			}
			if err != nil {
				return err
			}
			snapshot[rel] = fmt.Sprintf("%v:%s", info.Mode().Perm(), content)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertSnapshotUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	after := treeSnapshot(t, root)
	for path, value := range after {
		if before[path] != value {
			t.Errorf("%s changed or was created", path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			t.Errorf("%s was removed", path)
		}
	}
}

func writeTestFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func TestRefreshInstalledHooksLeavesUnpreparedProjectAlone(t *testing.T) {
	cases := map[string]func(t *testing.T, root string){
		// Script names alone prove nothing: a repository can hold its own git-guard.sh.
		"same-named user scripts": func(t *testing.T, root string) {},
		// A registration of a script init also writes, but not of its gate.
		"user registration of a non-gate script": func(t *testing.T, root string) {
			writeTestFile(t, filepath.Join(root, ".claude", "settings.json"),
				`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"bash \"$CLAUDE_PROJECT_DIR/.claude/hooks/git-guard.sh\""}]}]}}`, 0o644)
		},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			// GIVEN a project init never prepared, holding user scripts named like ours
			root := t.TempDir()
			writeTestFile(t, filepath.Join(root, ".claude", "hooks", "git-guard.sh"), "#!/bin/bash\n# user guard\n", 0o755)
			writeTestFile(t, filepath.Join(root, ".codex", "hooks", "session-context.sh"), "#!/bin/bash\n# user context\n", 0o755)
			extra(t, root)
			before := treeSnapshot(t, root)

			// WHEN refreshing
			report, err := RefreshInstalledHooks(root)

			// THEN nothing is written or created
			if err != nil {
				t.Fatal(err)
			}
			if report.Installed || len(report.Refreshed) != 0 || len(report.Skipped) != 0 {
				t.Fatalf("report = %+v, want nothing installed or refreshed", report)
			}
			assertSnapshotUnchanged(t, root, before)
		})
	}
}

// hookInstall is one provider's real init writer and the files it installs.
type hookInstall struct {
	install  func(root string) error
	hooksDir func(root string) string
	configs  func(root string) []string
}

var hookInstalls = map[string]hookInstall{
	"claude": {
		func(root string) error {
			return WriteClaudeSettingsWithOptions(root, nil, ConfirmOptions{AutoConfirm: true})
		},
		claudeHooksDir,
		func(root string) []string { return []string{filepath.Join(root, ".claude", "settings.json")} },
	},
	"codex": {
		func(root string) error {
			return WriteCodexProjectHooksWithOptions(root, nil, ConfirmOptions{AutoConfirm: true})
		},
		codexHooksDir,
		func(root string) []string {
			return []string{filepath.Join(codexDir(root), "hooks.json"), filepath.Join(codexDir(root), "config.toml")}
		},
	},
	"copilot": {
		func(root string) error {
			return WriteCopilotProjectHooksWithOptions(root, nil, ConfirmOptions{AutoConfirm: true})
		},
		copilotHooksDir,
		func(root string) []string { return []string{copilotHooksConfigPath(root)} },
	},
}

// dirFiles returns name → mode:content for the regular files in dir.
func dirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = fmt.Sprintf("%v:%s", info.Mode().Perm(), content)
	}
	return files
}

func TestRefreshInstalledHooksConvergesRealInstallationsToFreshInit(t *testing.T) {
	for name, provider := range hookInstalls {
		t.Run(name, func(t *testing.T) {
			// GIVEN a fresh init as the oracle, and an older installation by the same writer
			fresh := t.TempDir()
			if err := provider.install(fresh); err != nil {
				t.Fatal(err)
			}
			want := dirFiles(t, provider.hooksDir(fresh))
			names := slices.Sorted(maps.Keys(want))
			if len(names) < 4 {
				t.Fatalf("fresh init installed %v, want at least 4 scripts", names)
			}

			root := t.TempDir()
			if err := provider.install(root); err != nil {
				t.Fatal(err)
			}
			dir := provider.hooksDir(root)
			stale, missing, notExecutable := filepath.Join(dir, names[0]), filepath.Join(dir, names[1]), filepath.Join(dir, names[2])
			writeTestFile(t, stale, "#!/bin/bash\n# an older release\n", 0o755)
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(notExecutable, 0o644); err != nil {
				t.Fatal(err)
			}
			// A git checkout under umask 002 leaves current scripts 0775.
			groupWritable := filepath.Join(dir, names[3])
			if err := os.Chmod(groupWritable, 0o775); err != nil {
				t.Fatal(err)
			}
			userScript := filepath.Join(dir, "my-hook.sh")
			writeTestFile(t, userScript, "#!/bin/bash\n# mine\n", 0o700)
			configs := map[string]string{}
			for _, path := range provider.configs(root) {
				configs[path] = readFile(t, path)
			}

			// WHEN refreshing
			report, err := RefreshInstalledHooks(root)

			// THEN the managed scripts equal a fresh init, and only the changed ones are listed
			if err != nil {
				t.Fatal(err)
			}
			wantRefreshed := []string{stale, missing, notExecutable}
			slices.Sort(wantRefreshed)
			if !report.Installed || !slices.Equal(report.Refreshed, wantRefreshed) {
				t.Fatalf("report = %+v, want installed with refreshed %v", report, wantRefreshed)
			}
			got := dirFiles(t, dir)
			for _, name := range names {
				if name == names[3] {
					// Current bytes and executable: left as the checkout made it.
					if wantContent := strings.SplitN(want[name], ":", 2)[1]; got[name] != "-rwxrwxr-x:"+wantContent {
						t.Errorf("%s was rewritten or changed: %q", name, got[name])
					}
					continue
				}
				if got[name] != want[name] {
					t.Errorf("%s differs from a fresh init", name)
				}
			}
			if got["my-hook.sh"] != "-rwx------:#!/bin/bash\n# mine\n" {
				t.Errorf("user script changed: %q", got["my-hook.sh"])
			}
			for path, content := range configs {
				if readFile(t, path) != content {
					t.Errorf("hook config %s was written", path)
				}
			}

			// AND a second run changes nothing
			again, err := RefreshInstalledHooks(root)
			if err != nil || len(again.Refreshed) != 0 {
				t.Fatalf("second run refreshed %v err=%v", again.Refreshed, err)
			}
		})
	}
}

func TestRefreshInstalledHooksRecognisesObsoleteCodexRegistration(t *testing.T) {
	// GIVEN a Codex install from a release that registered the gate without the root guard
	fresh := t.TempDir()
	if err := WriteCodexHooks(fresh); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(codexDir(root), "hooks.json"),
		`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"bash \"$(git rev-parse --show-toplevel)/.codex/hooks/enforce-init.sh\""}]}]}}`, 0o644)
	writeTestFile(t, filepath.Join(codexHooksDir(root), "enforce-init.sh"), "#!/bin/bash\nexit 2\n", 0o755)

	// WHEN refreshing
	report, err := RefreshInstalledHooks(root)

	// THEN the set counts as installed and its scripts equal a fresh install
	if err != nil || !report.Installed {
		t.Fatalf("report = %+v err=%v, want installed", report, err)
	}
	want, got := dirFiles(t, codexHooksDir(fresh)), dirFiles(t, codexHooksDir(root))
	if len(got) != len(want) {
		t.Fatalf("installed %d scripts, want %d", len(got), len(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s differs from a fresh install", name)
		}
	}
}

func TestRefreshInstalledHooksSkipsMalformedConfig(t *testing.T) {
	// GIVEN a Claude install whose settings no longer parse
	root := t.TempDir()
	if err := WriteClaudeSettingsWithOptions(root, nil, ConfirmOptions{AutoConfirm: true}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(root, ".claude", "settings.json")
	writeTestFile(t, settings, "{", 0o644)
	writeTestFile(t, filepath.Join(claudeHooksDir(root), "enforce-init.sh"), "#!/bin/bash\nexit 2\n", 0o755)
	before := treeSnapshot(t, root)

	// WHEN refreshing
	report, err := RefreshInstalledHooks(root)

	// THEN nothing is written and the config is named
	if err != nil {
		t.Fatal(err)
	}
	if report.Installed || !slices.Equal(report.MalformedConfigs, []string{settings}) {
		t.Fatalf("report = %+v, want not installed with %s malformed", report, settings)
	}
	assertSnapshotUnchanged(t, root, before)
}

func TestRefreshInstalledHooksIgnoresBrokenConfigsItDoesNotOwn(t *testing.T) {
	cases := map[string]func(t *testing.T, config string){
		"malformed": func(t *testing.T, config string) { writeTestFile(t, config, "{", 0o644) },
		"unreadable": func(t *testing.T, config string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads any file")
			}
			writeTestFile(t, config, "{}", 0o000)
		},
	}
	for name, breakConfig := range cases {
		t.Run(name, func(t *testing.T) {
			// GIVEN a project init never prepared, whose own Claude and Codex configs are broken
			root := t.TempDir()
			for _, config := range []string{filepath.Join(root, ".claude", "settings.json"), filepath.Join(codexDir(root), "hooks.json")} {
				breakConfig(t, config)
			}
			before := treeSnapshot(t, root)

			// WHEN refreshing
			report, err := RefreshInstalledHooks(root)

			// THEN nothing is reported and nothing is written
			if err != nil {
				t.Fatal(err)
			}
			if report.Installed || len(report.MalformedConfigs) != 0 || len(report.Refreshed) != 0 {
				t.Fatalf("report = %+v, want nothing", report)
			}
			assertSnapshotUnchanged(t, root, before)
		})
	}
}

func TestRefreshInstalledHooksSkipsSymlinkedScript(t *testing.T) {
	// GIVEN a Claude install whose session-context.sh links to a user script
	root := t.TempDir()
	if err := WriteClaudeSettingsWithOptions(root, nil, ConfirmOptions{AutoConfirm: true}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "my-context.sh")
	writeTestFile(t, target, "#!/bin/bash\n# mine\n", 0o755)
	link := filepath.Join(claudeHooksDir(root), "session-context.sh")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, root)

	// WHEN refreshing
	report, err := RefreshInstalledHooks(root)

	// THEN the link and its target are left alone and the link is named
	if err != nil {
		t.Fatal(err)
	}
	if !report.Installed || !slices.Equal(report.Skipped, []string{link}) || len(report.Refreshed) != 0 {
		t.Fatalf("report = %+v, want %s skipped and nothing refreshed", report, link)
	}
	assertSnapshotUnchanged(t, root, before)
	if readFile(t, target) != "#!/bin/bash\n# mine\n" {
		t.Fatal("symlink target was written")
	}
}
