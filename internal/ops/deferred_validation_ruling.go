package ops

import (
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

// Operator authority over deferred validation (ADR-0204). A ruling defers an
// exact canonical command for every strict task listing it; a retirement
// removes an obsolete command from the effective batch set; settlement clears
// a predecessor runner the platform cannot fence; an abort ends the live
// batch. Each requires a ruling text
// so the receipt and ledger name the authority they rest on.

// deferredValidationRulingAction and deferredValidationRulingOutcome are the
// operator writes and their results; DeferredValidationRulingResult keeps
// string fields and takes them by conversion.
type (
	deferredValidationRulingAction  string
	deferredValidationRulingOutcome string
)

const (
	rulingActionRule   deferredValidationRulingAction = "rule"
	rulingActionUnrule deferredValidationRulingAction = "unrule"
	rulingActionRetire deferredValidationRulingAction = "retire"
	rulingActionSettle deferredValidationRulingAction = "settle"
	rulingActionAbort  deferredValidationRulingAction = "abort"

	rulingOutcomeUnchanged deferredValidationRulingOutcome = "unchanged"
	rulingOutcomeSet       deferredValidationRulingOutcome = "set"
	rulingOutcomeRemoved   deferredValidationRulingOutcome = "removed"
	rulingOutcomeRetired   deferredValidationRulingOutcome = "retired"
	rulingOutcomeSettled   deferredValidationRulingOutcome = "settled"
	rulingOutcomeRequested deferredValidationRulingOutcome = "requested"
)

// DeferredValidationRulingResult reports one operator write.
type DeferredValidationRulingResult struct {
	Action  string `json:"action"`
	Outcome string `json:"outcome"`
	Command string `json:"command,omitempty"`
}

func validateRulingCommand(command, ruling string, needRuling bool) error {
	if errs := models.ValidationCommandViolations("command", []string{command}); len(errs) > 0 {
		return &PreconditionError{Reason: errs[0].Error()}
	}
	if needRuling && strings.TrimSpace(ruling) == "" {
		return &PreconditionError{Reason: "--ruling must name the decision that authorizes this change"}
	}
	return nil
}

func auditDeferredValidation(action deferredValidationRulingAction, outcome, command, ruling string) {
	mask := secretmask.New()
	log.Printf("deferred_validation action=%s outcome=%s actor=%q command=%q ruling=%q",
		action, outcome, "operator", mask.MaskText(command), boundPostWorktreeCmd(mask.MaskText(ruling)))
}

// RuleDeferredValidation records an operator ruling deferring command. An
// identical command already ruled is a no-op.
func RuleDeferredValidation(projectRoot, command, ruling string) (*DeferredValidationRulingResult, error) {
	if err := validateRulingCommand(command, ruling, true); err != nil {
		return nil, err
	}
	result := &DeferredValidationRulingResult{Action: string(rulingActionRule), Command: secretmask.New().MaskText(command)}
	err := db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		if models.DeferredValidationRulingFor(state.Config, command) != nil {
			result.Outcome = string(rulingOutcomeUnchanged)
			return nil
		}
		if state.Config.DeferredValidation == nil {
			state.Config.DeferredValidation = &models.DeferredValidationConfig{}
		}
		state.Config.DeferredValidation.Rulings = append(state.Config.DeferredValidation.Rulings, models.DeferredValidationRuling{
			Command: command, Ruling: strings.TrimSpace(ruling), RecordedAt: time.Now().UTC(),
		})
		result.Outcome = string(rulingOutcomeSet)
		return nil
	})
	auditDeferredValidation(rulingActionRule, result.Outcome, command, ruling)
	if err != nil {
		return nil, fmt.Errorf("record deferred-validation ruling: %w", err)
	}
	return result, nil
}

// UnruleDeferredValidation withdraws a ruling. Receipts that deferred the
// command under it stop satisfying reviewer assignment; receipts that
// executed it are unaffected.
func UnruleDeferredValidation(projectRoot, command string) (*DeferredValidationRulingResult, error) {
	if err := validateRulingCommand(command, "", false); err != nil {
		return nil, err
	}
	result := &DeferredValidationRulingResult{Action: string(rulingActionUnrule), Command: secretmask.New().MaskText(command)}
	err := db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		result.Outcome = string(rulingOutcomeUnchanged)
		config := state.Config.DeferredValidation
		if config == nil {
			return nil
		}
		kept := config.Rulings[:0]
		for _, ruling := range config.Rulings {
			if ruling.Command == command {
				result.Outcome = string(rulingOutcomeRemoved)
				continue
			}
			kept = append(kept, ruling)
		}
		config.Rulings = kept
		return nil
	})
	auditDeferredValidation(rulingActionUnrule, result.Outcome, command, "")
	if err != nil {
		return nil, fmt.Errorf("withdraw deferred-validation ruling: %w", err)
	}
	return result, nil
}

