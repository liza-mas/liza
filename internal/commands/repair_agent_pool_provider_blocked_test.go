package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Operator defect D58: pool auto-repair started agents on a provider the
// engine knew to be quota-exhausted. A role whose configured provider is
// blocked is not started; it is reported once per episode as having no
// usable provider, with the remedy.

const noUsableProviderCategory = "NO USABLE PROVIDER"

// blockProviderQuota raises a quota signal for provider with an announced
// reset two days out and returns the block's expiry as the alerts print it.
func blockProviderQuota(t *testing.T, projectRoot, provider string) string {
	t.Helper()
	resetsAt := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	if err := agent.RaiseQuotaExhaustion(projectRoot, &agent.QuotaExhaustion{Provider: provider, Message: "You've hit your usage limit", ResetsAt: resetsAt}); err != nil {
		t.Fatal(err)
	}
	return resetsAt.Add(time.Minute).Format(time.RFC3339)
}

func quotaSignalFile(provider string) string {
	return filepath.Join(paths.ProjectDirName(), "provider-quota-exhausted-"+provider)
}

func noUsableProviderAlerts(alerts []Alert) []Alert {
	var out []Alert
	for _, alert := range alerts {
		if alert.Category == noUsableProviderCategory {
			out = append(out, alert)
		}
	}
	return out
}

// tickWithinBackoff runs one watcher pass without ageing the per-role
// backoff, so roles attempted on the previous pass are not due.
func (h *poolRepairHarness) tickWithinBackoff(state *models.State) AutoRepairAgentPoolOutcome {
	h.t.Helper()
	written := *state
	written.Agents = withLiveOrchestrator(state.Agents, time.Now().UTC())
	rewriteOrchestratorRepairState(h.t, h.root, &written)
	return RunAutoRepairAgentPool(context.Background(), &written, WatchConfig{
		ProjectRoot: h.root,
		StateCache:  h.cache,
		WarnWriter:  io.Discard,
	})
}

// coderMissingRoleAlert is the role-presence alert auto-repair supersedes for
// a role it handles.
func coderMissingRoleAlert() Alert {
	return Alert{Category: "MISSING ROLE", Level: AlertLevelWarning, Message: "No live agent for role coder with claimable work (1 task(s): task-a)"}
}

func coderBlockedDemand() *models.State {
	state := coderDemand(1)
	state.Config.DefaultDoerCLI = "codex"
	state.Config.DefaultReviewerCLI = "claude"
	return state
}

// GIVEN coder demand whose only configured provider is quota-blocked
// WHEN the watcher ticks, inside and past the role's backoff
// THEN nothing is started, one NO USABLE PROVIDER alert names the CLI, the
// expiry and both remedies, and MISSING ROLE for the role is suppressed on
// every tick.
func TestRunAutoRepairAgentPool_QuotaBlockedRoleIsReportedNotStarted(t *testing.T) {
	h := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	until := blockProviderQuota(t, h.root, "codex")

	var alerts []Alert
	for i, tick := range []func(*models.State) AutoRepairAgentPoolOutcome{h.tick, h.tickWithinBackoff, h.tick, h.tickWithinBackoff} {
		outcome := tick(state)
		alerts = append(alerts, outcome.Alerts...)
		if kept := FilterAlertsAfterAutoRepair([]Alert{coderMissingRoleAlert()}, outcome); len(kept) != 0 {
			t.Errorf("tick %d: MISSING ROLE for the blocked coder role was not suppressed: %+v", i+1, kept)
		}
	}

	if len(h.spawned) != 0 {
		t.Fatalf("spawned %d agent(s) on the quota-blocked provider, want none", len(h.spawned))
	}
	blocked := noUsableProviderAlerts(alerts)
	if len(blocked) != 1 {
		t.Fatalf("NO USABLE PROVIDER alerts = %+v, want exactly one across four ticks; all alerts: %+v", blocked, alerts)
	}
	if blocked[0].Level != AlertLevelCritical {
		t.Errorf("alert level = %q, want critical", blocked[0].Level)
	}
	for _, want := range []string{"coder", "task-a", "codex", until, paths.ModelsFileName, "delete " + quotaSignalFile("codex")} {
		if !strings.Contains(blocked[0].Message, want) {
			t.Errorf("alert message missing %q: %s", want, blocked[0].Message)
		}
	}
	for _, alert := range alerts {
		if alert.Category == "AUTO REPAIR FAILED" {
			t.Errorf("a quota-blocked role is not a repair failure: %+v", alert)
		}
	}
}

