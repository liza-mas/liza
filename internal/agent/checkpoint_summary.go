package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
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

// checkpointSummaryOutputTailBytes bounds the CLI output kept for a failure
// message; checkpointSummaryDetailBytes bounds what reaches the alert.
const (
	checkpointSummaryOutputTailBytes = 4096
	checkpointSummaryDetailBytes     = 512
)

// checkpointSummaryScopeLimit bounds the tasks one automatic summary covers.
// A one-shot summary that reads everything the run produced outgrows any
// fixed bound (#171).
const checkpointSummaryScopeLimit = 25

// checkpointSummaryScope is the slice of the run one automatic summary covers:
// the tasks merged after Since and up to the checkpoint, newest first.
type checkpointSummaryScope struct {
	Since   time.Time
	TaskIDs []string
	// Omitted counts the tasks merged in the window beyond the limit.
	Omitted int
}

// checkpointSummaryRunner is the function used to actually invoke a CLI for
// the checkpoint-summary skill. It is a package var so tests can substitute
// a deterministic fake without spawning a real LLM subprocess.
//
// Contract:
//   - ctx: the supervisor's context; cancelling it stops the CLI
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

// checkpointSummaryInFlight tracks the one summary a supervisor runs in the
// background at a time. A checkpoint raised while it runs keeps its
// obligation, which a later poll claims once this one ends.
var checkpointSummaryInFlight struct {
	sync.Mutex
	done chan struct{}
}

// beginCheckpointSummary reserves the background slot; ok is false while
// another summary runs. finish releases the slot.
func beginCheckpointSummary() (finish func(), ok bool) {
	checkpointSummaryInFlight.Lock()
	defer checkpointSummaryInFlight.Unlock()
	if checkpointSummaryInFlight.done != nil {
		return nil, false
	}
	done := make(chan struct{})
	checkpointSummaryInFlight.done = done
	return func() {
		checkpointSummaryInFlight.Lock()
		checkpointSummaryInFlight.done = nil
		checkpointSummaryInFlight.Unlock()
		close(done)
	}, true
}

// waitForCheckpointSummary blocks until the background summary, if any, ends.
func waitForCheckpointSummary() {
	checkpointSummaryInFlight.Lock()
	done := checkpointSummaryInFlight.done
	checkpointSummaryInFlight.Unlock()
	if done != nil {
		<-done
	}
}

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
//
// The CLI runs in the background: a summary takes minutes, and running it in
// the caller held the orchestrator for the whole run at every checkpoint
// (#171). One runs at a time; while it does, a newer obligation is left
// unclaimed for a later poll.
func maybeEmitCheckpointSummary(ctx context.Context, bb *db.Blackboard, projectRoot, roleType string, state *models.State) {
	if roleType != "orchestrator" || state == nil || state.PendingCheckpointSummary == nil || bb == nil {
		return
	}
	finish, ok := beginCheckpointSummary()
	if !ok {
		return
	}

	var claimed *models.PendingCheckpointSummary
	var scope checkpointSummaryScope
	if err := bb.Modify(func(s *models.State) error {
		// Re-read under the lock: another observation may have claimed it.
		if s.PendingCheckpointSummary == nil {
			return nil
		}
		claimed = s.PendingCheckpointSummary
		scope = buildCheckpointSummaryScope(s, claimed)
		s.PendingCheckpointSummary = nil
		return nil
	}); err != nil {
		finish()
		GetLogger().Warn("Failed to claim the checkpoint-summary obligation", "error", err)
		return
	}
	if claimed == nil {
		finish()
		return
	}

	cfg := state.Config
	go func() {
		defer finish()
		emitCheckpointSummary(ctx, projectRoot, claimed.Trigger, scope, cfg)
	}()
}

// buildCheckpointSummaryScope selects the tasks whose latest merge falls after
// the previous checkpoint and no later than this one, newest first. Without a
// previous checkpoint the window starts at the sprint start.
func buildCheckpointSummaryScope(state *models.State, pending *models.PendingCheckpointSummary) checkpointSummaryScope {
	scope := checkpointSummaryScope{Since: state.Sprint.Timeline.Started}
	if pending.Since != nil {
		scope.Since = *pending.Since
	}
	type merge struct {
		id string
		at time.Time
	}
	var merges []merge
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for j := len(task.History) - 1; j >= 0; j-- {
			entry := task.History[j]
			if entry.Event != models.TaskEventMerged {
				continue
			}
			if entry.Time.After(scope.Since) && !entry.Time.After(pending.At) {
				merges = append(merges, merge{id: task.ID, at: entry.Time})
			}
			break
		}
	}
	sort.SliceStable(merges, func(a, b int) bool { return merges[a].at.After(merges[b].at) })
	for i, m := range merges {
		if i == checkpointSummaryScopeLimit {
			scope.Omitted = len(merges) - i
			break
		}
		scope.TaskIDs = append(scope.TaskIDs, m.id)
	}
	return scope
}

