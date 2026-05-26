package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
)

// CheckpointSummaryRelPath is the location, relative to the project root,
// where the auto-generated checkpoint summary is written. Keep it under
// .liza/ so Liza does not overwrite user-owned project documentation.
const CheckpointSummaryRelPath = ".liza/checkpoint-summary.md"

// checkpointSummaryDefaultTimeout bounds how long the spawned CLI is allowed
// to run before being terminated. Checkpoint summaries are short reads + a
// single Markdown emission — generous but bounded.
const checkpointSummaryDefaultTimeout = 5 * time.Minute

// checkpointSummaryRunner is the function used to actually invoke a CLI for
// the checkpoint-summary skill. It is a package var so tests can substitute
// a deterministic fake without spawning a real LLM subprocess.
//
// Contract:
//   - projectRoot: working directory for the spawn (state.yaml lives in .liza/)
//   - cliName: resolved default CLI (claude, codex, gemini, ...)
//   - prompt: the message handed to the CLI; instructs it to use the
//     checkpoint-summary skill against .liza/state.yaml and write the report
//     to .liza/checkpoint-summary.md.
//   - cfg: runtime config used to preserve per-CLI launch settings.
//
// Returns nil on a successful spawn that wrote the report. Any error is
// non-fatal at the call site — the merge itself has already succeeded.
var checkpointSummaryRunner = runCheckpointSummaryCLI

// emitCheckpointSummary runs the checkpoint-summary skill against the project
// for the given just-merged task. It is best-effort: any failure is logged
// and discarded so a transient CLI hiccup does not poison the merge path.
//
// Behavior:
//   - opt-out via Config.AutoCheckpointSummary == false
//   - report is written to <projectRoot>/.liza/checkpoint-summary.md
//   - CLI is resolved through ResolveDefaultCLI (state.yaml > env > const)
func emitCheckpointSummary(projectRoot string, taskID string, cfg models.Config) {
	logger := GetLogger()

	if cfg.AutoCheckpointSummary != nil && !*cfg.AutoCheckpointSummary {
		logger.Info("Auto checkpoint-summary disabled by config", "task_id", taskID)
		return
	}

	cliName := ResolveDefaultCLI(cfg.DefaultCLI)
	prompt := buildCheckpointSummaryPrompt(taskID)

	if err := checkpointSummaryRunner(projectRoot, cliName, prompt, cfg); err != nil {
		logger.Warn("Auto checkpoint-summary failed",
			"task_id", taskID,
			"cli", cliName,
			"error", err)
		return
	}

	logger.Info("Auto checkpoint-summary emitted",
		"task_id", taskID,
		"cli", cliName,
		"path", CheckpointSummaryRelPath)
}

// buildCheckpointSummaryPrompt builds the prompt sent to the configured CLI.
// It is self-contained: the CLI must read .liza/state.yaml, apply the
// checkpoint-summary skill, and write the result. Kept short on purpose —
// the skill instructions live in skills/checkpoint-summary/SKILL.md.
func buildCheckpointSummaryPrompt(taskID string) string {
	return fmt.Sprintf(`Use the checkpoint-summary skill.

Context: task %s just merged into the integration branch. Read .liza/state.yaml,
apply the checkpoint-summary skill protocol, and write the report to
%s (overwrite if it already exists). Do not create, edit, or delete any other
file. Do not ask follow-up questions.
`, taskID, CheckpointSummaryRelPath)
}

// runCheckpointSummaryCLI is the production implementation of
// checkpointSummaryRunner. It spawns the configured CLI with the prompt on
// stdin, captures combined output for the logger, and lets the CLI write
// the markdown report itself (the skill knows where to put it).
//
// The runner reuses the same per-CLI argv builders as normal agent runs where
// practical, but keeps output discarded because this is a best-effort side
// effect rather than a supervised agent session.
func runCheckpointSummaryCLI(projectRoot, cliName, prompt string, cfg models.Config) error {
	beforeStatus, err := gitStatusSnapshot(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to snapshot git status before checkpoint-summary: %w", err)
	}

	reportPath := filepath.Join(projectRoot, CheckpointSummaryRelPath)
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return fmt.Errorf("failed to prepare checkpoint-summary directory: %w", err)
	}

	env := filterAPIKeyEnv(os.Environ())

	ctx, cancel := context.WithTimeout(context.Background(), checkpointSummaryDefaultTimeout)
	defer cancel()

	cmd, useStdin, err := checkpointSummaryCLICommand(ctx, projectRoot, cliName, prompt, cfg, env)
	if err != nil {
		return err
	}
	cmd.Dir = projectRoot
	cmd.Env = env

	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		cmd.Stdin = nil
	}

	// Discard subprocess output. Auto-summary is best-effort, and persisted
	// agent output handling belongs to the normal supervised agent pipeline.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	runErr := cmd.Run()
	statusErr := validateCheckpointSummaryStatus(projectRoot, beforeStatus)
	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("checkpoint-summary CLI %q timed out after %s", cliName, checkpointSummaryDefaultTimeout)
		}
		if statusErr != nil {
			return fmt.Errorf("checkpoint-summary CLI %q failed: %w; %v", cliName, runErr, statusErr)
		}
		return fmt.Errorf("checkpoint-summary CLI %q failed: %w", cliName, runErr)
	}
	if statusErr != nil {
		return statusErr
	}

	// Sanity: confirm the report was actually written. If the CLI exited 0
	// but never wrote the file, surface that as an error so callers can warn.
	info, statErr := os.Stat(reportPath)
	if statErr != nil {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report missing at %s: %w", reportPath, statErr)
	}
	if info.Size() == 0 {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report is empty at %s", reportPath)
	}
	return nil
}