// GIVEN a blocked coder role and a healthy reviewer role with their own
// backoffs
// WHEN ticks alternate between the reviewer being due and no role being due
// THEN the coder's episode is announced once, not re-announced when only
// another role was inspected for starts.
func TestRunAutoRepairAgentPool_BlockedEpisodeSurvivesOtherRolesTicks(t *testing.T) {
	h := newPoolRepairHarness(t)
	now := time.Now().UTC()
	state := coderBlockedDemand()
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("review-a", models.TaskStatusReadyForReview, now))
	blockProviderQuota(t, h.root, "codex")

	var alerts []Alert
	steps := []struct {
		tick          func(*models.State) AutoRepairAgentPoolOutcome
		reviewerIsDue bool
	}{
		{h.tick, true},
		{h.tickWithinBackoff, false},
		{h.tick, true},
		{h.tickWithinBackoff, false},
	}
	for i, step := range steps {
		outcome := step.tick(state)
		alerts = append(alerts, outcome.Alerts...)
		if got := slices.Contains(outcome.AttemptedRoles, "code-reviewer"); got != step.reviewerIsDue {
			t.Fatalf("tick %d: reviewer attempted = %v, want %v (attempted %v)", i+1, got, step.reviewerIsDue, outcome.AttemptedRoles)
		}
		if slices.Contains(outcome.AttemptedRoles, "coder") {
			t.Fatalf("tick %d: the blocked coder role was attempted", i+1)
		}
		if kept := FilterAlertsAfterAutoRepair([]Alert{coderMissingRoleAlert()}, outcome); len(kept) != 0 {
			t.Errorf("tick %d: MISSING ROLE for the blocked coder role was not suppressed: %+v", i+1, kept)
		}
		// The started reviewer exits without registering, so the review
		// demand stays and the role is due again once its backoff passes.
		for _, pid := range h.spawned {
			h.status[pid] = procscan.AgentProcessDead
		}
	}
	if blocked := noUsableProviderAlerts(alerts); len(blocked) != 1 {
		t.Fatalf("NO USABLE PROVIDER alerts = %+v, want one for the coder episode", blocked)
	}
}

// GIVEN coder demand on a quota-blocked CLI
// WHEN the repair command runs (not a dry run)
// THEN it starts nothing, reports the blocked work with its expiry and tasks,
// and does not fail.
func TestRepairAgentPool_QuotaBlockedDoerIsReportedNotFailed(t *testing.T) {
	root := writeRepairAgentPoolState(t, coderBlockedDemand())
	until := blockProviderQuota(t, root, "codex")
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true})

	if err != nil || len(calls) != 0 || len(result.Failed) != 0 || len(result.Missing) != 0 {
		t.Fatalf("err = %v, calls = %+v, failed = %+v, missing = %+v; want nothing started and no failure", err, calls, result.Failed, result.Missing)
	}
	if len(result.ProviderBlocked) != 1 {
		t.Fatalf("provider blocked = %+v, want the coder work", result.ProviderBlocked)
	}
	work := result.ProviderBlocked[0]
	if work.Role != "coder" || work.CLI != "codex" || work.Item != 0 || work.Until.UTC().Format(time.RFC3339) != until || !slices.Equal(work.TaskIDs, []string{"task-a"}) {
		t.Fatalf("provider blocked = %+v, want coder on codex until %s for [task-a]", work, until)
	}
}

// GIVEN a missing orchestrator past its grace whose CLI is quota-blocked
// WHEN the watcher ticks twice
// THEN no orchestrator is started and one NO USABLE PROVIDER alert names it.
func TestRunAutoRepairAgentPool_BlockedOrchestratorIsReportedNotStarted(t *testing.T) {
	unsetAutoRepairAgentPoolEnv(t)
	isolateOrchestratorProcfs(t)
	projectRoot, state := setupOrchestratorRepairProject(t, nil)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatal(err)
	}
	cli, _, err := resolveRepairCLI(RepairCLI{}, "orchestrator", state, pr)
	if err != nil {
		t.Fatal(err)
	}
	until := blockProviderQuota(t, projectRoot, cli)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	cache := agedOrchestratorAbsence(2 * time.Minute)
	first := runOrchestratorAutoRepair(projectRoot, state, cache)
	second := runOrchestratorAutoRepair(projectRoot, state, cache)

	if len(calls) != 0 || slices.Contains(first.AttemptedRoles, "orchestrator") {
		t.Fatalf("calls = %+v, attempted = %v; want no orchestrator start", calls, first.AttemptedRoles)
	}
	blocked := noUsableProviderAlerts(slices.Concat(first.Alerts, second.Alerts))
	if len(blocked) != 1 {
		t.Fatalf("NO USABLE PROVIDER alerts = %+v, want one", blocked)
	}
	for _, want := range []string{"orchestrator: no live orchestrator while goal is IN_PROGRESS", cli, until} {
		if !strings.Contains(blocked[0].Message, want) {
			t.Errorf("alert message missing %q: %s", want, blocked[0].Message)
		}
	}
}

