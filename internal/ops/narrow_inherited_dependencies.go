package ops

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const narrowInheritedDependenciesOperation = "narrow-inherited-dependencies"

// NarrowSelection authors inherit_inputs for one output entry of a producer
// after its children were generated. Index names the producer's output[]
// position, which is also the generated child's position.
type NarrowSelection struct {
	Index         int                   `yaml:"index" json:"index"`
	InheritInputs *models.InheritInputs `yaml:"inherit_inputs" json:"inherit_inputs"`
}

// NarrowSelectionsFile is the on-disk shape accepted by the CLI.
type NarrowSelectionsFile struct {
	Outputs []NarrowSelection `yaml:"outputs" json:"outputs"`
}

// NarrowedChild reports what happened to one generated child.
type NarrowedChild struct {
	TaskID                string   `json:"task_id"`
	OutputIndex           int      `json:"output_index"`
	Status                string   `json:"status"`
	Action                string   `json:"action"` // narrowAction value
	SkipReason            string   `json:"skip_reason,omitempty"`
	RemovedDependencies   []string `json:"removed_dependencies,omitempty"`
	AddedDependencies     []string `json:"added_dependencies,omitempty"`
	CanonicalDependencies []string `json:"canonical_dependencies,omitempty"`
}

// NarrowInheritedDependenciesResult contains the outcome of narrowing one
// producer's generated children.
type NarrowInheritedDependenciesResult struct {
	models.LifecycleOutcome
	ProducerID string          `json:"producer_id"`
	Transition string          `json:"transition"`
	Children   []NarrowedChild `json:"children"`
	Narrowed   int             `json:"narrowed"`
	Unchanged  int             `json:"unchanged"`
	Skipped    int             `json:"skipped"`
	Warnings   []string        `json:"warnings,omitempty"`
}

func (r *NarrowInheritedDependenciesResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// LoadNarrowSelectionsFile parses a YAML or JSON selections file.
func LoadNarrowSelectionsFile(path string) ([]NarrowSelection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot read selections file: %v", err)}
	}
	var file NarrowSelectionsFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot parse selections file: %v", err)}
	}
	if len(file.Outputs) == 0 {
		return nil, &PreconditionError{Reason: "selections file names no outputs"}
	}
	return file.Outputs, nil
}

// NarrowInheritedDependencies applies post-hoc inherit_inputs to a MERGED
// producer's output[] and rewrites the depends_on of its generated children
// that are still in their initial status, removing the inherited phase-gate
// edges the selection does not keep (ADR-0137 applied after generation).
//
// It is a metadata repair operation. Terminal, claimed, blocked, executing,
// and reviewing children are left untouched and reported as skipped. Edges
// that did not come from phase-gate inheritance (siblings, task_depends_on,
// manual retargets) are never removed. It fails closed: any selection that
// cannot be resolved against the current upstream outputs errors the whole
// operation and nothing is written.
func NarrowInheritedDependencies(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason, agentID string) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, agentID, nil)
}

// NarrowInheritedDependenciesWithAuthority fences the rewrite with the
// orchestrator's registration generation.
func NarrowInheritedDependenciesWithAuthority(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason string, authority models.AgentAuthority) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, authority.ID, &authority)
}

// NarrowInheritedDependenciesWithAuthorityAndOptions fences an identified rewrite.
func NarrowInheritedDependenciesWithAuthorityAndOptions(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, authority.ID, &authority, opts)
}

// narrowAction is the finite set of outcomes one generated child can report.
type narrowAction string

const (
	narrowActionNarrowed  narrowAction = "narrowed"
	narrowActionUnchanged narrowAction = "unchanged"
	narrowActionSkipped   narrowAction = "skipped"
)

// narrowRequest is the caller's intent, validated before any state is read.
type narrowRequest struct {
	producerID string
	transition string
	selections []NarrowSelection
	reason     string
	agentID    string
}

