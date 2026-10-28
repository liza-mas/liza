package present

// This file extends the adapter set additively (gap G-b): its production entry
// is commands.RunAutoRepairAgentPool, which the delivered watch adapter does not
// reach. The exported spawn seam records each start request and starts no
// process.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// The watcher's cache keys are part of what the golden freezes: the cases seed
// and age entries by their literal spelling, never through a production name.
const (
	autoRepairBackoffPrefix     = "auto-repair-agent-pool:"
	autoRepairStartCountPrefix  = "auto-repair-agent-pool-start-count:"
	autoRepairSuppressedPrefix  = "auto-repair-agent-pool-suppressed:"
	autoRepairOrchestratorSince = "orchestrator-missing:since"
	// autoRepairFirstPID lies above Linux pid_max: no process holds it, so a
	// start observed on a later tick has exited.
	autoRepairFirstPID = 5100000
)

// autoRepairRun is one case's watcher: a project, a process-local cache and the
// launcher script, shared by its ticks.
type autoRepairRun struct {
	t        *testing.T
	project  string
	cache    map[string]time.Time
	record   strings.Builder
	requests []spawnRequest
	pids     int
	tick     int
}

type autoRepairTick struct {
	label string
	// before prepares the project or the cache for this tick.
	before func(*autoRepairRun)
	// cancelled runs the tick under an already cancelled context.
	cancelled bool
}

type autoRepairCase struct {
	name, project string
	// env is the value of the auto-repair variable; unset when empty.
	env string
	// fail returns the error a start of role (list item 0 for none) fails with.
	fail  func(role string, item int) error
	ticks []autoRepairTick
}

// elapse moves every cache entry with prefix back by twice span.
func (r *autoRepairRun) elapse(prefix string, span time.Duration) {
	r.t.Helper()
	moved := 0
	for key := range r.cache {
		if strings.HasPrefix(key, prefix) {
			r.cache[key] = time.Now().UTC().Add(-2 * span)
			moved++
		}
	}
	if moved == 0 {
		r.t.Fatalf("no cache entry with prefix %s to move", prefix)
	}
}

func (r *autoRepairRun) liza(name string) string { return filepath.Join(r.project, ".liza", name) }

func (r *autoRepairRun) writeModels(content string) {
	r.t.Helper()
	if err := os.WriteFile(r.liza("models.yaml"), []byte(content), 0o644); err != nil {
		r.t.Fatal("write fixture models file")
	}
}

// preferProviderDiversity makes the coding pair's reviews prefer a provider other
// than the doer's, which a new reviewer sharing the doer's provider may not
// claim while a different-provider reviewer is registered.
func (r *autoRepairRun) preferProviderDiversity() {
	r.t.Helper()
	path := r.liza("pipeline.yaml")
	pipeline, err := os.ReadFile(path)
	if err != nil || bytes.Count(pipeline, poolPipelineQuorum) != 1 {
		r.t.Fatal("fixture pipeline must hold exactly one coding-pair quorum of 2")
	}
	diverse := append(append([]byte(nil), poolPipelineQuorum...), []byte("        provider-diversity: preferred\n")...)
	if err := os.WriteFile(path, bytes.Replace(pipeline, poolPipelineQuorum, diverse, 1), 0o644); err != nil {
		r.t.Fatal("write fixture pipeline")
	}
}

// livePID registers the fixture agent holding placeholder under this process's
// PID, which makes it a live agent with reviewer capacity.
func (r *autoRepairRun) livePID(placeholder string) {
	r.t.Helper()
	path := r.liza("state.yaml")
	state, err := os.ReadFile(path)
	if err != nil || bytes.Count(state, []byte(placeholder)) != 1 {
		r.t.Fatal("fixture state must carry the placeholder PID once")
	}
	if err := os.WriteFile(path, bytes.Replace(state, []byte(placeholder), []byte(strconv.Itoa(os.Getpid())), 1), 0o644); err != nil {
		r.t.Fatal("write fixture state")
	}
}