func checkpointSummaryCLICommand(
	ctx context.Context,
	projectRoot, cliName, prompt string,
	cfg models.Config,
	env []string,
) (*exec.Cmd, bool, error) {
	actualCLI := cliName
	if cliName == "mistral" {
		actualCLI = "vibe"
	}

	args, useStdin, err := checkpointSummaryCLIArgs(actualCLI, projectRoot, prompt, cfg, env)
	if err != nil {
		return nil, false, err
	}

	switch actualCLI {
	case "codex":
		codexConfig := resolveCodexLaunchConfig(cfg, env)
		cmd, err := codexCommandContext(ctx, codexConfig.PackageVersion, args)
		if err != nil {
			return nil, false, err
		}
		return cmd, useStdin, nil
	default:
		return exec.CommandContext(ctx, actualCLI, args...), useStdin, nil
	}
}

// checkpointSummaryCLIArgs returns the argv tail and whether stdin should be
// piped for each supported CLI. It mirrors the normal supervisor's per-CLI
// argv builders so checkpoint summaries honor the same launch semantics.
func checkpointSummaryCLIArgs(cliName, projectRoot, prompt string, cfg models.Config, env []string) ([]string, bool, error) {
	switch cliName {
	case "claude":
		disableSubagents := envValue(env, "LIZA_DISABLE_CLAUDE_SUBAGENTS") == "1"
		return buildClaudeArgs(prompt, true, "", disableSubagents), true, nil
	case "codex":
		codexConfig := resolveCodexLaunchConfig(cfg, env)
		return buildCodexArgs(projectRoot, prompt, true, "", nil, codexConfig.LegacyLandlock), true, nil
	case "gemini":
		return []string{"-p"}, true, nil
	case "vibe", "mistral":
		return []string{"-p", prompt}, false, nil
	case "kimi":
		return []string{"-p"}, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported CLI for checkpoint-summary: %q", cliName)
	}
}

type checkpointStatusEntry struct {
	status      string
	fingerprint string
}

func gitStatusSnapshot(projectRoot string) (map[string]checkpointStatusEntry, error) {
	output, err := gitenv.Output(projectRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}

	entries := parseGitStatusPorcelainZ(output)
	snapshot := make(map[string]checkpointStatusEntry, len(entries))
	for _, entry := range entries {
		snapshot[entry.path] = checkpointStatusEntry{
			status:      entry.status,
			fingerprint: checkpointPathFingerprint(projectRoot, entry.path),
		}
	}
	return snapshot, nil
}

type gitStatusEntry struct {
	status string
	path   string
}

func parseGitStatusPorcelainZ(output []byte) []gitStatusEntry {
	records := strings.Split(string(output), "\x00")
	var entries []gitStatusEntry
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		if len(record) < 4 {
			continue
		}
		status := record[:2]
		entries = append(entries, gitStatusEntry{
			status: status,
			path:   filepath.ToSlash(record[3:]),
		})
		if strings.ContainsAny(status, "RC") && i+1 < len(records) {
			i++ // porcelain -z includes the source path as a separate record.
		}
	}
	return entries
}

func checkpointPathFingerprint(projectRoot, relPath string) string {
	info, err := os.Lstat(filepath.Join(projectRoot, filepath.FromSlash(relPath)))
	if err != nil {
		if os.IsNotExist(err) {
			return "missing"
		}
		return "stat-error:" + err.Error()
	}
	return fmt.Sprintf("mode=%s size=%d mod=%d", info.Mode().String(), info.Size(), info.ModTime().UnixNano())
}

func validateCheckpointSummaryStatus(projectRoot string, before map[string]checkpointStatusEntry) error {
	after, err := gitStatusSnapshot(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to snapshot git status after checkpoint-summary: %w", err)
	}
	unexpected := unexpectedCheckpointSummaryStatusChanges(before, after)
	if len(unexpected) > 0 {
		return fmt.Errorf("checkpoint-summary CLI modified unexpected paths: %s", strings.Join(unexpected, ", "))
	}
	return nil
}

func unexpectedCheckpointSummaryStatusChanges(before, after map[string]checkpointStatusEntry) []string {
	var unexpected []string
	for path, afterEntry := range after {
		if path == filepath.ToSlash(CheckpointSummaryRelPath) {
			continue
		}
		beforeEntry, existed := before[path]
		if existed && beforeEntry == afterEntry {
			continue
		}
		unexpected = append(unexpected, formatCheckpointStatusEntry(afterEntry, path))
	}

	for path, beforeEntry := range before {
		if path == filepath.ToSlash(CheckpointSummaryRelPath) {
			continue
		}
		if _, stillPresent := after[path]; !stillPresent {
			unexpected = append(unexpected, formatCheckpointStatusEntry(beforeEntry, path)+" (removed)")
		}
	}
	sort.Strings(unexpected)
	return unexpected
}

func formatCheckpointStatusEntry(entry checkpointStatusEntry, path string) string {
	if entry.status == "" {
		return path
	}
	return entry.status + " " + path
}

// filterAPIKeyEnv removes ANTHROPIC_API_KEY from the env list. The user's
// CLAUDE.md mandates this for any subprocess that may shell out to claude —
// it conflicts with the OAuth token mechanism and surfaces as "invalid API
// key" errors.
func filterAPIKeyEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}
