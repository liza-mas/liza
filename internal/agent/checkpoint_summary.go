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

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const checkpointSummaryFileName = "checkpoint-summary.md"

// checkpointSummaryRelPath is the location, relative to the project root, where
// the auto-generated checkpoint summary is written. Keep it under the branded
// project runtime directory so it does not overwrite user-owned documentation.
func checkpointSummaryRelPath() string {
	return filepath.ToSlash(filepath.Join(paths.ProjectDirName(), checkpointSummaryFileName))
}

func checkpointSummaryStateRelPath() string {
	return filepath.ToSlash(filepath.Join(paths.ProjectDirName(), paths.StateFileName))
}

// checkpointSummaryDefaultTimeout bounds how long the spawned CLI is allowed
// to run before being terminated. Checkpoint summaries are short reads + a
// single Markdown emission — generous but bounded.
const checkpointSummaryDefaultTimeout = 5 * time.Minute

// checkpointSummaryRunner is the function used to actually invoke a CLI for
// the checkpoint-summary skill. It is a package var so tests can substitute
// a deterministic fake without spawning a real LLM subprocess.
//
// Contract:
//   - projectRoot: working directory for the spawn (state.yaml lives in the
//     branded project runtime directory)
//   - cliName: resolved default CLI (claude, codex, gemini, ...)
//   - prompt: the message handed to the CLI; instructs it to use the
//     checkpoint-summary skill against state.yaml and write the report under
//     the branded project runtime directory.
//   - cfg: runtime config used to preserve per-CLI launch settings.
//
// Returns nil on a successful spawn that wrote the report. Any error is
// non-fatal at the call site — the merge itself has already succeeded.
var checkpointSummaryRunner = runCheckpointSummaryCLI

// maybeEmitCheckpointSummary writes the steering report for a checkpoint whose
// obligation is still outstanding, then clears it.
//
// The obligation is durable state recorded by the checkpoint producers, not a
// reading of the current sprint. Three earlier designs were reachable only
// some of the time: emitting after an orchestrator turn missed every
// checkpoint that arrived while it was idle, since the pause gate blocks
// before execution; keying on sprint status lost the report whenever another
// role's auto-resume moved the sprint on first; and keying on
// Sprint.Timeline.CheckpointAt lost it again when a rollover replaced the
// sprint, timeline included. A durable obligation outlives all three, plus a
// supervisor restart.
//
// Claim-then-emit: the obligation is cleared before the CLI runs, so a failing
// or absent CLI cannot make every later poll retry it. That is the
// best-effort contract — one attempt per checkpoint.
//
// Only the orchestrator emits; otherwise every supervisor role would spawn its
// own CLI for the same checkpoint.
func maybeEmitCheckpointSummary(bb *db.Blackboard, projectRoot, roleType string, state *models.State) {
	if roleType != "orchestrator" || state == nil || state.PendingCheckpointSummary == nil || bb == nil {
		return
	}

	var claimed *models.PendingCheckpointSummary
	if err := bb.Modify(func(s *models.State) error {
		// Re-read under the lock: another observation may have claimed it.
		if s.PendingCheckpointSummary == nil {
			return nil
		}
		claimed = s.PendingCheckpointSummary
		s.PendingCheckpointSummary = nil
		return nil
	}); err != nil {
		GetLogger().Warn("Failed to claim the checkpoint-summary obligation", "error", err)
		return
	}
	if claimed == nil {
		return
	}

	emitCheckpointSummary(projectRoot, claimed.Trigger, state.Config)
}

// emitCheckpointSummary runs the checkpoint-summary skill against the project
// after the sprint reached a checkpoint. It is best-effort: any failure is
// logged and discarded so a transient CLI hiccup cannot affect the checkpoint
// that already happened.
//
// The caller is responsible for invoking this exactly once per checkpoint and
// for doing so outside the state lock: the spawned CLI reads state.yaml itself
// and may run for minutes.
//
// Behavior:
//   - opt-out via Config.AutoCheckpointSummary == false
//   - report is written under the branded project runtime directory
//   - CLI is resolved through ResolveDefaultCLI (state.yaml > env > const)
func emitCheckpointSummary(projectRoot string, trigger string, cfg models.Config) {
	logger := GetLogger()

	if cfg.AutoCheckpointSummary != nil && !*cfg.AutoCheckpointSummary {
		logger.Info("Auto checkpoint-summary disabled by config", "trigger", trigger)
		return
	}

	cliName := ResolveDefaultCLI(cfg.DefaultCLI)
	prompt := buildCheckpointSummaryPrompt(trigger)

	if err := checkpointSummaryRunner(projectRoot, cliName, prompt, cfg); err != nil {
		logger.Warn("Auto checkpoint-summary failed",
			"trigger", trigger,
			"cli", cliName,
			"error", err)
		// One attempt per checkpoint: without an alert the human waiting at the
		// checkpoint never learns the report they were meant to read is missing.
		if alertErr := alerts.Write(paths.New(projectRoot).AlertsLogPath(), alerts.Alert{
			Timestamp: time.Now().UTC(),
			Level:     alerts.AlertLevelWarning,
			Category:  "CHECKPOINT SUMMARY FAILED",
			Message:   fmt.Sprintf("%s not written (cli %s): %v; run the checkpoint-summary skill manually", checkpointSummaryRelPath(), cliName, err),
		}); alertErr != nil {
			logger.Warn("Failed to alert on checkpoint-summary failure", "error", alertErr)
		}
		return
	}

	logger.Info("Auto checkpoint-summary emitted",
		"trigger", trigger,
		"cli", cliName,
		"path", checkpointSummaryRelPath())
}

