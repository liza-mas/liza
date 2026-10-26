package ops

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// Deferred validation failure attribution: carry-forward, known-red skipping
// and the bisect over merges since a command last passed (ADR-0204).

// carryForward returns, by command digest, the carried_forward results of the
// commands whose latest pass still holds at head (D-230): the command's newest
// execution passed, in a batch that certified what it observed, at an
// ancestor of head, and nothing changed since in its declared scope (the whole
// tree when none) outside the inert paths. A forced batch carries nothing;
// any doubt executes the command. The bool reports a halt.
func (r *deferredValidationRunner) carryForward(ctx context.Context, state *models.State, head string, commands []models.DeferredValidationCommand, forced bool) (map[string]models.DeferredValidationResult, bool) {
	carried := map[string]models.DeferredValidationResult{}
	if forced {
		return carried, false
	}
	ledger := deferredValidationLedger(state)
	for _, command := range commands {
		batch, execution := ledger.LatestExecutionFor(command.CommandSHA256)
		if batch == nil || execution.Status != models.DeferredValidationResultPassed || batch.Reason == string(deferredValidationReasonUnstoppable) {
			continue
		}
		pathspecs, ok := deferredCarryPathspecs(state.Config, command.Command)
		if !ok {
			fmt.Fprintf(r.log, "deferred-validation: %q runs: its scope or inert paths hold an empty or ':'-prefixed pathspec\n", r.mask(command.Command))
			continue
		}
		unchanged, res := r.unchangedSince(ctx, batch.Commit, head, pathspecs)
		if res.halted {
			return nil, true
		}
		if res.err != nil {
			fmt.Fprintf(r.log, "deferred-validation: %q runs: cannot compare %s with %s: %s\n", r.mask(command.Command), batch.Commit, head, r.mask(deferredFailureText(res)))
			continue
		}
		if unchanged {
			now := time.Now().UTC()
			carried[command.CommandSHA256] = models.DeferredValidationResult{
				Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, Status: models.DeferredValidationResultCarriedForward,
				StartedAt: now, FinishedAt: now, CarriedFrom: batch.Commit,
			}
		}
	}
	return carried, false
}

// knownRed returns, by command digest, the known_red results of the commands
// whose failure is already attributed and whose fix is in flight (D-181): the
// command's newest execution failed, in a batch that certified what it
// observed and named exactly one culprit task for it, and that (culprit,
// command) discovery was converted to a fix task still in flight. Such a
// command is neither run nor bisected until the fix merges or dies. A forced
// batch skips nothing; any doubt executes the command.
func (r *deferredValidationRunner) knownRed(state *models.State, commands []models.DeferredValidationCommand, forced bool) map[string]models.DeferredValidationResult {
	known := map[string]models.DeferredValidationResult{}
	if forced {
		return known
	}
	ledger := deferredValidationLedger(state)
	resolver := models.NewDependencyResolver(state)
	for _, command := range commands {
		batch, execution := ledger.LatestExecutionFor(command.CommandSHA256)
		if batch == nil || execution.Status != models.DeferredValidationResultFailed || batch.Reason == string(deferredValidationReasonUnstoppable) {
			continue
		}
		culprit := deferredSoleCulprit(batch, command.CommandSHA256)
		if culprit == "" {
			continue
		}
		owner := deferredFixInFlight(state, resolver, culprit, command.CommandSHA256)
		if owner == "" {
			continue
		}
		fmt.Fprintf(r.log, "deferred-validation: %q not run: it failed at %s (culprit %s) and its fix %s is in flight\n", r.mask(command.Command), batch.Commit, culprit, owner)
		now := time.Now().UTC()
		known[command.CommandSHA256] = models.DeferredValidationResult{
			Command: r.mask(command.Command), CommandSHA256: command.CommandSHA256, Status: models.DeferredValidationResultKnownRed,
			ExitCode: -1, StartedAt: now, FinishedAt: now, CarriedFrom: batch.Commit, OwnedBy: owner,
		}
	}
	return known
}

// deferredSoleCulprit is the task batch attributed commandSHA's failure to, or
// "" when the failure is unattributed, unbisected or attributed ambiguously.
func deferredSoleCulprit(batch *models.DeferredValidationBatch, commandSHA string) string {
	task := ""
	for _, culprit := range batch.Culprits {
		if culprit.CommandSHA256 != commandSHA {
			continue
		}
		if culprit.TaskID == "" || task != "" {
			return ""
		}
		task = culprit.TaskID
	}
	return task
}