// RetireDeferredValidation removes an obsolete command from the effective set
// for every deferral recorded so far. History stays immutable, and a later
// deferral of the same command re-enters the set.
func RetireDeferredValidation(projectRoot, command, ruling string) (*DeferredValidationRulingResult, error) {
	if err := validateRulingCommand(command, ruling, true); err != nil {
		return nil, err
	}
	sha := fmt.Sprintf("%x", sha256.Sum256([]byte(command)))
	result := &DeferredValidationRulingResult{Action: string(rulingActionRetire), Command: secretmask.New().MaskText(command)}
	err := db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		previous := db.CloneState(state)
		ledger := deferredValidationLedger(state)
		effective := false
		for _, candidate := range ledger.EffectiveCommands() {
			effective = effective || candidate.CommandSHA256 == sha
		}
		if !effective {
			return &PreconditionError{Reason: "the command is not in the effective deferred-validation set"}
		}
		ledger.Retirements = append(ledger.Retirements, models.DeferredValidationRetirement{
			Command: command, CommandSHA256: sha, Ruling: strings.TrimSpace(ruling),
			RecordedAt: time.Now().UTC(), CoversDeferrals: len(ledger.Deferrals),
		})
		result.Outcome = string(rulingOutcomeRetired)
		return validateIntegrationLifecycleTransition(previous, state)
	})
	auditDeferredValidation(rulingActionRetire, result.Outcome, command, ruling)
	if err != nil {
		return nil, fmt.Errorf("retire deferred command: %w", err)
	}
	return result, nil
}

// SettleDeferredValidation clears a predecessor runner record after the
// operator confirmed none of its processes still runs. It requires the runner
// lock, so it cannot clear a live runner's record.
func SettleDeferredValidation(projectRoot, ruling string) (*DeferredValidationRulingResult, error) {
	if strings.TrimSpace(ruling) == "" {
		return nil, &PreconditionError{Reason: "--ruling must record what the operator confirmed"}
	}
	dir := deferredValidationDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	held, acquired, err := filelock.New(filepath.Join(dir, deferredValidationRunnerLockName)).TryHold(lockLabelDeferredValidationSettle)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, &PreconditionError{Reason: "a deferred-validation runner holds the lock; it is live and cannot be settled"}
	}
	defer func() { _ = held.Release() }()
	result := &DeferredValidationRulingResult{Action: string(rulingActionSettle), Outcome: string(rulingOutcomeUnchanged)}
	err = db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		previous := db.CloneState(state)
		ledger := deferredValidationLedger(state)
		if ledger == nil || ledger.Runner == nil {
			return nil
		}
		now := time.Now().UTC()
		ledger.AppendBatch(models.DeferredValidationBatch{
			Commit: ledger.Runner.Commit, StartedAt: ledger.Runner.StartedAt, FinishedAt: now,
			Status: models.DeferredValidationBatchAborted, Reason: string(deferredValidationReasonRunnerSettled),
		})
		ledger.Runner = nil
		result.Outcome = string(rulingOutcomeSettled)
		return validateIntegrationLifecycleTransition(previous, state)
	})
	auditDeferredValidation(rulingActionSettle, result.Outcome, "", ruling)
	if err != nil {
		return nil, fmt.Errorf("settle deferred-validation runner: %w", err)
	}
	return result, nil
}

