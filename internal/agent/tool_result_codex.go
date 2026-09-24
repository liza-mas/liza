package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/paths"
)

// codexToolResultLaunch is scoped to one already-approved harness process.
// CLI uses Args before its subcommand; maintained ACP uses Wrapper as CODEX_PATH.
// Neither path mutates the user's Codex configuration or existing hooks.
type codexToolResultLaunch struct {
	Args    []string
	Wrapper string
	Cleanup func()
}

func prepareCodexToolResultLaunch(projectRoot, engineExecutable, codexExecutable string, prefix ...string) (codexToolResultLaunch, error) {
	var result codexToolResultLaunch
	// The managed boundary rewrites Bash through a POSIX shell wrapper; Windows
	// has no POSIX sh. Like the maintained ACP path, it therefore degrades to
	// the native launch without the tool-result boundary: the caller runs Codex
	// directly and oversized outputs are not captured there.
	if runtime.GOOS == "windows" {
		result.Wrapper = codexExecutable
		result.Cleanup = func() {}
		return result, nil
	}
	if !filepath.IsAbs(codexExecutable) {
		resolved, err := exec.LookPath(codexExecutable)
		if err != nil {
			return result, fmt.Errorf("cannot resolve Codex executable for managed tool-result boundary")
		}
		codexExecutable = resolved
	}
	if !filepath.IsAbs(projectRoot) || !filepath.IsAbs(engineExecutable) || !filepath.IsAbs(codexExecutable) {
		return result, fmt.Errorf("codex tool-result launch paths must be absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, codexExecutable, append(append([]string{}, prefix...), "--version")...).Output()
	if err != nil || !codexSupportsToolResultHooks(string(version)) {
		return result, fmt.Errorf("managed tool-result boundary requires Codex CLI 0.154.0 or newer")
	}
	root := filepath.Join(projectRoot, paths.ProjectDirName(), "tool-results")
	hookCommand := codexToolResultShellQuote(engineExecutable) + " tool-result --root " + codexToolResultShellQuote(root) + " codex-hook"
	encoded, _ := json.Marshal(hookCommand)
	// The hook rewrites Bash into the managed runner. Codex reports
	// bypassPermissions to that hook when the rendered config's
	// approval_policy="never" applies, and the same config keeps sandbox_mode
	// "workspace-write" — no launch-time permission override is passed, so the
	// OS-enforced sandbox stays active. --dangerously-bypass-hook-trust only
	// skips hash verification of the engine's own generated session hooks (the
	// app-server path achieves the same through hooks.state pinning); it does
	// not bypass approvals or the sandbox.
	result.Args = []string{"--dangerously-bypass-hook-trust", "-c", "features.hooks=true"}
	for _, event := range []struct{ name, matcher string }{{"PreToolUse", "Bash"}, {"PostToolUse", ".*"}} {
		config := fmt.Sprintf("hooks.%s=[{matcher=%q,hooks=[{type=\"command\",command=%s,timeout=30}]}]", event.name, event.matcher, encoded)
		result.Args = append(result.Args, "-c", config)
	}
	script := "#!/bin/sh\nexec " + codexToolResultShellQuote(codexExecutable)
	for _, arg := range append(append([]string{}, prefix...), result.Args...) {
		script += " " + codexToolResultShellQuote(arg)
	}
	script += " \"$@\"\n"
	result.Wrapper, err = persistToolResultScript(projectRoot, "codex", script)
	if err != nil {
		return result, err
	}
	result.Cleanup = func() {}
	return result, nil
}

// persistToolResultScript atomically publishes an immutable runtime script.
// ACP sessions retain it across prompt boundaries, so callers must not remove it.
func persistToolResultScript(projectRoot, prefix, script string) (string, error) {
	if !filepath.IsAbs(projectRoot) || prefix == "" || strings.Trim(prefix, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") != "" {
		return "", fmt.Errorf("invalid tool-result runtime script path")
	}
	dir := filepath.Join(projectRoot, paths.ProjectDirName(), "tool-result-runtime")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if info, e := os.Lstat(dir); e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("unsafe tool-result runtime directory")
	}
	hash := sha256.Sum256([]byte(script))
	path := filepath.Join(dir, fmt.Sprintf("%s-%x", prefix, hash))
	file, err := os.CreateTemp(dir, ".tool-result-script-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0700); err != nil {
		_ = file.Close()
		return "", err
	}
	_, writeErr := file.WriteString(script)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return "", fmt.Errorf("cannot persist tool-result runtime script")
	}
	if err = os.Link(file.Name(), path); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, statErr := os.Lstat(path)
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0700 {
		return "", fmt.Errorf("unsafe existing tool-result runtime script")
	}
	existing, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(existing, []byte(script)) {
		return "", fmt.Errorf("tool-result runtime script content mismatch")
	}
	return path, nil
}

func codexSupportsToolResultHooks(version string) bool {
	fields := strings.Fields(version)
	if len(fields) != 2 || fields[0] != "codex-cli" {
		return false
	}
	// A prerelease precedes its release, so it qualifies only when its release
	// is newer than 0.154.0: `0.160.0-alpha.1` passes, `0.154.0-alpha` does not.
	release, _, prerelease := strings.Cut(fields[1], "-")
	components := strings.Split(release, ".")
	if len(components) != 3 {
		return false
	}
	numbers := [3]int{}
	for i, part := range components {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
		numbers[i] = n
	}
	if prerelease {
		return numbers[0] > 0 || numbers[1] > 154 || (numbers[1] == 154 && numbers[2] > 0)
	}
	return numbers[0] > 0 || numbers[1] >= 154
}

func codexToolResultShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