// deferredFixInFlight returns the live task fixing culprit's failure of
// commandSHA: the (culprit, command) discovery's converted task, or the live
// replacement it was superseded by, while it is unmerged. It returns "" when
// no such discovery was converted to a task, or the fix merged or died.
func deferredFixInFlight(state *models.State, resolver *models.DependencyResolver, culprit, commandSHA string) string {
	for _, discovery := range state.Discovered {
		if discovery.By != models.DeferredValidationDiscoveryBy || discovery.Extra["culprit_task"] != culprit || discovery.Extra["command_sha256"] != commandSHA {
			continue
		}
		if discovery.ConvertedToTask == nil || discoveryDisposition(*discovery.ConvertedToTask).isSentinel() {
			continue
		}
		fix := resolver.Resolve(*discovery.ConvertedToTask)
		if fix.Satisfied() || fix.Invalid() {
			continue
		}
		for _, id := range fix.BlockingIDs {
			if task := state.FindTask(id); task != nil && !task.Status.IsTerminal() {
				return id
			}
		}
	}
	return ""
}

// unchangedSince reports whether from is head or an ancestor of it and no path
// matching pathspecs differs between them. Renames count on both sides.
func (r *deferredValidationRunner) unchangedSince(ctx context.Context, from, head string, pathspecs []string) (bool, execResult) {
	if from == head {
		return true, execResult{}
	}
	if ancestor, res := r.isAncestor(ctx, from, head); !ancestor {
		return false, res
	}
	args := append([]string{"--glob-pathspecs", "diff", "--quiet", "--no-ext-diff", "--no-renames", from, head, "--"}, pathspecs...)
	res := r.git(ctx, r.root, args...)
	if res.err != nil && !res.halted && res.exitCode == 1 {
		return false, execResult{}
	}
	return res.err == nil, res
}

// deferredCarryPathspecs is the Git pathspec list whose changes stop command's
// pass from being carried forward: its declared scope, or the whole tree, minus
// the inert paths. Operator entries are glob patterns; an empty one, a declared
// scope without paths, or one carrying its own pathspec magic is refused.
func deferredCarryPathspecs(config models.Config, command string) ([]string, bool) {
	invalid := func(pattern string) bool {
		return strings.TrimSpace(pattern) == "" || strings.HasPrefix(pattern, ":")
	}
	pathspecs := []string{"."}
	if scope := models.DeferredValidationScopeFor(config, command); scope != nil {
		if len(scope.Paths) == 0 || slices.ContainsFunc(scope.Paths, invalid) {
			return nil, false
		}
		pathspecs = slices.Clone(scope.Paths)
	}
	if config.DeferredValidation != nil {
		for _, inert := range config.DeferredValidation.InertPaths {
			if invalid(inert) {
				return nil, false
			}
			pathspecs = append(pathspecs, ":(exclude)"+inert)
		}
	}
	return pathspecs, true
}

type bisectCandidate struct {
	taskID string
	commit string
}

// bisect attributes a command that failed at head, probing in the command's
// lane worktree. The baseline is the latest batch that passed it, or where its
// earliest effective deferral's merge started. Candidates are forward merges
// between baseline and head in integration ancestry order. A culprit is named
// only when its predecessor was executed and passed and its own commit was
// executed and failed; under non-monotonic history the result is a reproduced
// transition, not necessarily the first.
func (r *deferredValidationRunner) bisect(ctx context.Context, state *models.State, worktree, head string, command models.DeferredValidationCommand) (models.DeferredValidationCulprit, bool) {
	culprit := models.DeferredValidationCulprit{CommandSHA256: command.CommandSHA256}
	// Every Git read below goes through the bounded, admitted executor: a
	// halt aborts the batch and a hung read ends as an unattributed culprit.
	baseline, halted := r.bisectBaseline(ctx, deferredValidationLedger(state), command.CommandSHA256, head)
	if halted {
		return culprit, true
	}
	if baseline == "" {
		return unattributedCulprit(culprit, "no_baseline"), false
	}
	ancestor, res := r.isAncestor(ctx, baseline, head)
	if res.halted {
		return culprit, true
	}
	if !ancestor {
		return unattributedCulprit(culprit, "baseline_not_in_ancestry"), false
	}
	candidates, res := r.bisectCandidates(ctx, state, baseline, head)
	if res.halted {
		return culprit, true
	}
	if res.err != nil {
		return unattributedCulprit(culprit, "history_unavailable"), false
	}
	return r.bisectSearch(ctx, state, worktree, head, command, baseline, candidates, culprit)
}

// unattributedCulprit is culprit with its attribution withdrawn for reason.
func unattributedCulprit(culprit models.DeferredValidationCulprit, reason string) models.DeferredValidationCulprit {
	culprit.TaskID, culprit.MergeCommit, culprit.Unattributed, culprit.Reason = "", "", true, reason
	return culprit
}

