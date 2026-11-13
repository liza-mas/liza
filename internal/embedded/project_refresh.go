package embedded

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
)

// HookRefreshReport describes what RefreshInstalledHooks did.
type HookRefreshReport struct {
	// Installed reports whether any hook set was found installed by init.
	Installed bool
	// Refreshed lists the hook scripts written: added, or rewritten because
	// their content or mode (not on Windows) differed from this binary's.
	Refreshed []string
	// Skipped lists managed script paths left alone because they are not
	// regular files (a symlink to a user script, for instance).
	Skipped []string
	// MalformedConfigs lists hook configs that could not be parsed; their hook
	// set is treated as not installed.
	MalformedConfigs []string
}

// installedHookSet is one provider's hook scripts and the hook config that
// proves init installed them.
type installedHookSet struct {
	dir      string
	contents map[string][]byte
	config   string
	// isAnchor reports whether a config string is a command init generates to
	// register this set's enforce-init.sh. A script name alone proves nothing:
	// a repository that never ran init can hold its own git-guard.sh.
	isAnchor func(string) bool
	// configOwned: init alone writes the config (a brand-named file).
	configOwned bool
}

// ownedAssetPresent reports whether an asset of ours sits beside the set's
// config: the config itself when only init writes it, else the set's
// enforce-init.sh. It decides only what is reported for a config that cannot
// be read, never whether anything is written.
func (s installedHookSet) ownedAssetPresent() bool {
	if s.configOwned {
		return true
	}
	_, err := os.Lstat(filepath.Join(s.dir, "enforce-init.sh"))
	return err == nil
}

func projectHookSets(projectRoot string) ([]installedHookSet, error) {
	claudeAnchor, err := embeddedAnchor(RenderedClaudeSettings(), ".claude/hooks/enforce-init.sh")
	if err != nil {
		return nil, fmt.Errorf("parse embedded claude-settings.json: %w", err)
	}
	copilotAnchor, err := embeddedAnchor(renderEmbeddedAsset(copilotHooksContent), ".copilot/hooks/enforce-init.sh")
	if err != nil {
		return nil, fmt.Errorf("parse embedded copilot-hooks.json: %w", err)
	}
	codexAnchor := func(command string) bool {
		return currentGeneratedCodexHookID(command) == "enforce-init.sh" || obsoleteGeneratedCodexHookID(command) == "enforce-init.sh"
	}
	return []installedHookSet{
		{claudeHooksDir(projectRoot), claudeHookContents, paths.New(projectRoot).ClaudeSettingsPath(), claudeAnchor, false},
		{codexHooksDir(projectRoot), codexHookContents, filepath.Join(codexDir(projectRoot), "hooks.json"), codexAnchor, false},
		{copilotHooksDir(projectRoot), copilotHookContents, copilotHooksConfigPath(projectRoot), copilotAnchor, true},
	}, nil
}

// embeddedAnchor matches the commands of the embedded hook config that name
// script.
func embeddedAnchor(config []byte, script string) (func(string) bool, error) {
	var parsed any
	if err := json.Unmarshal(config, &parsed); err != nil {
		return nil, err
	}
	anchors := map[string]bool{}
	walkJSONStrings(parsed, func(s string) {
		if strings.Contains(s, script) {
			anchors[s] = true
		}
	})
	return func(command string) bool { return anchors[command] }, nil
}

func walkJSONStrings(value any, visit func(string)) {
	switch v := value.(type) {
	case string:
		visit(v)
	case []any:
		for _, item := range v {
			walkJSONStrings(item, visit)
		}
	case map[string]any:
		for _, item := range v {
			walkJSONStrings(item, visit)
		}
	}
}

// installed reports whether the set's hook config registers its enforce-init.sh
// with a command init generates. A config that cannot be read or parsed
// reports false; it is an error, or recorded in report, only when it sits
// beside an asset of ours, since the command runs in every repository and a
// user's own broken settings are not ours to report.
func (s installedHookSet) installed(report *HookRefreshReport) (bool, error) {
	data, err := os.ReadFile(s.config)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		if !s.ownedAssetPresent() {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", s.config, err)
	}
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		if s.ownedAssetPresent() {
			report.MalformedConfigs = append(report.MalformedConfigs, s.config)
		}
		return false, nil
	}
	found := false
	walkJSONStrings(parsed, func(command string) { found = found || s.isAnchor(command) })
	return found, nil
}

// RefreshInstalledHooks brings the hook scripts of every hook set init
// installed in projectRoot to this binary's version, the bytes and mode a
// fresh init writes (DEV-960). A set counts as installed only when its hook
// config registers enforce-init.sh with a command init generates. Hook
// configs and files other than the managed scripts are never written, and a
// managed path that is not a regular file is skipped and reported.
func RefreshInstalledHooks(projectRoot string) (HookRefreshReport, error) {
	var report HookRefreshReport
	sets, err := projectHookSets(projectRoot)
	if err != nil {
		return report, err
	}
	for _, set := range sets {
		installed, err := set.installed(&report)
		if err != nil {
			return report, err
		}
		if !installed {
			continue
		}
		report.Installed = true
		for _, name := range slices.Sorted(maps.Keys(set.contents)) {
			path := filepath.Join(set.dir, name)
			want := renderEmbeddedAsset(set.contents[name])
			info, err := os.Lstat(path)
			switch {
			case os.IsNotExist(err):
			case err != nil:
				return report, fmt.Errorf("inspect %s: %w", path, err)
			case !info.Mode().IsRegular():
				report.Skipped = append(report.Skipped, path)
				continue
			default:
				current, err := os.ReadFile(path)
				if err != nil {
					return report, fmt.Errorf("read %s: %w", path, err)
				}
				// Executable, not exactly 0755: a git checkout applies the umask
				// (0775 under umask 002), and the hook commands need only -x.
				// Windows has no executable bit to compare.
				if bytes.Equal(current, want) && (runtime.GOOS == "windows" || info.Mode().Perm()&0o100 != 0) {
					continue
				}
			}
			// Atomic: running sessions execute these scripts.
			if err := writeFileAtomicMode(path, want, 0o755); err != nil {
				return report, fmt.Errorf("write %s: %w", path, err)
			}
			report.Refreshed = append(report.Refreshed, path)
		}
	}
	return report, nil
}