func autoRepairCases() []autoRepairCase {
	orchestratorOverdue := func(r *autoRepairRun) {
		r.cache[autoRepairOrchestratorSince] = time.Now().UTC().Add(-10 * time.Minute)
	}
	elapseBackoff := func(r *autoRepairRun) { r.elapse(autoRepairBackoffPrefix, commands.AutoRepairAgentPoolBackoff) }
	// pending names a started process the way the watcher records it:
	// role, PID, explicit agent ID and models.yaml item.
	pending := func(role string, pid int, agentID string, item int) string {
		return fmt.Sprintf("auto-repair-agent-pool-pending:%s:%d:%s:%d", role, pid, agentID, item)
	}
	refuse := func(roles ...string) func(string, int) error {
		return func(role string, _ int) error {
			if slices.Contains(roles, role) {
				return errors.New("oracle launcher refused " + role)
			}
			return nil
		}
	}
	return []autoRepairCase{
		{name: "spawn-success", project: "demand", ticks: []autoRepairTick{
			{label: "orchestrator absence inside its grace"},
			{label: "orchestrator absence past its grace", before: func(r *autoRepairRun) {
				orchestratorOverdue(r)
				elapseBackoff(r)
			}},
		}},
		{name: "spawn-partial-failure", project: "demand", fail: refuse("code-reviewer"), ticks: []autoRepairTick{
			{label: "reviewer start refused", before: orchestratorOverdue},
		}},
		{name: "spawn-failure-every-role", project: "demand", fail: refuse("coder", "code-reviewer", "orchestrator"), ticks: []autoRepairTick{
			{label: "every start refused", before: orchestratorOverdue},
		}},
		{name: "backoff", project: "demand", ticks: []autoRepairTick{
			{label: "first attempt"},
			{label: "immediately after: roles inside their backoff"},
			{label: "backoff elapsed", before: elapseBackoff},
		}},
		{name: "suppression", project: "demand", ticks: []autoRepairTick{
			{label: "first start"},
			{label: "second start, first exited", before: elapseBackoff},
			{label: "third start, second exited", before: elapseBackoff},
			{label: "start cap reached: suppressed with an alert", before: elapseBackoff},
			{label: "suppression holds without repeating its alert", before: elapseBackoff},
			{label: "cooldown elapsed: counts cleared, start again", before: func(r *autoRepairRun) {
				r.elapse(autoRepairSuppressedPrefix, commands.AutoRepairAgentPoolSuppressionCooldown)
			}},
		}},
		{name: "pending-spawns", project: "demand", ticks: []autoRepairTick{
			{label: "registered start resets the count; unregistered exited start counts; malformed entry dropped", before: func(r *autoRepairRun) {
				now := time.Now().UTC()
				r.cache[autoRepairStartCountPrefix+"coder"] = time.Date(2001, 1, 1, 0, 0, 0, 2, time.UTC)
				r.cache[pending("coder", 5000001, "", 0)] = now
				r.cache[pending("code-reviewer", autoRepairFirstPID+900, "code-reviewer-7", 2)] = now
				r.cache["auto-repair-agent-pool-pending:orphan"] = now
			}},
			{label: "settled entries are gone", before: elapseBackoff},
		}},
		{name: "review-slots", project: "slots", ticks: []autoRepairTick{
			{label: "reviewer starts bound to models.yaml slots"},
			{label: "same condition: no repeat alerts", before: elapseBackoff},
		}},
		{name: "unservable-reviewer", project: "unservable", ticks: []autoRepairTick{
			{label: "reviewer work no new reviewer may claim: warned, nothing started", before: func(r *autoRepairRun) {
				r.preferProviderDiversity()
				r.livePID("5000005")
			}},
			{label: "same condition: the warning is not repeated"},
		}},
		{name: "provider-blocked", project: "provider-blocked", ticks: []autoRepairTick{
			{label: "start CLI quota-blocked", before: orchestratorOverdue},
			{label: "same episode: no repeat alert"},
		}},
		{name: "models-file-error", project: "demand", ticks: []autoRepairTick{
			{label: "invalid models.yaml", before: func(r *autoRepairRun) { r.writeModels("roles:\n  typo: {cli: codex}\n") }},
			{label: "same error: no repeat alert"},
			{label: "file fixed", before: func(r *autoRepairRun) { r.writeModels("roles:\n  coder: {cli: codex}\n") }},
			{label: "another error", before: func(r *autoRepairRun) {
				elapseBackoff(r)
				r.writeModels("roles:\n  reviewer: {cli: codex}\n")
			}},
		}},
		{name: "disabled", project: "demand", env: "no", ticks: []autoRepairTick{
			{label: "variable disables repair and clears its episode", before: func(r *autoRepairRun) {
				r.cache[autoRepairBackoffPrefix+"coder"] = time.Now().UTC()
				r.cache[autoRepairStartCountPrefix+"coder"] = time.Date(2001, 1, 1, 0, 0, 0, 2, time.UTC)
				r.cache["auto-repair-agent-pool-provider-blocked:coder#0@claude@2099-01-01T00:00:00Z"] = time.Now().UTC()
			}},
		}},
		{name: "unrecognised-env", project: "demand", env: "perhaps", ticks: []autoRepairTick{
			{label: "warns once and stays enabled"},
			{label: "no second warning", before: elapseBackoff},
		}},
		{name: "cancelled", project: "demand", ticks: []autoRepairTick{
			{label: "cancelled context does nothing", cancelled: true},
		}},
	}
}