// bisectBaseline is the commit the command last passed at, when it is an
// ancestor of head, else the parent state of the earliest effective deferral's
// merge. It is "" when neither is known; the bool reports a halt.
func (r *deferredValidationRunner) bisectBaseline(ctx context.Context, ledger *models.DeferredValidationLedger, commandSHA, head string) (string, bool) {
	if passed := ledger.LatestPassFor(commandSHA); passed != nil {
		ancestor, res := r.isAncestor(ctx, passed.Commit, head)
		if res.halted {
			return "", true
		}
		if ancestor {
			return passed.Commit, false
		}
	}
	halted := false
	earliest := earliestDeferral(ledger.EffectiveDeferrals(commandSHA), func(ancestor, descendant string) (bool, error) {
		before, res := r.isAncestor(ctx, ancestor, descendant)
		halted = halted || res.halted
		return before, res.err
	})
	if halted {
		return "", true
	}
	if earliest == nil {
		return "", false
	}
	before, res := r.deferralBefore(ctx, *earliest)
	if res.halted {
		return "", true
	}
	if res.err != nil {
		return "", false
	}
	return before, false
}

// bisectSearch binary-searches candidates for the first merge whose commit
// fails, probing the baseline first and recording each executed probe as a
// step of culprit. The commit at head is known to fail.
func (r *deferredValidationRunner) bisectSearch(ctx context.Context, state *models.State, worktree, head string, command models.DeferredValidationCommand, baseline string, candidates []bisectCandidate, culprit models.DeferredValidationCulprit) (models.DeferredValidationCulprit, bool) {
	probes := map[string]models.DeferredValidationResultStatus{head: models.DeferredValidationResultFailed}
	step := func(rev string) (models.DeferredValidationResultStatus, bool) {
		if status, ok := probes[rev]; ok {
			return status, false
		}
		status := models.DeferredValidationResultUnavailable
		exitCode := -1
		if res := r.prepareProbe(ctx, state, worktree, rev); res.halted {
			return status, true
		} else if res.err == nil {
			result, halted := r.probe(ctx, state.Config, worktree, rev, command, false)
			if halted {
				return status, true
			}
			status, exitCode = result.Status, result.ExitCode
		}
		probes[rev] = status
		culprit.Steps = append(culprit.Steps, models.DeferredValidationProbe{Commit: rev, Status: status, ExitCode: exitCode})
		return status, false
	}
	status, halted := step(baseline)
	if halted {
		return culprit, true
	}
	switch status {
	case models.DeferredValidationResultPassed:
	case models.DeferredValidationResultFailed:
		return unattributedCulprit(culprit, "fails_at_baseline"), false
	default:
		return unattributedCulprit(culprit, fmt.Sprintf("setup_unavailable at %s", baseline)), false
	}
	lo, hi := -1, len(candidates)
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		status, halted := step(candidates[mid].commit)
		if halted {
			return culprit, true
		}
		switch status {
		case models.DeferredValidationResultPassed:
			lo = mid
		case models.DeferredValidationResultFailed:
			hi = mid
		default:
			return unattributedCulprit(culprit, fmt.Sprintf("setup_unavailable at %s", candidates[mid].commit)), false
		}
	}
	if hi == len(candidates) {
		return unattributedCulprit(culprit, "fails_after_last_merge"), false
	}
	// hi-lo == 1: the predecessor (baseline or candidates[lo]) was executed and
	// passed, and candidates[hi] was executed and failed (at head, by the batch).
	culprit.TaskID, culprit.MergeCommit = candidates[hi].taskID, candidates[hi].commit
	return culprit, false
}

// bisectCandidates lists forward merges whose resulting commit lies on the
// ancestry path baseline..head, ordered by that path.
func (r *deferredValidationRunner) bisectCandidates(ctx context.Context, state *models.State, baseline, head string) ([]bisectCandidate, execResult) {
	list := r.git(ctx, r.root, "rev-list", "--ancestry-path", "--reverse", baseline+".."+head)
	if list.err != nil {
		return nil, list
	}
	position := map[string]int{}
	for i, commit := range strings.Fields(list.raw) {
		position[commit] = i
	}
	byCommit := map[string]bisectCandidate{}
	for _, receipt := range state.Goal.Integration.MutationReceipts {
		if _, onPath := position[receipt.AfterCommit]; !onPath || receipt.BeforeCommit == receipt.AfterCommit {
			continue
		}
		forward, res := r.isAncestor(ctx, receipt.BeforeCommit, receipt.AfterCommit)
		if res.halted {
			return nil, res
		}
		if !forward {
			continue
		}
		byCommit[receipt.AfterCommit] = bisectCandidate{taskID: receipt.TaskID, commit: receipt.AfterCommit}
	}
	candidates := make([]bisectCandidate, 0, len(byCommit))
	for _, candidate := range byCommit {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool { return position[candidates[i].commit] < position[candidates[j].commit] })
	return candidates, execResult{}
}
