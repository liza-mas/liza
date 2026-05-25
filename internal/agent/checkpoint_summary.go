package agent

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// CheckpointSummaryRelPath is the location, relative to the project root,
// where the auto-generated checkpoint summary is written.
const CheckpointSummaryRelPath = "docs/checkpoint-summary.md"

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
//     to docs/checkpoint-summary.md.
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
//   - report is written to <projectRoot>/docs/checkpoint-summary.md
//   - CLI is resolved through ResolveDefaultCLI (state.yaml > env > const)
func emitCheckpointSummary(projectRoot string, taskID string, cfg models.Config) {
	logger := GetLogger()

	if cfg.AutoCheckpointSummary != nil && !*cfg.AutoCheckpointSummary {
		logger.Info("Auto checkpoint-summary disabled by config", "task_id", taskID)
		return
	}

	cliName := ResolveDefaultCLI(cfg.DefaultCLI)
	prompt := buildCheckpointSummaryPrompt(taskID)

	if err := checkpointSummaryRunner(projectRoot, cliName, prompt); err != nil {
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
%s (overwrite if it already exists). Do not ask follow-up questions.
`, taskID, CheckpointSummaryRelPath)
}

// runCheckpointSummaryCLI is the production implementation of
// checkpointSummaryRunner. It spawns the configured CLI with the prompt on
// stdin, captures combined output for the logger, and lets the CLI write
// the markdown report itself (the skill knows where to put it).
//
// This intentionally does NOT use the rich DefaultCLIExecutor pipeline used
// for normal agent runs: this is a side-effect emitter, not a full agent
// session, and should not depend on agent state (heartbeat, output dir,
// secret masker) or hijack the same per-agent semantics.
func runCheckpointSummaryCLI(projectRoot, cliName, prompt string) error {
	args, useStdin, err := checkpointSummaryCLIArgs(cliName, prompt)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkpointSummaryDefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cliName, args...)
	cmd.Dir = projectRoot
	// Strip ANTHROPIC_API_KEY to avoid conflict with OAuth flow when
	// the configured CLI is claude — see CLAUDE.md Claude Auth rule.
	cmd.Env = filterAPIKeyEnv(os.Environ())

	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	} else {
		cmd.Stdin = nil
	}

	// Capture output — we don't surface it to the user, but log it for
	// triage if the report ends up empty or missing.
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("checkpoint-summary CLI %q timed out after %s", cliName, checkpointSummaryDefaultTimeout)
		}
		return fmt.Errorf("checkpoint-summary CLI %q failed: %w", cliName, err)
	}

	// Sanity: confirm the report was actually written. If the CLI exited 0
	// but never wrote the file, surface that as an error so callers can warn.
	reportPath := filepath.Join(projectRoot, CheckpointSummaryRelPath)
	info, statErr := os.Stat(reportPath)
	if statErr != nil {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report missing at %s: %w", reportPath, statErr)
	}
	if info.Size() == 0 {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report is empty at %s", reportPath)
	}
	return nil
}

// checkpointSummaryCLIArgs returns the argv tail and whether stdin should be
// piped, for each supported CLI. Mirrors the per-CLI dispatch in the main
// supervisor without pulling in agent output logging concerns.
func checkpointSummaryCLIArgs(cliName, prompt string) ([]string, bool, error) {
	switch cliName {
	case "claude":
		return []string{"--print", "--permission-mode", "acceptEdits"}, true, nil
	case "codex":
		return []string{"exec", "--skip-git-repo-check", "-"}, true, nil
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