func (r narrowRequest) check() error {
	if r.producerID == "" {
		return &PreconditionError{Reason: "producer task ID is required"}
	}
	if r.reason == "" {
		return &PreconditionError{Reason: "reason is required"}
	}
	if r.agentID == "" {
		return &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	if len(r.selections) == 0 {
		return &PreconditionError{Reason: "at least one output selection is required"}
	}
	return validateNarrowSelectionsShape(r.selections)
}

func narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *NarrowInheritedDependenciesResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := LifecycleEffectsNone
	defer func() {
		retErr = WrapLifecycleError(narrowInheritedDependenciesOperation, observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(narrowInheritedDependenciesOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	req := narrowRequest{producerID: producerID, transition: transitionName, selections: selections, reason: reason, agentID: agentID}
	if err := req.check(); err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := RequestBlackboard(lp.StatePath(), authority, opts)
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(narrowInheritedDependenciesOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}

	var result NarrowInheritedDependenciesResult
	now := time.Now().UTC()

	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(narrowInheritedDependenciesOperation, observed, callbackErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		}()
		producer := state.FindTask(req.producerID)
		if producer == nil {
			return &errors.NotFoundError{Entity: "task", ID: req.producerID}
		}
		copy := *producer
		observed = &copy

		admitted, err := admitNarrowRequest(state, producer, req, resolver, authority, opts)
		if err != nil {
			return err
		}
		if admitted.replay != nil {
			result = *admitted.replay
			return errLifecycleReplay
		}
		pass, err := newNarrowPass(state, producer, admitted.transition, resolver, req, now)
		if err != nil {
			return err
		}
		if result, err = pass.run(req.selections); err != nil {
			return err
		}
		pass.auditProducer(result, req.selections)

		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr); err != nil {
			return err
		}

		result.LifecycleOutcome, err = CompleteLifecycleRequest(producer, admitted.request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = LifecycleEffectsUnknown
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(narrowInheritedDependenciesOperation, observed, fmt.Errorf("failed to narrow inherited dependencies: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, effects)
	}

	logNarrowResult(lp, now, req, &result)
	return &result, nil
}

// narrowAdmission is what the locked callback learns from the request identity.
type narrowAdmission struct {
	transition string
	request    LifecycleRequest
	replay     *NarrowInheritedDependenciesResult
}

// admitNarrowRequest resolves the transition, builds the lifecycle request and
// answers an exact replay from its retained receipt.
func admitNarrowRequest(state *models.State, producer *models.Task, req narrowRequest, resolver *pipeline.Resolver, authority *models.AgentAuthority, opts LifecycleRequestOptions) (narrowAdmission, error) {
	resolvedTransition, err := resolveNarrowTransition(producer, req.transition, resolver)
	if err != nil {
		return narrowAdmission{}, err
	}
	request, err := NewLifecycleRequest(narrowInheritedDependenciesOperation, producer, req.agentID, authority, opts, struct {
		Transition string
		Selections []NarrowSelection
		Reason     string
	}{resolvedTransition, req.selections, req.reason})
	if err != nil {
		return narrowAdmission{}, err
	}
	receipt, err := CheckLifecycleRequest(producer, request, state.Agents)
	if err != nil {
		return narrowAdmission{}, err
	}
	admitted := narrowAdmission{transition: resolvedTransition, request: request}
	if receipt != nil {
		admitted.replay = &NarrowInheritedDependenciesResult{ProducerID: req.producerID, Transition: resolvedTransition, LifecycleOutcome: LifecycleReplayOutcome(producer, receipt, req.agentID)}
	}
	return admitted, nil
}

// logNarrowResult appends the activity-log line after the mutation committed;
// a log failure is reported as a warning, never as a failed narrowing.
func logNarrowResult(lp paths.LizaPaths, now time.Time, req narrowRequest, result *NarrowInheritedDependenciesResult) {
	entry := log.Entry{
		Timestamp: now,
		Agent:     req.agentID,
		Action:    narrowInheritedDependenciesOperation,
		Task:      &req.producerID,
		Detail:    fmt.Sprintf("%s: narrowed=%d unchanged=%d skipped=%d: %s", result.Transition, result.Narrowed, result.Unchanged, result.Skipped, req.reason),
	}
	if err := log.New(lp.LogPath()).Append(entry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
}

func resolveNarrowTransition(producer *models.Task, transitionName string, resolver *pipeline.Resolver) (string, error) {
	if transitionName != "" {
		return transitionName, nil
	}
	var executed []string
	for name, done := range producer.TransitionsExecuted {
		if !done || name == models.ReplannedTransition {
			continue
		}
		td, err := resolver.Transition(name)
		if err != nil || td.Cardinality != models.CardinalityPerSubtask {
			continue
		}
		executed = append(executed, name)
	}
	switch len(executed) {
	case 0:
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has no executed per-subtask transition; pass --transition explicitly", producer.ID)}
	case 1:
		return executed[0], nil
	default:
		sort.Strings(executed)
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has several executed per-subtask transitions (%s); pass --transition explicitly", producer.ID, strings.Join(executed, ", "))}
	}
}

func validateNarrowSelectionsShape(selections []NarrowSelection) error {
	seen := make(map[int]bool, len(selections))
	for i, selection := range selections {
		if selection.Index < 0 {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d]: negative output index %d", i, selection.Index)}
		}
		if seen[selection.Index] {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d]: duplicate output index %d", i, selection.Index)}
		}
		seen[selection.Index] = true
		if selection.InheritInputs == nil {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d] (output %d): inherit_inputs is required; use mode %q to restore the whole-phase barrier", i, selection.Index, models.InheritModeAll)}
		}
	}
	return nil
}

func cloneInheritInputs(in *models.InheritInputs) *models.InheritInputs {
	if in == nil {
		return nil
	}
	out := &models.InheritInputs{Mode: in.Mode}
	for _, selection := range in.Selections {
		out.Selections = append(out.Selections, models.InputSelection{
			UpstreamTask: selection.UpstreamTask,
			Outputs:      append([]int(nil), selection.Outputs...),
		})
	}
	return out
}

func selectedIndexList(selections []NarrowSelection) []string {
	indexes := make([]int, 0, len(selections))
	for _, selection := range selections {
		indexes = append(indexes, selection.Index)
	}
	sort.Ints(indexes)
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, strconv.Itoa(index))
	}
	return out
}