// AbortDeferredValidation records an operator's request that the live runner
// end its batch (D-181). A runner is live only while it holds the runner lock;
// the request is written onto the runner record present at the write, which
// the runner polls by its own token. "requested" means recorded, not done: the
// batch's aborted_by_operator reason confirms the abort.
func AbortDeferredValidation(projectRoot, ruling string) (*DeferredValidationRulingResult, error) {
	if strings.TrimSpace(ruling) == "" {
		return nil, &PreconditionError{Reason: "--ruling must name the decision that authorizes this abort"}
	}
	dir := deferredValidationDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	held, acquired, err := filelock.New(filepath.Join(dir, deferredValidationRunnerLockName)).TryHold(lockLabelDeferredValidationAbort)
	if err != nil {
		return nil, err
	}
	if acquired {
		_ = held.Release()
		return nil, &PreconditionError{Reason: fmt.Sprintf("no deferred-validation runner is live; a record a dead runner left is recovered by the next run, or by %s when unsettled", brand.Command(deferredValidationOperation, "settle"))}
	}
	result := &DeferredValidationRulingResult{Action: string(rulingActionAbort)}
	err = db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		previous := db.CloneState(state)
		ledger := deferredValidationLedger(state)
		if ledger == nil || ledger.Runner == nil {
			return &PreconditionError{Reason: "no live deferred-validation batch: the runner holds no batch record"}
		}
		if ledger.Runner.Unsettled != "" {
			return &PreconditionError{Reason: fmt.Sprintf("the runner record is unsettled (%s); confirm none of its processes still runs, then run %s", ledger.Runner.Unsettled, brand.Command(deferredValidationOperation, "settle"))}
		}
		if ledger.Runner.Abort != nil {
			result.Outcome = string(rulingOutcomeUnchanged)
			return nil
		}
		ledger.Runner.Abort = &models.DeferredValidationAbort{Ruling: strings.TrimSpace(ruling), RequestedAt: time.Now().UTC()}
		result.Outcome = string(rulingOutcomeRequested)
		return validateIntegrationLifecycleTransition(previous, state)
	})
	auditDeferredValidation(rulingActionAbort, result.Outcome, "", ruling)
	if err != nil {
		return nil, fmt.Errorf("abort deferred-validation batch: %w", err)
	}
	return result, nil
}

// DeferredValidationStatus is the operator view of the ledger.
type DeferredValidationStatus struct {
	IntegrationHEAD string                                `json:"integration_head"`
	Effective       []DeferredValidationStatusCommand     `json:"effective"`
	Rulings         []DeferredValidationStatusCommand     `json:"rulings,omitempty"`
	Runner          *models.DeferredValidationRunner      `json:"runner,omitempty"`
	LatestBatches   []models.DeferredValidationBatch      `json:"latest_batches,omitempty"`
	Retirements     []models.DeferredValidationRetirement `json:"retirements,omitempty"`
}

// DeferredValidationStatusCommand is one masked command in the status view.
type DeferredValidationStatusCommand struct {
	Command       string `json:"command"`
	CommandSHA256 string `json:"command_sha256,omitempty"`
	Ruling        string `json:"ruling,omitempty"`
}

// ReadDeferredValidationStatus returns the masked ledger view.
func ReadDeferredValidationStatus(projectRoot string) (*DeferredValidationStatus, error) {
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return nil, err
	}
	head, err := ResolveIntegrationHEAD(projectRoot, state.Config.IntegrationBranch)
	if err != nil {
		return nil, err
	}
	mask := acceptanceExecutionMask(os.Environ())
	status := &DeferredValidationStatus{IntegrationHEAD: head}
	ledger := deferredValidationLedger(state)
	for _, command := range ledger.EffectiveCommands() {
		status.Effective = append(status.Effective, DeferredValidationStatusCommand{Command: mask(command.Command), CommandSHA256: command.CommandSHA256})
	}
	if config := state.Config.DeferredValidation; config != nil {
		for _, ruling := range config.Rulings {
			status.Rulings = append(status.Rulings, DeferredValidationStatusCommand{Command: mask(ruling.Command), Ruling: mask(ruling.Ruling)})
		}
	}
	if ledger != nil {
		if ledger.Runner != nil {
			runner := *ledger.Runner
			if runner.Abort != nil {
				abort := *runner.Abort
				abort.Ruling = mask(abort.Ruling)
				runner.Abort = &abort
			}
			status.Runner = &runner
		}
		status.Retirements = slices.Clone(ledger.Retirements)
		for i := range status.Retirements {
			status.Retirements[i].Command = mask(status.Retirements[i].Command)
			status.Retirements[i].Ruling = mask(status.Retirements[i].Ruling)
		}
		// Batches were masked in the runner's environment; mask again in the
		// viewer's, whose secrets can differ.
		first := max(0, len(ledger.Batches)-5)
		for _, batch := range ledger.Batches[first:] {
			batch.Reason = mask(batch.Reason)
			batch.Results = slices.Clone(batch.Results)
			for i := range batch.Results {
				batch.Results[i].Command = mask(batch.Results[i].Command)
				batch.Results[i].Excerpt = mask(batch.Results[i].Excerpt)
			}
			status.LatestBatches = append(status.LatestBatches, batch)
		}
	}
	return status, nil
}
