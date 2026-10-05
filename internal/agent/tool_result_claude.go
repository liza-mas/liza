package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/paths"
)

// ClaudeToolResultSettings is an additive, launch-local settings overlay. It
// does not edit user/project settings or grant permissions. Pass the result as
// Claude's --settings argument for both interactive and non-interactive runs.
func ClaudeToolResultSettings(executable, projectRoot string) (string, error) {
	if executable == "" || projectRoot == "" {
		return "", fmt.Errorf("claude result boundary requires executable and project root")
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return "", err
	}
	projectRoot, err = filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	command := quote(executable) + " tool-result --root " + quote(filepath.Join(projectRoot, paths.ProjectDirName(), "tool-results")) + " claude-hook"
	hook := func(matcher string) []any {
		return []any{map[string]any{"matcher": matcher, "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 60}}}}
	}
	payload, err := json.Marshal(map[string]any{"hooks": map[string]any{"PostToolUse": hook("Read|Grep|Glob|mcp__.*"), "PostToolBatch": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 60}}}}}})
	return string(payload), err
}

// validateClaudeToolResultBoundary refuses versions that silently ignore native
// rewrites and startup settings that would suppress the mandatory hook overlay.
func validateClaudeToolResultBoundary(ctx context.Context, executable, projectRoot string, args []string) error {
	for _, arg := range args {
		if arg == "--bare" || arg == "--settings" || strings.HasPrefix(arg, "--settings=") {
			return fmt.Errorf("claude tool-result boundary cannot safely combine with startup hook-overriding settings")
		}
	}
	versionCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, executable, "--version").Output()
	if err != nil {
		return fmt.Errorf("cannot verify Claude tool-result hook support")
	}
	match := regexp.MustCompile(`(?:^|\s)([0-9]+)\.([0-9]+)\.([0-9]+)(?:\s|$)`).FindStringSubmatch(string(output))
	if len(match) != 4 {
		return fmt.Errorf("cannot identify Claude version for tool-result hooks")
	}
	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch, _ := strconv.Atoi(match[3])
	if major < 2 || major == 2 && (minor < 1 || minor == 1 && patch < 267) {
		return fmt.Errorf("native Claude tool-result hooks require Claude Code 2.1.267 or newer")
	}
	settings := []string{filepath.Join(projectRoot, ".claude", "settings.json"), filepath.Join(projectRoot, ".claude", "settings.local.json")}
	if home, err := os.UserHomeDir(); err == nil {
		settings = append(settings, filepath.Join(home, ".claude", "settings.json"))
	}
	for _, path := range settings {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot check Claude hook activation settings")
		}
		var value map[string]any
		if json.Unmarshal(data, &value) != nil {
			return fmt.Errorf("invalid Claude hook activation settings")
		}
		if disabled, _ := value["disableAllHooks"].(bool); disabled {
			return fmt.Errorf("claude disableAllHooks prevents tool-result boundary activation")
		}
	}
	return nil
}
