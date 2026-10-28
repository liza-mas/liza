package pairingindex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// ManagedHookMarker identifies hook files owned by pairing index plumbing.
const ManagedHookMarker = "# PAIRING-INDEX-HOOK: managed"

// ManagedIndexScriptMarker identifies pairing index scripts owned by this tool.
const ManagedIndexScriptMarker = "# PAIRING-INDEX-SCRIPT: managed"

const legacyManagedIndexScriptMarker = "# LIZA-PAIRING-INDEX-SCRIPT: managed"

// hookNameEnvVar carries the git hook name from the wrapper to the dispatcher.
//
// When the hook is a symlink to the dispatcher, the dispatcher reads the name
// from $0. The wrapper installed where symlinks are unavailable execs the
// dispatcher by its own name, so $0 says the dispatcher filename and the
// post-checkout file-checkout short-circuit would never fire. The wrapper
// therefore states the name explicitly.
const hookNameEnvVar = "PAIRING_INDEX_HOOK_NAME"

func scriptName() string {
	return brand.RuntimeValues().BinaryName + "-index.sh"
}

func hookDispatcherName() string {
	return brand.RuntimeValues().BinaryName + "-index-hook.sh"
}

// InstallLifecycleHooks installs or verifies managed pairing index wrappers.
func InstallLifecycleHooks(opts InstallHooksOptions) (InstallHooksResult, error) {
	hooks := opts.Hooks
	if len(hooks) == 0 {
		hooks = DefaultLifecycleHooks()
	}

	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return InstallHooksResult{}, err
	}
	result := InstallHooksResult{HooksDir: hooksDir}

	if err := ensureHooksDir(hooksDir); err != nil {
		return result, err
	}
	if err := rejectHookCollisions(hooksDir, hooks); err != nil {
		return result, err
	}
	if _, err := installManagedHookDispatcher(filepath.Join(hooksDir, hookDispatcherName())); err != nil {
		return result, err
	}

	for _, hook := range hooks {
		hookPath := filepath.Join(hooksDir, hook)
		action, err := installManagedHook(hookPath, hook)
		if err != nil {
			return result, err
		}
		result.Hooks = append(result.Hooks, HookInstallResult{
			Hook:   hook,
			Path:   hookPath,
			Action: action,
		})
	}
	return result, nil
}

// InstallIndexScript installs or verifies the managed pairing index entrypoint.
func InstallIndexScript(opts InstallIndexScriptOptions) (InstallIndexScriptResult, error) {
	if !opts.DisableStacklit {
		if err := ensureStacklitArtifactCleanliness(opts.RepoRoot); err != nil {
			return InstallIndexScriptResult{}, err
		}
	}
	if err := ensureScipArtifactCleanliness(opts.RepoRoot, opts.ScipPlans); err != nil {
		return InstallIndexScriptResult{}, err
	}
	enableFunctionalClusters := opts.EnableFunctionalClusters && !opts.DisableStacklit && len(opts.ScipPlans) > 0
	if enableFunctionalClusters {
		if err := ensureFunctionalClustersArtifactCleanliness(opts.RepoRoot); err != nil {
			return InstallIndexScriptResult{}, err
		}
	}

	hooksDir, err := ResolveEffectiveHooksDir(opts.RepoRoot)
	if err != nil {
		return InstallIndexScriptResult{}, err
	}
	if err := ensureHooksDir(hooksDir); err != nil {
		return InstallIndexScriptResult{}, err
	}

	scriptPath := filepath.Join(hooksDir, scriptName())
	content, err := renderIndexScript(renderIndexScriptOptions{
		RepoRoot:                 opts.RepoRoot,
		EnableStacklit:           !opts.DisableStacklit,
		EnableFunctionalClusters: enableFunctionalClusters,
		ScipPlans:                opts.ScipPlans,
	})
	if err != nil {
		return InstallIndexScriptResult{}, err
	}
	action, err := installManagedIndexScript(scriptPath, content)
	if err != nil {
		return InstallIndexScriptResult{Path: scriptPath}, err
	}
	return InstallIndexScriptResult{Path: scriptPath, Action: action}, nil
}

func ensureHooksDir(hooksDir string) error {
	info, err := os.Stat(hooksDir)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("effective Git hooks path %q is not a directory", hooksDir)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("inspect effective Git hooks directory %q: %w", hooksDir, err)
	}
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("create effective Git hooks directory %q: %w", hooksDir, err)
	}
	return nil
}

// replaceExecutable writes content to a temporary file beside path and renames
// it into place. sh reads a script as it runs it, so rewriting the script, the
// dispatcher or a hook wrapper in place under a running hook would make that
// hook continue at its old byte offset in the new content. A rename leaves the
// running shell on the old inode.
func replaceExecutable(path, content string) error {
	staged, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	_, writeErr := staged.WriteString(content)
	closeErr := staged.Close()
	if err := errors.Join(writeErr, closeErr, os.Chmod(stagedPath, 0755)); err != nil {
		_ = os.Remove(stagedPath)
		return err
	}
	if err := os.Rename(stagedPath, path); err != nil {
		_ = os.Remove(stagedPath)
		return err
	}
	return nil
}