// TestOracleAutoRepairAgentPool pins the production entry
// commands.RunAutoRepairAgentPool (gap G-b) over committed pool states: per
// tick the golden holds the start requests, the outcome (attempted, suppressed
// and blocked roles, spawned and failed starts), the alerts in emitted order,
// the warnings written, and the watcher cache after the tick (keys, and failed
// start counts). The case tail holds the spawn log the watcher wrote. Cases cover
// spawn success and failure, the orchestrator grace, backoff, suppression and
// its cooldown, pending starts that registered or exited, starts bound to
// models.yaml review slots, reviewer work no new reviewer may claim (doer
// provider diversity: the warning, no start, one warning per episode), a
// quota-blocked provider, an invalid models file, the environment
// toggle and a cancelled context. A start still alive on a later tick needs a
// process matching an agent supervisor, which the proof environment excludes
// (E2); the white-box tests script that observation.
func TestOracleAutoRepairAgentPool(t *testing.T) {
	skipUnlessLinux(t)
	// The seam and the process environment are process-wide.
	for _, tc := range autoRepairCases() {
		t.Run(tc.name, func(t *testing.T) { runAutoRepairCase(t, tc) })
	}
}

func runAutoRepairCase(t *testing.T, tc autoRepairCase) {
	t.Helper()
	e := newEnvironment(t)
	project := materialisePool(t, e, tc.project)
	isolateProcessEnvironment(t, e)
	if tc.env != "" {
		t.Setenv(commands.EnvAutoRepairAgentPool, tc.env)
	}
	run := &autoRepairRun{t: t, project: project, cache: map[string]time.Time{}}

	restore := commands.SetRepairAgentPoolSpawnForTest(func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error) {
		if projectRoot != project {
			t.Errorf("start request names a project other than the fixture")
		}
		run.requests = append(run.requests, spawnRequest{role, cli, agentID, cliFromConfig, modelsItem})
		if tc.fail != nil {
			if err := tc.fail(role, modelsItem); err != nil {
				return 0, err
			}
		}
		if role == "orchestrator" {
			return 0, nil // a launcher reporting no PID
		}
		run.pids++
		return autoRepairFirstPID + run.pids, nil
	})
	defer restore()

	for _, step := range tc.ticks {
		run.runTick(step)
	}
	run.record.WriteString("=== spawn log\n")
	logData, err := os.ReadFile(run.liza("log.yaml"))
	switch {
	case errors.Is(err, os.ErrNotExist):
		run.record.WriteString("none\n")
	case err != nil:
		t.Fatal("read spawn log")
	default:
		run.record.WriteString(autoRepairLog(string(logData)))
	}
	e.norm.Check(t, oracle.Human, "auto-repair-pool/"+tc.name+".txt", []byte(run.record.String()))
}