// GIVEN a blocked coder role
// WHEN its demand disappears for a tick and returns, and later the provider
// re-raises its signal with a later reset
// THEN each is a new episode and alerts again.
func TestRunAutoRepairAgentPool_BlockedEpisodeEndsAndRestarts(t *testing.T) {
	h := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	idle := coderBlockedDemand()
	idle.Tasks = nil
	blockProviderQuota(t, h.root, "codex")

	count := func(outcome AutoRepairAgentPoolOutcome) int { return len(noUsableProviderAlerts(outcome.Alerts)) }
	if got := count(h.tick(state)); got != 1 {
		t.Fatalf("first episode: %d alert(s), want 1", got)
	}
	if got := count(h.tick(idle)); got != 0 {
		t.Fatalf("no demand: %d alert(s), want 0", got)
	}
	if got := count(h.tick(state)); got != 1 {
		t.Fatalf("demand back: %d alert(s), want 1 for the new episode", got)
	}
	if got := count(h.tick(state)); got != 0 {
		t.Fatalf("unchanged episode: %d alert(s), want 0", got)
	}

	resetsAt := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	if err := agent.RaiseQuotaExhaustion(h.root, &agent.QuotaExhaustion{Provider: "codex", Message: "You've hit your usage limit", ResetsAt: resetsAt}); err != nil {
		t.Fatal(err)
	}
	outcome := h.tick(state)
	blocked := noUsableProviderAlerts(outcome.Alerts)
	if len(blocked) != 1 || !strings.Contains(blocked[0].Message, resetsAt.Add(time.Minute).Format(time.RFC3339)) {
		t.Fatalf("re-raised signal: alerts = %+v, want one naming the new expiry", blocked)
	}
	if len(h.spawned) != 0 {
		t.Fatalf("spawned %d agent(s) on the quota-blocked provider, want none", len(h.spawned))
	}
}

// GIVEN a quota signal that cannot be read, which blocks with a fail-closed
// expiry re-derived on every read
// WHEN the watcher ticks repeatedly
// THEN the episode is announced once, with no invented expiry.
func TestRunAutoRepairAgentPool_UnreadableSignalIsOneEpisode(t *testing.T) {
	h := newPoolRepairHarness(t)
	state := coderBlockedDemand()
	if err := os.MkdirAll(filepath.Join(agent.QuotaSignalPath(h.root, "codex"), "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}

	var alerts []Alert
	for range 3 {
		alerts = append(alerts, h.tick(state).Alerts...)
	}

	if len(h.spawned) != 0 {
		t.Fatalf("spawned %d agent(s) on the blocked provider, want none", len(h.spawned))
	}
	blocked := noUsableProviderAlerts(alerts)
	if len(blocked) != 1 || !strings.Contains(blocked[0].Message, "until an unknown time") {
		t.Fatalf("NO USABLE PROVIDER alerts = %+v, want one with an unknown expiry", blocked)
	}
}

// GIVEN one free reviewer slot, task A bound to list item 1 on a blocked CLI
// and task B bound to item 2 on a healthy CLI
// WHEN pool repair plans starts
// THEN the free slot goes to item 2 for B, and only A is reported against
// the blocked item.
func TestRepairAgentPool_BlockedItemDoesNotTakeTheFreeSlot(t *testing.T) {
	now := time.Now().UTC()
	root := writeSlotRepairFixture(t, 1,
		testhelpers.BuildTaskByStatus("review-fresh", models.TaskStatusReadyForReview, now),
		secondReviewTask("review-second", now))
	blockProviderQuota(t, root, "claude")
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, Missing: true, DryRun: true})
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(result.Commands) != 1 || !strings.Contains(result.Commands[0], "--models-item 2") {
		t.Fatalf("commands = %v, want only the item-2 start for review-second", result.Commands)
	}
	if len(result.Missing) != 1 || result.Missing[0].SpawnCount != 1 || !slices.Equal(result.Missing[0].Items, []int{2}) {
		t.Fatalf("missing = %+v, want one item-2 start", result.Missing)
	}
	if len(result.ProviderBlocked) != 1 || result.ProviderBlocked[0].Item != 1 || result.ProviderBlocked[0].CLI != "claude" ||
		!slices.Equal(result.ProviderBlocked[0].TaskIDs, []string{"review-fresh"}) {
		t.Fatalf("provider blocked = %+v, want item 1 (claude) holding only review-fresh", result.ProviderBlocked)
	}

	stdout := captureStdout(t, func() { printRepairAgentPoolResult(result) })
	var blockedLine string
	for line := range strings.SplitSeq(stdout, "\n") {
		if strings.Contains(line, "item 1 (claude)") {
			blockedLine = line
		}
	}
	if !strings.Contains(stdout, "no usable provider") || !strings.Contains(blockedLine, "review-fresh") || strings.Contains(blockedLine, "review-second") {
		t.Fatalf("output must report only review-fresh against blocked item 1:\n%s", stdout)
	}
}