// buildCheckpointSummaryPrompt builds the prompt sent to the configured CLI.
// It is self-contained: the CLI must read state.yaml, apply the
// checkpoint-summary skill, and write the result. Kept short on purpose —
// the skill instructions live in skills/checkpoint-summary/SKILL.md.
//
// The trigger is the checkpoint's own trigger and may be empty for a
// checkpoint taken without one; the sentence stays readable either way.
func buildCheckpointSummaryPrompt(trigger string) string {
	occasion := "the sprint just reached a checkpoint"
	if trigger != "" {
		occasion = fmt.Sprintf("the sprint just reached a checkpoint (trigger: %s)", trigger)
	}
	return fmt.Sprintf(`Use the checkpoint-summary skill.

Context: %s. Read %s,
apply the checkpoint-summary skill protocol, and write the report to
%s (overwrite if it already exists). Do not create, edit, or delete any other
file. Do not ask follow-up questions.
`, occasion, checkpointSummaryStateRelPath(), checkpointSummaryRelPath())
}

// runCheckpointSummaryCLI is the production implementation of
// checkpointSummaryRunner. It spawns the configured CLI with the prompt on
// stdin, captures combined output for the logger, and lets the CLI write
// the markdown report itself (the skill knows where to put it).
//
// The runner resolves argv from the provider catalog like normal agent runs,
// but keeps output discarded because this is a best-effort side effect rather
// than a supervised agent session.
func runCheckpointSummaryCLI(projectRoot, cliName, prompt string, cfg models.Config) error {
	beforeStatus, err := gitStatusSnapshot(projectRoot)
	if err != nil {
		return fmt.Errorf("failed to snapshot git status before checkpoint-summary: %w", err)
	}

	reportRelPath := checkpointSummaryRelPath()
	reportPath := filepath.Join(projectRoot, filepath.FromSlash(reportRelPath))
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
	plan, err := checkpointSummaryLaunchPlan(projectRoot, cliName, prompt, cfg, env)
	if err != nil {
		return nil, false, err
	}
	if plan.RequiresCodexWrapper {
		codexConfig := resolveCodexLaunchConfig(cfg, env)
		cmd, err := codexCommandContext(ctx, codexConfig.PackageVersion, plan.Args)
		if err != nil {
			return nil, false, err
		}
		return cmd, plan.UsesStdin, nil
	}
	return exec.CommandContext(ctx, plan.Executable, plan.Args...), plan.UsesStdin, nil
}

// checkpointSummaryLaunchPlan resolves the one-shot argv from the provider
// catalog, the same source normal agent runs use, so every configured CLI can
// emit a summary. An ACP tool runs through its CLI counterpart: a summary is a
// single prompt and needs no ACP session.
func checkpointSummaryLaunchPlan(projectRoot, cliName, prompt string, cfg models.Config, env []string) (LaunchPlan, error) {
	if cliName == "vibe" {
		cliName = "mistral"
	}
	registry := AgentToolRegistry(cfg)
	if tool, ok := registry[cliName]; ok && tool.Backend == ToolBackendACPX {
		counterpart := acpxAgentNameFromTool(cliName)
		if cliTool, ok := registry[counterpart]; !ok || cliTool.Backend == ToolBackendACPX {
			return LaunchPlan{}, fmt.Errorf("checkpoint-summary: ACP tool %q has no CLI counterpart", cliName)
		}
		cliName = counterpart
	}
	plan, err := ResolveLaunchPlan(LaunchPlanRequest{
		ToolName:         cliName,
		Prompt:           prompt,
		ProjectRoot:      projectRoot,
		RuntimeConfig:    cfg,
		DisableSubagents: brandedEnvListGateValue(env, "DISABLE_CLAUDE_SUBAGENTS") == "1",
	})
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("checkpoint-summary: %w", err)
	}
	if plan.UsesPromptFile {
		return LaunchPlan{}, fmt.Errorf("checkpoint-summary: prompt-file transport of %q is not supported", cliName)
	}
	return plan, nil
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
		if path == checkpointSummaryRelPath() {
			continue
		}
		beforeEntry, existed := before[path]
		if existed && beforeEntry == afterEntry {
			continue
		}
		unexpected = append(unexpected, formatCheckpointStatusEntry(afterEntry, path))
	}

	for path, beforeEntry := range before {
		if path == checkpointSummaryRelPath() {
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

// drainPendingCheckpointSummary honours an outstanding checkpoint-summary
// obligation as the orchestrator supervisor exits.
//
// Registered as a defer by RunSupervisor for the orchestrator role, so it
// covers every exit — goal complete, STOPPED, wait timeout, ordinary return —
// in one place rather than adding another in-loop poll.
//
// The in-loop observation points only run while the supervisor is running.
// A terminal checkpoint that another role auto-resumes through COMPLETED into
// a new sprint, stopping the goal, retires every one of them before this
// process looks — and no later orchestrator will run to pick the obligation
// up, so the run's last and most useful steering report would never be
// written.
//
// Skipped when the context is already cancelled: a signalled shutdown should
// not wait minutes for a report.
func drainPendingCheckpointSummary(ctx context.Context, bb *db.Blackboard, projectRoot string) {
	if ctx.Err() != nil || bb == nil {
		return
	}
	state, err := bb.Read()
	if err != nil {
		return
	}
	maybeEmitCheckpointSummary(bb, projectRoot, "orchestrator", state)
}
