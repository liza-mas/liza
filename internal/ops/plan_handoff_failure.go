package ops

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

const (
	handoffFailureVersion = 1
	handoffOutputRefusal  = "output_validation"
	handoffInputRefusal   = "selective_inheritance"
)

// Only initial pure refusals use this type. Recovery and graph/configuration
// failures deliberately retain their existing retry semantics.
type handoffInputError struct {
	class string
	index int
	err   error
}

func (e *handoffInputError) Error() string { return e.err.Error() }
func (e *handoffInputError) Unwrap() error { return e.err }

// PlanHandoffFailure is a persisted observation of refused material inputs.
// It is diagnostic evidence, not a hold or an executed-transition marker.
type PlanHandoffFailure struct {
	Version     int    `json:"version" yaml:"version"`
	TaskID      string `json:"task_id" yaml:"task_id"`
	Transition  string `json:"transition" yaml:"transition"`
	Class       string `json:"failure_class" yaml:"failure_class"`
	OutputIndex int    `json:"output_index" yaml:"output_index"`
	Fingerprint string `json:"input_fingerprint" yaml:"input_fingerprint"`
	Error       string `json:"error" yaml:"error"`
}

func (d PlanHandoffDomain) failureFingerprint(state *models.State, task *models.Task, failure PlanHandoffFailure) string {
	if d.resolver == nil || !d.GatesTransition(task, failure.Transition) || failure.OutputIndex < 0 || failure.OutputIndex >= len(task.Output) {
		return ""
	}
	if failure.Class != handoffOutputRefusal && failure.Class != handoffInputRefusal {
		return ""
	}
	td, err := d.resolver.Transition(failure.Transition)
	if err != nil || td.Cardinality != "per-subtask" {
		return ""
	}
	resolved, err := buildTransitionDefFromPipeline(d.resolver, failure.Transition)
	if err != nil {
		return ""
	}
	deps, _, err := canonicalizeConcreteDependencyList(state, d.resolver, task.ID, task.RolePair, task.DependsOn)
	if err != nil {
		return ""
	}
	// Empty and nil dependencies have identical expansion semantics.
	deps = slices.Clone(deps)
	slices.Sort(deps)
	deps = slices.Compact(deps)
	if len(deps) == 0 {
		deps = nil
	}
	relevantKinds := map[string]string{}
	upstreams := map[string]any{}
	if failure.Class == handoffInputRefusal {
		incumbents := collectNonTerminalByKind(state, declaredOriginals(task.Output))
		for _, entry := range task.Output {
			if id := incumbents[entry.Kind]; entry.Kind != "" && id != "" {
				relevantKinds[entry.Kind] = id
			}
		}
		entry := task.Output[failure.OutputIndex]
		if entry.InheritInputs == nil {
			return ""
		}
		for _, selection := range entry.InheritInputs.Selections {
			upstream := state.FindTask(selection.UpstreamTask)
			if upstream == nil {
				upstreams[selection.UpstreamTask] = nil
				continue
			}
			children := make([]bool, len(upstream.Output))
			for i := range upstream.Output {
				children[i] = state.FindTask(perSubtaskChildID(upstream.ID, td.TaskSlugOrName(), i)) != nil
			}
			upstreams[upstream.ID] = map[string]any{
				"executed":  upstream.TransitionsExecuted[failure.Transition],
				"replanned": upstream.TransitionsExecuted["replanned"],
				"children":  children,
			}
		}
	}
	material := map[string]any{
		"version": handoffFailureVersion, "class": failure.Class, "index": failure.OutputIndex,
		"transition": td, "output": task.Output, "depends_on": deps,
		"resolved_target": resolved.targetRolePair, "target_status": resolved.targetStatus, "task_type": resolved.taskType,
		"upstreams": upstreams, "incumbents": relevantKinds,
	}
	encoded, err := json.Marshal(material)
	if err != nil {
		return ""
	}
	return lifecycleDigest(encoded)
}

// TransitionFailure returns a failure only while the latest observation for
// this pending transition matches current inputs. Unknown metadata fails open.
func (d PlanHandoffDomain) TransitionFailure(state *models.State, task *models.Task, transition string) *PlanHandoffFailure {
	if !d.Pending(task) || task.TransitionsExecuted[transition] || !d.GatesTransition(task, transition) {
		return nil
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventTransitionFailed || entry.Extra["transition"] != transition {
			continue
		}
		if index, present := entry.Extra["output_index"]; !present || index == nil {
			return nil
		}
		encoded, err := json.Marshal(entry.Extra)
		if err != nil {
			return nil
		}
		var failure PlanHandoffFailure
		if json.Unmarshal(encoded, &failure) != nil || failure.Version != handoffFailureVersion || failure.TaskID != task.ID || failure.Error == "" || !lifecycleDigestValid(failure.Fingerprint) {
			return nil
		}
		if current := d.failureFingerprint(state, task, failure); current != "" && current == failure.Fingerprint {
			return &failure
		}
		return nil
	}
	return nil
}

// Failures reports pending repair evidence, independently of wake eligibility.
func (d PlanHandoffDomain) Failures(state *models.State, task *models.Task) []PlanHandoffFailure {
	var failures []PlanHandoffFailure
	if task == nil {
		return nil
	}
	for _, name := range d.gatedByPair[task.RolePair] {
		if failure := d.TransitionFailure(state, task, name); failure != nil {
			failures = append(failures, *failure)
		}
	}
	return failures
}

func (d PlanHandoffDomain) HasFailedPlan(state *models.State) bool {
	for _, id := range state.Sprint.Scope.Planned {
		if len(d.Failures(state, state.FindTask(id))) != 0 {
			return true
		}
	}
	return false
}

// recordFailure must run in the same lock and state as the refused attempt.
func (d PlanHandoffDomain) recordFailure(state *models.State, task *models.Task, transition string, refusal *handoffInputError, now time.Time) *PlanHandoffFailure {
	failure := PlanHandoffFailure{Version: handoffFailureVersion, TaskID: task.ID, Transition: transition, Class: refusal.class, OutputIndex: refusal.index, Error: secretmask.New().MaskText(refusal.Error())}
	failure.Fingerprint = d.failureFingerprint(state, task, failure)
	if failure.Fingerprint == "" {
		return nil
	}
	if existing := d.TransitionFailure(state, task, transition); existing != nil && existing.Fingerprint == failure.Fingerprint {
		return nil
	}
	encoded, _ := json.Marshal(failure)
	var extra map[string]any
	_ = json.Unmarshal(encoded, &extra)
	task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventTransitionFailed, Extra: extra})
	return &failure
}

func writeHandoffFailureAlert(projectRoot string, failure PlanHandoffFailure, now time.Time) error {
	return alerts.Write(paths.New(projectRoot).AlertsLogPath(), alerts.Alert{
		Timestamp: now, Level: alerts.AlertLevelWarning, Category: "PLAN HANDOFF FAILED",
		OnceKey: "PLAN HANDOFF FAILED|" + failure.TaskID + "|" + failure.Transition + "|" + failure.Fingerprint,
		Message: fmt.Sprintf("task %s transition %s: %s; repair inputs and retry %s, or retire its unused handoff with %s", failure.TaskID, failure.Transition, failure.Error,
			brand.Command("resume"), brand.Command("plan-check", failure.TaskID, "--replaced-by", "<merged-correction>")),
	})
}