func installManagedIndexScript(scriptPath, want string) (HookAction, error) {
	name := scriptName()
	current, err := os.ReadFile(scriptPath)
	if os.IsNotExist(err) {
		if err := replaceExecutable(scriptPath, want); err != nil {
			return "", fmt.Errorf("install %s: %w", name, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if !managedIndexScriptOwned(string(current)) {
		if looksLikeLegacyIndexScript(string(current)) {
			return "", fmt.Errorf("%s at %s already exists and appears to be a legacy managed index hook; move it aside and rerun %s: mv %s %s.backup", name, scriptPath, brand.Command("init"), scriptPath, scriptPath)
		}
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", name, scriptPath, brand.NameTitle)
	}
	if string(current) == want {
		if err := os.Chmod(scriptPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s: %w", name, err)
		}
		return HookActionVerified, nil
	}
	if err := replaceExecutable(scriptPath, want); err != nil {
		return "", fmt.Errorf("update %s: %w", name, err)
	}
	return HookActionUpdated, nil
}

func managedIndexScriptOwned(content string) bool {
	return strings.Contains(content, ManagedIndexScriptMarker) ||
		strings.Contains(content, legacyManagedIndexScriptMarker)
}

func rejectHookCollisions(hooksDir string, hooks []string) error {
	var collisions []HookCollision
	for _, hook := range hooks {
		hookPath := filepath.Join(hooksDir, hook)
		managed, err := managedLifecycleHookExists(hookPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !managed {
			collisions = append(collisions, HookCollision{Hook: hook, Path: hookPath})
		}
	}
	if len(collisions) > 0 {
		return &HookCollisionError{Collisions: collisions}
	}
	return nil
}

func managedLifecycleHookExists(hookPath string) (bool, error) {
	info, err := os.Lstat(hookPath)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(hookPath)
		if err != nil {
			return false, err
		}
		if filepath.Base(target) == hookDispatcherName() {
			return true, nil
		}
	}
	content, err := os.ReadFile(hookPath)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(content), ManagedHookMarker) {
		return true, nil
	}
	return looksLikeLegacyHookDispatcher(string(content)), nil
}

func installManagedHookDispatcher(dispatcherPath string) (HookAction, error) {
	want := managedHookDispatcherContent(indexBinary())
	name := hookDispatcherName()
	current, err := os.ReadFile(dispatcherPath)
	if os.IsNotExist(err) {
		if err := replaceExecutable(dispatcherPath, want); err != nil {
			return "", fmt.Errorf("install %s: %w", name, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	if string(current) == want {
		if err := os.Chmod(dispatcherPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s: %w", name, err)
		}
		return HookActionVerified, nil
	}
	if !strings.Contains(string(current), ManagedHookMarker) && !looksLikeLegacyHookDispatcher(string(current)) {
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", name, dispatcherPath, brand.NameTitle)
	}
	if err := replaceExecutable(dispatcherPath, want); err != nil {
		return "", fmt.Errorf("update %s: %w", name, err)
	}
	return HookActionUpdated, nil
}

func installManagedHook(hookPath, hook string) (HookAction, error) {
	info, err := os.Lstat(hookPath)
	if os.IsNotExist(err) {
		if err := os.Symlink(hookDispatcherName(), hookPath); err != nil {
			return installManagedHookWrapper(hookPath, hook)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s hook: %w", hook, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(hookPath)
		if err != nil {
			return "", fmt.Errorf("read %s hook symlink: %w", hook, err)
		}
		if filepath.Base(target) == hookDispatcherName() {
			return HookActionVerified, nil
		}
	}

	managed, err := managedLifecycleHookExists(hookPath)
	if err != nil {
		return "", fmt.Errorf("read %s hook: %w", hook, err)
	}
	if !managed {
		return "", fmt.Errorf("%s at %s already exists and is not managed by %s", hook, hookPath, brand.NameTitle)
	}

	// Build the symlink beside the hook and move it into place, rather than
	// removing the hook first. os.Symlink cannot overwrite an existing file, but
	// removing before knowing whether the link can be created destroys a working
	// wrapper wherever symlinks are unavailable — Windows without Developer Mode
	// or an elevated shell, and filesystems that do not support them. The
	// wrapper fallback then sees no file, reinstalls from scratch and reports
	// "installed" on every run instead of "verified" or "updated".
	//
	// The staging name is unique per invocation (rather than a fixed
	// "<hook>.tmp") so this never deletes a file it did not create itself.
	dir := filepath.Dir(hookPath)
	stagingFile, err := os.CreateTemp(dir, filepath.Base(hookPath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("stage %s hook: %w", hook, err)
	}
	staged := stagingFile.Name()
	stagingFile.Close()
	if err := os.Remove(staged); err != nil {
		return "", fmt.Errorf("clear staged %s hook: %w", hook, err)
	}
	if err := os.Symlink(hookDispatcherName(), staged); err != nil {
		return installManagedHookWrapper(hookPath, hook)
	}
	if err := os.Rename(staged, hookPath); err != nil {
		if removeErr := os.Remove(staged); removeErr != nil && !os.IsNotExist(removeErr) {
			return "", fmt.Errorf("replace %s hook: %w (and clearing %s failed: %v)", hook, err, staged, removeErr)
		}
		return "", fmt.Errorf("replace %s hook: %w", hook, err)
	}
	return HookActionUpdated, nil
}

func installManagedHookWrapper(hookPath, hook string) (HookAction, error) {
	want := managedHookContent(hook)
	current, err := os.ReadFile(hookPath)
	if os.IsNotExist(err) {
		if err := replaceExecutable(hookPath, want); err != nil {
			return "", fmt.Errorf("install %s hook wrapper: %w", hook, err)
		}
		return HookActionInstalled, nil
	}
	if err != nil {
		return "", fmt.Errorf("read %s hook wrapper: %w", hook, err)
	}
	if string(current) == want {
		if err := os.Chmod(hookPath, 0755); err != nil {
			return "", fmt.Errorf("chmod %s hook wrapper: %w", hook, err)
		}
		return HookActionVerified, nil
	}
	if err := replaceExecutable(hookPath, want); err != nil {
		return "", fmt.Errorf("update %s hook wrapper: %w", hook, err)
	}
	return HookActionUpdated, nil
}

func managedHookContent(hook string) string {
	return fmt.Sprintf(`#!/bin/sh
%s
# Hook: %s
hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 0
dispatcher="$hook_dir/%s"
if [ -x "$dispatcher" ]; then
	%s=%s "$dispatcher" "$@"
fi
exit 0
		`, ManagedHookMarker, hook, hookDispatcherName(), hookNameEnvVar, shellQuote(hook))
}

// managedHookDispatcherContent routes every lifecycle hook through the
// coordinator (RunRefresh), which serializes refreshes with merge-triggered
// ones. When indexBinary no longer exists it falls back to the binary on PATH;
// with neither it skips the refresh rather than run the script unserialized.
func managedHookDispatcherContent(indexBinary string) string {
	return fmt.Sprintf(`#!/bin/sh
%s
set -eu

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$repo_root" ]; then
	exit 0
fi

hook_name="${%s:-$(basename "$0")}"
unset %s
if [ "$hook_name" = "post-checkout" ] && [ "${3:-}" = "0" ]; then
	exit 0
fi

case "$repo_root" in
	*/.worktrees/*)
		exit 0
		;;
esac

hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd) || exit 0
script="$hook_dir/%s"
if [ ! -x "$script" ]; then
	exit 0
fi

cd "$repo_root"
semble_gate=$(printf '%%s' "${%s-${LIZA_ENABLE_SEMBLE:-}}" | tr '[:upper:]' '[:lower:]' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
if [ "$hook_name" = "post-commit" ] && [ "$semble_gate" != "1" ] && [ "$semble_gate" != "true" ]; then
	# Inspect root commits and every merge parent. Disabling rename detection
	# keeps code renamed to documentation visible as a code deletion.
	# --quiet does not reliably report merge differences. Check the path list;
	# only a successful empty result skips refresh, and errors fall through.
	if changed_paths="$(git diff-tree --root --no-commit-id --name-only -r -m --no-renames HEAD -- . \
		':(glob,exclude,icase)**/*.md' \
		':(glob,exclude,icase)**/*.rst' \
		':(glob,exclude,icase)**/*.adoc' \
		':(glob,exclude,icase)specs/plans/**/*-output.json' \
		':(glob,exclude,icase)specs/epics/**/*-output.json' \
		':(glob,exclude,icase)specs/arch-plan/**/*-output.json' 2>/dev/null)" && [ -z "$changed_paths" ]; then
		exit 0
	fi
fi

%s%s
if [ -n "$index_binary" ] && [ ! -x "$index_binary" ]; then
	index_binary="$(command -v %s 2>/dev/null || true)"
fi
if [ -z "$index_binary" ]; then
	echo "%s: %s not found; skipping index refresh" >&2
	exit 0
fi
exec "$index_binary" %s --trigger "$hook_name"
`, ManagedHookMarker, hookNameEnvVar, hookNameEnvVar, scriptName(), brand.EnvName("ENABLE_SEMBLE"),
		indexBinaryAssignment, shellQuote(indexBinary),
		shellWord(brand.RuntimeValues().BinaryName),
		hookDispatcherName(), brand.RuntimeValues().BinaryName,
		RefreshCommandName)
}

func looksLikeLegacyHookDispatcher(content string) bool {
	return strings.Contains(content, `repo_root="$(git rev-parse --show-toplevel`) &&
		strings.Contains(content, "post-checkout") &&
		strings.Contains(content, ".worktrees") &&
		strings.Contains(content, "liza-index.sh")
}

func looksLikeLegacyIndexScript(content string) bool {
	return strings.Contains(content, "liza pairing mode") ||
		(strings.Contains(content, "scip-") && strings.Contains(content, "stacklit"))
}

func outputSuffix(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	return ": " + output
}