// autoRepairLog keeps the action and detail of each entry; the entry times and
// ordinary fixture content are not what the watcher decides.
func autoRepairLog(data string) string {
	var out strings.Builder
	for line := range strings.Lines(data) {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "action:") || strings.Contains(trimmed, "detail:") || strings.Contains(trimmed, "agent:") {
			out.WriteString(trimmed + "\n")
		}
	}
	if out.Len() == 0 {
		return "none\n"
	}
	return out.String()
}

func (r *autoRepairRun) runTick(step autoRepairTick) {
	r.t.Helper()
	r.tick++
	if step.before != nil {
		step.before(r)
	}
	state, err := db.For(r.liza("state.yaml")).ReadSnapshot()
	if err != nil {
		r.t.Fatal("load pool fixture state")
	}
	r.requests = nil
	var warnings bytes.Buffer
	ctx := context.Background()
	if step.cancelled {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	outcome := commands.RunAutoRepairAgentPool(ctx, state, commands.WatchConfig{
		ProjectRoot: r.project,
		StateCache:  r.cache,
		WarnWriter:  &warnings,
	})

	w := &r.record
	fmt.Fprintf(w, "=== tick %d: %s\n--- start requests\n", r.tick, step.label)
	for i, request := range r.requests {
		fmt.Fprintf(w, "%d: %s\n", i+1, request)
	}
	if len(r.requests) == 0 {
		w.WriteString("none\n")
	}
	fmt.Fprintf(w, "--- outcome\nattempted: %v\nsuppressed: %v\nblocked: %v\n", outcome.AttemptedRoles, outcome.SuppressedRoles, outcome.BlockedRoles)
	for _, spawned := range outcome.Spawned {
		fmt.Fprintf(w, "spawned: role=%s agent_id=%s cli=%s models_item=%d pid=%d command=%s\n", spawned.Role, spawned.AgentID, spawned.CLI, spawned.Item, spawned.PID, spawned.Command)
	}
	for _, failed := range outcome.Failed {
		fmt.Fprintf(w, "failed: role=%s cli=%s command=%s error=%s\n", failed.Role, failed.CLI, failed.Command, failed.Error)
	}
	w.WriteString("--- alerts\n")
	for _, alert := range outcome.Alerts {
		w.WriteString(alert.String() + "\n")
	}
	if len(outcome.Alerts) == 0 {
		w.WriteString("none\n")
	}
	fmt.Fprintf(w, "--- warnings\n%s", warnings.String())
	if warnings.Len() == 0 {
		w.WriteString("none\n")
	}
	w.WriteString("--- cache\n")
	keys := make([]string, 0, len(r.cache))
	for key := range r.cache {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		// The failed start count is the stamp's nanosecond field; no other value
		// is a decision of the watcher that a later run can compare.
		if strings.HasPrefix(key, autoRepairStartCountPrefix) {
			key += "=" + strconv.Itoa(r.cache[key].Nanosecond())
		}
		w.WriteString(key + "\n")
	}
	if len(keys) == 0 {
		w.WriteString("none\n")
	}
}

// TestAutoRepairUnservableFixtureValidates keeps the state this file adds to the
// pool fixtures a valid one.
func TestAutoRepairUnservableFixtureValidates(t *testing.T) {
	skipUnlessLinux(t)
	e := newEnvironment(t)
	result := runCLI(t, e, materialisePool(t, e, "unservable"), nil, "validate")
	if result.exit != 0 {
		t.Fatalf("unservable fixture validate exited %d:\n%s\n%s", result.exit, e.norm.Text(string(result.stdout)), e.norm.Text(string(result.stderr)))
	}
}