// emitCheckpointSummary runs the checkpoint-summary skill against the project
// after the sprint reached a checkpoint. It is best-effort: any failure is
// logged and discarded so a transient CLI hiccup cannot affect the checkpoint
// that already happened.
//
// The caller is responsible for invoking this exactly once per checkpoint and
// for doing so outside the state lock and off the supervisor loop: the spawned
// CLI reads state.yaml itself and may run for minutes.
//
// Behavior:
//   - opt-out via Config.AutoCheckpointSummary == false
//   - report is written under the branded project runtime directory
//   - CLI is the orchestrator's, resolved through ResolveDefaultCLIForRole
//     (doer config > doer env > global config > env > const), so the summary
//     runs on the provider the run already uses (D-57)
func emitCheckpointSummary(ctx context.Context, projectRoot string, trigger string, scope checkpointSummaryScope, cfg models.Config) {
	logger := GetLogger()

	if cfg.AutoCheckpointSummary != nil && !*cfg.AutoCheckpointSummary {
		logger.Info("Auto checkpoint-summary disabled by config", "trigger", trigger)
		return
	}

	cliName := ResolveDefaultCLIForRole("orchestrator", cliResolutionConfig(cfg))
	prompt := buildCheckpointSummaryPrompt(trigger, scope)

	if err := checkpointSummaryRunner(ctx, projectRoot, cliName, prompt, cfg); err != nil {
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
//
// The scope and the one-turn rule bound the run: a summary told to read every
// artifact of the run delegated the reading to subagents and ended its turn
// before writing (#171).
func buildCheckpointSummaryPrompt(trigger string, scope checkpointSummaryScope) string {
	occasion := "the sprint just reached a checkpoint"
	if trigger != "" {
		occasion = fmt.Sprintf("the sprint just reached a checkpoint (trigger: %s)", trigger)
	}
	var tasks strings.Builder
	for _, id := range scope.TaskIDs {
		fmt.Fprintf(&tasks, "- %s\n", id)
	}
	if len(scope.TaskIDs) == 0 {
		tasks.WriteString("(no task merged in this window)\n")
	}
	if scope.Omitted > 0 {
		fmt.Fprintf(&tasks, "(%d earlier tasks merged in this window are not covered; say so in the report.)\n", scope.Omitted)
	}
	return fmt.Sprintf(`Use the checkpoint-summary skill.

Context: %s. Read %s,
apply the checkpoint-summary skill protocol, and write the report to
%s (overwrite if it already exists).

Scope: this report covers only the tasks merged since %s:
%sRead only these tasks' own plan_ref, arch_ref and spec_ref artifacts and the
goal spec sections they cite; take everything else from state fields. If no
task is listed, report only the current blocked tasks and holds from state.

Work alone and finish in this turn: do not delegate to subagents, schedule
wakeups or start background work, and write the report before ending your
turn. Do not create, edit, or delete any other file. Do not ask follow-up
questions.
`, occasion, checkpointSummaryStateRelPath(), checkpointSummaryRelPath(),
		scope.Since.UTC().Format(time.RFC3339), tasks.String())
}

// runCheckpointSummaryCLI is the production implementation of
// checkpointSummaryRunner. It spawns the configured CLI with the prompt on
// stdin, captures combined output for the logger, and lets the CLI write
// the markdown report itself (the skill knows where to put it).
//
// The runner resolves argv from the provider catalog like normal agent runs.
// Output is not persisted, because this is a best-effort side effect rather
// than a supervised agent session, but its tail is kept so a failure names
// its cause instead of only an exit status.
func runCheckpointSummaryCLI(parent context.Context, projectRoot, cliName, prompt string, cfg models.Config) error {
	reportRelPath := checkpointSummaryRelPath()
	reportPath := filepath.Join(projectRoot, filepath.FromSlash(reportRelPath))
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		return fmt.Errorf("failed to prepare checkpoint-summary directory: %w", err)
	}

	env := filterAPIKeyEnv(os.Environ())

	// Derived from the supervisor's context so a stopping supervisor kills the
	// CLI instead of orphaning it without its timeout.
	ctx, cancel := context.WithTimeout(parent, checkpointSummaryDefaultTimeout)
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

	// One writer for both streams: exec serializes writes to it.
	tail := &outputTail{limit: checkpointSummaryOutputTailBytes}
	cmd.Stdout = tail
	cmd.Stderr = tail

	runErr := cmd.Run()
	// Mask against the unfiltered env: filterAPIKeyEnv drops the very key
	// most likely to be echoed.
	detail := checkpointSummaryFailureDetail(tail, os.Environ())
	if runErr != nil {
		if parent.Err() != nil {
			return fmt.Errorf("checkpoint-summary CLI %q cancelled: supervisor stopping%s", cliName, detail)
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("checkpoint-summary CLI %q timed out after %s%s", cliName, checkpointSummaryDefaultTimeout, detail)
		}
		return fmt.Errorf("checkpoint-summary CLI %q failed: %w%s", cliName, runErr, detail)
	}

	// Sanity: confirm the report was actually written. If the CLI exited 0
	// but never wrote the file, surface that as an error so callers can warn;
	// the output tail shows what the session did instead.
	info, statErr := os.Stat(reportPath)
	if statErr != nil {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report missing at %s: %w%s", reportPath, statErr, detail)
	}
	if info.Size() == 0 {
		return fmt.Errorf("checkpoint-summary CLI exited 0 but report is empty at %s%s", reportPath, detail)
	}
	return nil
}

// outputTail keeps the last limit bytes written to it.
type outputTail struct {
	limit     int
	buf       []byte
	truncated bool
}

func (t *outputTail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.limit; over > 0 {
		t.buf = t.buf[over:]
		t.truncated = true
	}
	return len(p), nil
}

// checkpointSummaryFailureDetail renders the output tail as one line for an
// error: secrets masked, whitespace collapsed, at most
// checkpointSummaryDetailBytes kept from the end. When the tail was cut, its
// first word may be a fragment of a secret the masker cannot recognize, so
// that word is dropped.
func checkpointSummaryFailureDetail(tail *outputTail, environ []string) string {
	fields := strings.Fields(newSecretMaskerFromEnv(environ).MaskText(string(tail.buf)))
	if tail.truncated && len(fields) > 0 {
		fields = fields[1:]
	}
	text := strings.Join(fields, " ")
	if text == "" {
		return ""
	}
	if len(text) > checkpointSummaryDetailBytes {
		text = "…" + strings.ToValidUTF8(text[len(text)-checkpointSummaryDetailBytes:], "")
	}
	return ": " + text
}

func checkpointSummaryCLICommand(
	ctx context.Context,
	projectRoot, cliName, prompt string,
	cfg models.Config,
	env []string,
) (*exec.Cmd, bool, error) {
	plan, err := checkpointSummaryLaunchPlan(projectRoot, cliName, prompt, cfg)
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
func checkpointSummaryLaunchPlan(projectRoot, cliName, prompt string, cfg models.Config) (LaunchPlan, error) {
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
		ToolName:      cliName,
		Prompt:        prompt,
		ProjectRoot:   projectRoot,
		RuntimeConfig: cfg,
		// A one-shot summary must finish in its own turn; a subagent let it end
		// the turn before writing the report (#171).
		DisableSubagents: true,
	})
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("checkpoint-summary: %w", err)
	}
	if plan.UsesPromptFile {
		return LaunchPlan{}, fmt.Errorf("checkpoint-summary: prompt-file transport of %q is not supported", cliName)
	}
	return plan, nil
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
// A summary already running in the background is waited for first, so the
// process does not exit under it. A cancelled context skips the drain: a
// signalled shutdown should not wait minutes for a report. The cancellation
// kills a running CLI, which is still waited for so it is not orphaned.
func drainPendingCheckpointSummary(ctx context.Context, bb *db.Blackboard, projectRoot string) {
	if ctx.Err() != nil || bb == nil {
		waitForCheckpointSummary()
		return
	}
	waitForCheckpointSummary()
	state, err := bb.ReadSnapshot()
	if err != nil {
		return
	}
	maybeEmitCheckpointSummary(ctx, bb, projectRoot, "orchestrator", state)
	waitForCheckpointSummary()
}
