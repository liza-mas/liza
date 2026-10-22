package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Deferred-validation defaults (ADR-0204).
const (
	// DefaultDeferredValidationBatchEveryMerges is the number of integration
	// merges after which a periodic batch becomes due.
	DefaultDeferredValidationBatchEveryMerges = 5
	// DefaultDeferredValidationSetupTimeoutSeconds bounds the project setup
	// command the batch runs before each probe.
	DefaultDeferredValidationSetupTimeoutSeconds = 1800
	// DeferredValidationMaxAttemptsPerCommit is how many aborted or
	// infra-failed batches the same integration commit may accumulate before
	// the integration gate blocks.
	DeferredValidationMaxAttemptsPerCommit = 3
	// DeferredValidationBatchRetention bounds the batch history kept in state;
	// the latest passing batch of each command is always kept as well.
	DeferredValidationBatchRetention = 20
	// DeferredValidationDiscoveryBy identifies discoveries the batch records.
	DeferredValidationDiscoveryBy = "deferred-validation"
)

// DeferredValidationConfig is the project-scoped batch configuration and the
// operator rulings that defer exact canonical commands.
type DeferredValidationConfig struct {
	// BatchEveryMerges is the periodic batch interval; non-positive means
	// DefaultDeferredValidationBatchEveryMerges.
	BatchEveryMerges int `yaml:"batch_every_merges,omitempty" json:"batch_every_merges,omitempty"`
	// SetupTimeoutSeconds bounds each probe's setup; non-positive means
	// DefaultDeferredValidationSetupTimeoutSeconds.
	SetupTimeoutSeconds int                        `yaml:"setup_timeout_seconds,omitempty" json:"setup_timeout_seconds,omitempty"`
	Rulings             []DeferredValidationRuling `yaml:"rulings,omitempty" json:"rulings,omitempty"`
	// InertPaths are Git glob pathspecs whose changes never stop a command's
	// latest pass from being carried forward (D-230).
	InertPaths []string `yaml:"inert_paths,omitempty" json:"inert_paths,omitempty"`
	// Scopes declare the paths an exact command covers; a change outside
	// them does not stop its latest pass from being carried forward (D-230).
	Scopes []DeferredValidationScope `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}

// DeferredValidationScope declares, as Git glob pathspecs, the paths whose
// changes can alter one exact command's outcome.
type DeferredValidationScope struct {
	Command string   `yaml:"command" json:"command"`
	Paths   []string `yaml:"paths" json:"paths"`
}

// DeferredValidationScopeFor returns the declared scope of an exact command,
// or nil.
func DeferredValidationScopeFor(config Config, command string) *DeferredValidationScope {
	if config.DeferredValidation == nil {
		return nil
	}
	for i := range config.DeferredValidation.Scopes {
		if config.DeferredValidation.Scopes[i].Command == command {
			return &config.DeferredValidation.Scopes[i]
		}
	}
	return nil
}

// DeferredValidationRuling defers one exact canonical command for every
// strict task that lists it, on the operator's recorded authority.
type DeferredValidationRuling struct {
	Command    string    `yaml:"command" json:"command"`
	Ruling     string    `yaml:"ruling" json:"ruling"`
	RecordedAt time.Time `yaml:"recorded_at" json:"recorded_at"`
}

// EffectiveBatchEveryMerges returns the configured positive interval or the
// default.
func EffectiveBatchEveryMerges(config Config) int {
	if config.DeferredValidation != nil && config.DeferredValidation.BatchEveryMerges > 0 {
		return config.DeferredValidation.BatchEveryMerges
	}
	return DefaultDeferredValidationBatchEveryMerges
}

// EffectiveDeferredValidationSetupTimeout returns the configured positive
// setup bound or the default.
func EffectiveDeferredValidationSetupTimeout(config Config) time.Duration {
	seconds := DefaultDeferredValidationSetupTimeoutSeconds
	if config.DeferredValidation != nil && config.DeferredValidation.SetupTimeoutSeconds > 0 {
		seconds = config.DeferredValidation.SetupTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// DeferredValidationRulingFor returns the operator ruling for an exact
// command, or nil.
func DeferredValidationRulingFor(config Config, command string) *DeferredValidationRuling {
	if config.DeferredValidation == nil {
		return nil
	}
	for i := range config.DeferredValidation.Rulings {
		if config.DeferredValidation.Rulings[i].Command == command {
			return &config.DeferredValidation.Rulings[i]
		}
	}
	return nil
}

// DeferredValidationLedger is the goal-scoped record of merged deferrals,
// operator retirements, batch results and the single runner.
type DeferredValidationLedger struct {
	Deferrals   []DeferredValidationDeferral   `yaml:"deferrals,omitempty" json:"deferrals,omitempty"`
	Retirements []DeferredValidationRetirement `yaml:"retirements,omitempty" json:"retirements,omitempty"`
	Batches     []DeferredValidationBatch      `yaml:"batches,omitempty" json:"batches,omitempty"`
	// LastBatchID is the high-water batch identity, so an identity retention
	// pruned is never written again.
	LastBatchID int                       `yaml:"last_batch_id,omitempty" json:"last_batch_id,omitempty"`
	Runner      *DeferredValidationRunner `yaml:"runner,omitempty" json:"runner,omitempty"`
}

// DeferredValidationCommand is one exact deferred command. Command is the
// canonical command the batch executes, as stored in task validation.
type DeferredValidationCommand struct {
	Command        string                      `yaml:"command" json:"command"`
	CommandSHA256  string                      `yaml:"command_sha256" json:"command_sha256"`
	TimeoutSeconds int                         `yaml:"timeout_seconds" json:"timeout_seconds"`
	Authority      AcceptanceDeferralAuthority `yaml:"authority" json:"authority"`
}

// DeferredValidationDeferral records the deferred commands of one MERGED task.
// BeforeCommit is the integration commit its merge started from.
type DeferredValidationDeferral struct {
	TaskID       string                      `yaml:"task_id" json:"task_id"`
	MergeCommit  string                      `yaml:"merge_commit" json:"merge_commit"`
	BeforeCommit string                      `yaml:"before_commit,omitempty" json:"before_commit,omitempty"`
	RecordedAt   time.Time                   `yaml:"recorded_at" json:"recorded_at"`
	Commands     []DeferredValidationCommand `yaml:"commands" json:"commands"`
}

// DeferredValidationRetirement removes an obsolete command from the effective
// set for the first CoversDeferrals deferrals, on an operator ruling.
type DeferredValidationRetirement struct {
	Command         string    `yaml:"command" json:"command"`
	CommandSHA256   string    `yaml:"command_sha256" json:"command_sha256"`
	Ruling          string    `yaml:"ruling" json:"ruling"`
	RecordedAt      time.Time `yaml:"recorded_at" json:"recorded_at"`
	CoversDeferrals int       `yaml:"covers_deferrals" json:"covers_deferrals"`
}

// DeferredValidationBatchStatus is the outcome of one batch.
type DeferredValidationBatchStatus string

const (
	DeferredValidationBatchPassed      DeferredValidationBatchStatus = "passed"
	DeferredValidationBatchFailed      DeferredValidationBatchStatus = "failed"
	DeferredValidationBatchAborted     DeferredValidationBatchStatus = "aborted"
	DeferredValidationBatchInfraFailed DeferredValidationBatchStatus = "infra_failed"
)

// IsValid reports whether the status is part of the persisted schema.
func (s DeferredValidationBatchStatus) IsValid() bool {
	switch s {
	case DeferredValidationBatchPassed, DeferredValidationBatchFailed, DeferredValidationBatchAborted, DeferredValidationBatchInfraFailed:
		return true
	}
	return false
}

// DeferredValidationResultStatus is the outcome of one command or probe.
type DeferredValidationResultStatus string

const (
	DeferredValidationResultPassed       DeferredValidationResultStatus = "passed"
	DeferredValidationResultFailed       DeferredValidationResultStatus = "failed"
	DeferredValidationResultContaminated DeferredValidationResultStatus = "contaminated"
	// DeferredValidationResultUnavailable is a bisect probe whose revision
	// could not be prepared (checkout, setup or a clean tree failed).
	DeferredValidationResultUnavailable DeferredValidationResultStatus = "unavailable"
	DeferredValidationResultNotRun      DeferredValidationResultStatus = "not_run"
	// DeferredValidationResultCarriedForward is a command not run because
	// nothing in its scope changed since its latest executed pass, named by
	// CarriedFrom (D-230).
	DeferredValidationResultCarriedForward DeferredValidationResultStatus = "carried_forward"
)

// IsValid reports whether the status is part of the persisted schema.
func (s DeferredValidationResultStatus) IsValid() bool {
	switch s {
	case DeferredValidationResultPassed, DeferredValidationResultFailed, DeferredValidationResultContaminated,
		DeferredValidationResultUnavailable, DeferredValidationResultNotRun, DeferredValidationResultCarriedForward:
		return true
	}
	return false
}

// Executed reports whether the command ran to an outcome on the code.
func (s DeferredValidationResultStatus) Executed() bool {
	switch s {
	case DeferredValidationResultPassed, DeferredValidationResultFailed, DeferredValidationResultContaminated:
		return true
	}
	return false
}

// DeferredValidationBatch records one batch at one integration commit.
type DeferredValidationBatch struct {
	ID         int                           `yaml:"id" json:"id"`
	Commit     string                        `yaml:"commit" json:"commit"`
	StartedAt  time.Time                     `yaml:"started_at" json:"started_at"`
	FinishedAt time.Time                     `yaml:"finished_at" json:"finished_at"`
	Status     DeferredValidationBatchStatus `yaml:"status" json:"status"`
	Reason     string                        `yaml:"reason,omitempty" json:"reason,omitempty"`
	Forced     bool                          `yaml:"forced,omitempty" json:"forced,omitempty"`
	Results    []DeferredValidationResult    `yaml:"results,omitempty" json:"results,omitempty"`
	Culprits   []DeferredValidationCulprit   `yaml:"culprits,omitempty" json:"culprits,omitempty"`
}

// DeferredValidationResult is one command's outcome in a batch. Command and
// Excerpt are masked presentation; Excerpt is bounded to the state text limit.
// OutputSHA256 names a failed command's full masked output in the project's
// tool-result store (`tool-result read`); it is empty when the capture
// overflowed (output omitted) or the store could not keep it. CarriedFrom is
// the commit of the pass a carried_forward result reuses.
type DeferredValidationResult struct {
	Command       string                         `yaml:"command" json:"command"`
	CommandSHA256 string                         `yaml:"command_sha256" json:"command_sha256"`
	Status        DeferredValidationResultStatus `yaml:"status" json:"status"`
	ExitCode      int                            `yaml:"exit_code" json:"exit_code"`
	StartedAt     time.Time                      `yaml:"started_at" json:"started_at"`
	FinishedAt    time.Time                      `yaml:"finished_at" json:"finished_at"`
	Excerpt       string                         `yaml:"excerpt,omitempty" json:"excerpt,omitempty"`
	OutputSHA256  string                         `yaml:"output_sha256,omitempty" json:"output_sha256,omitempty"`
	CarriedFrom   string                         `yaml:"carried_from,omitempty" json:"carried_from,omitempty"`
}

// DeferredValidationCulprit attributes one failing command. TaskID is set only
// when a passing predecessor and a failing successor were both executed;
// otherwise Unattributed holds and Reason says why.
type DeferredValidationCulprit struct {
	CommandSHA256 string                    `yaml:"command_sha256" json:"command_sha256"`
	TaskID        string                    `yaml:"task_id,omitempty" json:"task_id,omitempty"`
	MergeCommit   string                    `yaml:"merge_commit,omitempty" json:"merge_commit,omitempty"`
	Unattributed  bool                      `yaml:"unattributed,omitempty" json:"unattributed,omitempty"`
	Reason        string                    `yaml:"reason,omitempty" json:"reason,omitempty"`
	Steps         []DeferredValidationProbe `yaml:"steps,omitempty" json:"steps,omitempty"`
}

// DeferredValidationProbe is one bisect execution of a command at a commit.
type DeferredValidationProbe struct {
	Commit   string                         `yaml:"commit" json:"commit"`
	Status   DeferredValidationResultStatus `yaml:"status" json:"status"`
	ExitCode int                            `yaml:"exit_code" json:"exit_code"`
}

// DeferredValidationRunner identifies the batch runner holding the runner
// lock. The OS lock is the authority; this record fences publication.
type DeferredValidationRunner struct {
	Token     string    `yaml:"token" json:"token"`
	PID       int       `yaml:"pid" json:"pid"`
	Commit    string    `yaml:"commit" json:"commit"`
	StartedAt time.Time `yaml:"started_at" json:"started_at"`
	// Unsettled is set when the runner ended without establishing that its
	// processes ended; the record then fences successors until an operator
	// settles it.
	Unsettled string `yaml:"unsettled,omitempty" json:"unsettled,omitempty"`
}

// EffectiveCommands returns the distinct deferred commands not covered by a
// retirement, ordered by first deferral. A retirement covers deferrals
// recorded before it; a later deferral of the same command re-enters the set.
func (ledger *DeferredValidationLedger) EffectiveCommands() []DeferredValidationCommand {
	if ledger == nil {
		return nil
	}
	index := map[string]int{}
	var commands []DeferredValidationCommand
	for i, deferral := range ledger.Deferrals {
		for _, command := range deferral.Commands {
			if ledger.retired(command.CommandSHA256, i) {
				continue
			}
			if at, seen := index[command.CommandSHA256]; seen {
				if command.TimeoutSeconds > commands[at].TimeoutSeconds {
					commands[at].TimeoutSeconds = command.TimeoutSeconds
				}
				continue
			}
			index[command.CommandSHA256] = len(commands)
			commands = append(commands, command)
		}
	}
	return commands
}

func (ledger *DeferredValidationLedger) retired(commandSHA string, deferralIndex int) bool {
	for _, retirement := range ledger.Retirements {
		if retirement.CommandSHA256 == commandSHA && deferralIndex < retirement.CoversDeferrals {
			return true
		}
	}
	return false
}

// EffectiveDeferrals returns the deferrals that keep the command in the
// effective set, in persistence order. Persistence order is not merge order,
// so callers needing the earliest merge order these by integration ancestry.
func (ledger *DeferredValidationLedger) EffectiveDeferrals(commandSHA string) []DeferredValidationDeferral {
	if ledger == nil {
		return nil
	}
	var deferrals []DeferredValidationDeferral
	for i, deferral := range ledger.Deferrals {
		if ledger.retired(commandSHA, i) {
			continue
		}
		for _, command := range deferral.Commands {
			if command.CommandSHA256 == commandSHA {
				deferrals = append(deferrals, deferral)
				break
			}
		}
	}
	return deferrals
}

// BatchesAt returns the batches recorded at commit, oldest first.
func (ledger *DeferredValidationLedger) BatchesAt(commit string) []DeferredValidationBatch {
	if ledger == nil {
		return nil
	}
	var batches []DeferredValidationBatch
	for _, batch := range ledger.Batches {
		if batch.Commit == commit {
			batches = append(batches, batch)
		}
	}
	return batches
}

// Covers reports whether a passed batch passed or carried forward every
// command in commands.
func (batch DeferredValidationBatch) Covers(commands []DeferredValidationCommand) bool {
	if batch.Status != DeferredValidationBatchPassed {
		return false
	}
	ran := map[string]bool{}
	for _, result := range batch.Results {
		if result.Status == DeferredValidationResultPassed || result.Status == DeferredValidationResultCarriedForward {
			ran[result.CommandSHA256] = true
		}
	}
	for _, command := range commands {
		if !ran[command.CommandSHA256] {
			return false
		}
	}
	return true
}

// LatestPassFor returns the most recent batch in which the command passed.
func (ledger *DeferredValidationLedger) LatestPassFor(commandSHA string) *DeferredValidationBatch {
	if ledger == nil {
		return nil
	}
	for i := len(ledger.Batches) - 1; i >= 0; i-- {
		for _, result := range ledger.Batches[i].Results {
			if result.CommandSHA256 == commandSHA && result.Status == DeferredValidationResultPassed {
				return &ledger.Batches[i]
			}
		}
	}
	return nil
}

// LatestExecutionFor returns the most recent batch that executed the command,
// and that execution; a carried-forward or not-run result is no execution.
func (ledger *DeferredValidationLedger) LatestExecutionFor(commandSHA string) (*DeferredValidationBatch, *DeferredValidationResult) {
	if ledger == nil {
		return nil, nil
	}
	for i := len(ledger.Batches) - 1; i >= 0; i-- {
		for j, result := range ledger.Batches[i].Results {
			if result.CommandSHA256 == commandSHA && result.Status.Executed() {
				return &ledger.Batches[i], &ledger.Batches[i].Results[j]
			}
		}
	}
	return nil, nil
}

// HighestBatchID is the largest batch identity ever written, pruned or not.
func (ledger *DeferredValidationLedger) HighestBatchID() int {
	if ledger == nil {
		return 0
	}
	highest := ledger.LastBatchID
	if n := len(ledger.Batches); n > 0 && ledger.Batches[n-1].ID > highest {
		highest = ledger.Batches[n-1].ID
	}
	return highest
}

// AppendBatch gives batch the next identity, appends it, records the
// high-water identity and prunes retention. It returns the stored batch.
func (ledger *DeferredValidationLedger) AppendBatch(batch DeferredValidationBatch) DeferredValidationBatch {
	batch.ID = ledger.HighestBatchID() + 1
	ledger.Batches = append(ledger.Batches, batch)
	ledger.LastBatchID = batch.ID
	ledger.PruneBatches()
	return batch
}

// PruneBatches keeps the newest DeferredValidationBatchRetention batches plus
// the latest batch in which each command passed and the latest that executed
// it, preserving order: a pruned newer failure must never let an older pass
// be carried forward.
func (ledger *DeferredValidationLedger) PruneBatches() {
	if ledger == nil || len(ledger.Batches) <= DeferredValidationBatchRetention {
		return
	}
	keep := map[int]bool{}
	for i := len(ledger.Batches) - DeferredValidationBatchRetention; i < len(ledger.Batches); i++ {
		keep[i] = true
	}
	passed, executed := map[string]bool{}, map[string]bool{}
	for i := len(ledger.Batches) - 1; i >= 0; i-- {
		for _, result := range ledger.Batches[i].Results {
			if result.Status == DeferredValidationResultPassed && !passed[result.CommandSHA256] {
				passed[result.CommandSHA256] = true
				keep[i] = true
			}
			if result.Status.Executed() && !executed[result.CommandSHA256] {
				executed[result.CommandSHA256] = true
				keep[i] = true
			}
		}
	}
	kept := make([]DeferredValidationBatch, 0, len(keep))
	for i, batch := range ledger.Batches {
		if keep[i] {
			kept = append(kept, batch)
		}
	}
	ledger.Batches = kept
}

// DeferredValidationLedgerViolations checks the static shape of a ledger.
func DeferredValidationLedgerViolations(ledger *DeferredValidationLedger) []error {
	if ledger == nil {
		return nil
	}
	var errs []error
	for i, deferral := range ledger.Deferrals {
		field := fmt.Sprintf("deferred_validation.deferrals[%d]", i)
		if deferral.TaskID == "" || !isHexDigest(deferral.MergeCommit, 40) || len(deferral.Commands) == 0 {
			errs = append(errs, fmt.Errorf("%s: requires task_id, a full merge_commit and at least one command", field))
		}
		for j, command := range deferral.Commands {
			if strings.TrimSpace(command.Command) == "" || !isHexDigest(command.CommandSHA256, 64) || command.TimeoutSeconds < 1 || !command.Authority.IsValid() {
				errs = append(errs, fmt.Errorf("%s.commands[%d]: requires command, 64-hex command_sha256, positive timeout_seconds and a valid authority", field, j))
			}
		}
	}
	for i, retirement := range ledger.Retirements {
		if !isHexDigest(retirement.CommandSHA256, 64) || strings.TrimSpace(retirement.Ruling) == "" || retirement.CoversDeferrals < 1 || retirement.CoversDeferrals > len(ledger.Deferrals) {
			errs = append(errs, fmt.Errorf("deferred_validation.retirements[%d]: requires command_sha256, a ruling and covers_deferrals within 1..%d", i, len(ledger.Deferrals)))
		}
	}
	for i, batch := range ledger.Batches {
		field := fmt.Sprintf("deferred_validation.batches[%d]", i)
		if batch.ID < 1 || (i > 0 && batch.ID <= ledger.Batches[i-1].ID) {
			errs = append(errs, fmt.Errorf("%s: ids must be positive and strictly increasing", field))
		}
		if !isHexDigest(batch.Commit, 40) || !batch.Status.IsValid() || batch.FinishedAt.Before(batch.StartedAt) {
			errs = append(errs, fmt.Errorf("%s: requires a full commit, a valid status and finished_at >= started_at", field))
		}
		for j, result := range batch.Results {
			if !isHexDigest(result.CommandSHA256, 64) || !result.Status.IsValid() {
				errs = append(errs, fmt.Errorf("%s.results[%d]: requires command_sha256 and a valid status", field, j))
			}
			if (result.Status == DeferredValidationResultCarriedForward) != (result.CarriedFrom != "") ||
				(result.CarriedFrom != "" && !isHexDigest(result.CarriedFrom, 40)) {
				errs = append(errs, fmt.Errorf("%s.results[%d]: a carried_forward result, and only one, requires a full carried_from commit", field, j))
			}
		}
		for j, culprit := range batch.Culprits {
			if !isHexDigest(culprit.CommandSHA256, 64) || (culprit.TaskID == "") != culprit.Unattributed {
				errs = append(errs, fmt.Errorf("%s.culprits[%d]: requires command_sha256 and exactly one of task_id or unattributed", field, j))
			}
		}
	}
	if n := len(ledger.Batches); n > 0 && ledger.LastBatchID != 0 && ledger.Batches[n-1].ID > ledger.LastBatchID {
		errs = append(errs, fmt.Errorf("deferred_validation.last_batch_id: must not be below a stored batch id"))
	}
	if runner := ledger.Runner; runner != nil && (runner.Token == "" || !isHexDigest(runner.Commit, 40)) {
		errs = append(errs, fmt.Errorf("deferred_validation.runner: requires token and a full commit"))
	}
	return errs
}

// SortedDeferredCommandSHAs returns the command digests of commands, sorted.
func SortedDeferredCommandSHAs(commands []DeferredValidationCommand) []string {
	shas := make([]string, 0, len(commands))
	for _, command := range commands {
		shas = append(shas, command.CommandSHA256)
	}
	sort.Strings(shas)
	return shas
}

func isHexDigest(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
