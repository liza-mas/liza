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

const handoffUndecidedVersion = 1

// PlanHandoffUndecided is a persisted observation that a PLANNING_COMPLETE
// turn left a plan without a disposition (D-49). While its fingerprint matches,
// the plan stays outstanding but does not wake PLANNING_COMPLETE: an identical
// turn would decide nothing either. New input re-admits it for one turn.
type PlanHandoffUndecided struct {
	Version     int    `json:"version" yaml:"version"`
	TaskID      string `json:"task_id" yaml:"task_id"`
	Class       string `json:"class" yaml:"class"`
	Blocker     string `json:"blocker,omitempty" yaml:"blocker,omitempty"`
	Fingerprint string `json:"fingerprint" yaml:"fingerprint"`
}

// undecidedFingerprint digests every input that can bring the orchestrator a
// new disposition: the hand-off class and blocker (which carry upstream
// changes), the plan's disposition, output and dependencies, its other history,
// and the operator notes addressed to it. Unrelated work is excluded. An empty
// result means the plan cannot be observed and fails open.
func (d PlanHandoffDomain) undecidedFingerprint(state *models.State, task *models.Task) string {
	if d.resolver == nil {
		return ""
	}
	class, blocker := d.Classify(state, task)
	if class != PlanHandoffNeedsReview && class != PlanHandoffNeedsReconciliation && class != PlanHandoffAmendmentReady {
		return ""
	}
	deps, _, err := canonicalizeConcreteDependencyList(state, d.resolver, task.ID, task.RolePair, task.DependsOn)
	if err != nil {
		return ""
	}
	deps = slices.Clone(deps)
	slices.Sort(deps)
	deps = slices.Compact(deps)
	if len(deps) == 0 {
		deps = nil
	}
	history := 0
	for i := range task.History {
		if task.History[i].Event != models.TaskEventPlanHandoffUndecided {
			history++
		}
	}
	var notes []time.Time
	for i := range state.HumanNotes {
		if note := &state.HumanNotes[i]; note.For == task.ID || note.For == "all" {
			notes = append(notes, note.Timestamp.UTC())
		}
	}
	inputs := map[string]any{
		"version": handoffUndecidedVersion, "class": class, "blocker": blocker,
		"plan_check": task.PlanCheck, "output": task.Output, "depends_on": deps,
		"history": history, "notes": notes,
	}
	if task.PlanAmendment != nil {
		inputs["plan_amendment"] = task.PlanAmendment
		inputs["pending_correction"] = d.ReadyPlanCorrection(state, task)
	}
	encoded, err := json.Marshal(inputs)
	if err != nil {
		return ""
	}
	return lifecycleDigest(encoded)
}

// UndecidedHandoff returns the plan's latest undecided observation while it
// matches current inputs. Malformed or unknown observations fail open.
func (d PlanHandoffDomain) UndecidedHandoff(state *models.State, task *models.Task) *PlanHandoffUndecided {
	if task == nil || !d.Pending(task) || !d.InDomain(task) && d.ReadyPlanCorrection(state, task) == nil {
		return nil
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != models.TaskEventPlanHandoffUndecided {
			continue
		}
		encoded, err := json.Marshal(entry.Extra)
		if err != nil {
			return nil
		}
		var observation PlanHandoffUndecided
		if json.Unmarshal(encoded, &observation) != nil || observation.Version != handoffUndecidedVersion ||
			observation.TaskID != task.ID || !lifecycleDigestValid(observation.Fingerprint) {
			return nil
		}
		if current := d.undecidedFingerprint(state, task); current != "" && current == observation.Fingerprint {
			return &observation
		}
		return nil
	}
	return nil
}

// HasUndecidedPlan reports whether a planned task is waiting, unchanged, for
// a disposition. Like a held plan, it keeps the sprint open: its children do
// not exist yet.
func (d PlanHandoffDomain) HasUndecidedPlan(state *models.State) bool {
	for _, id := range state.Sprint.Scope.Planned {
		if d.UndecidedHandoff(state, state.FindTask(id)) != nil {
			return true
		}
	}
	return false
}

// HasStalledHandoff reports outstanding hand-off work that no wake will
// resume until its inputs change: a failed or undecided plan.
func (d PlanHandoffDomain) HasStalledHandoff(state *models.State) bool {
	return d.HasFailedPlan(state) || d.HasUndecidedPlan(state)
}

// RecordUndecided appends an undecided observation for a plan that a
// PLANNING_COMPLETE turn, woken on before, left without a disposition. It must
// run under the state lock on current. Nothing is recorded when the plan's
// inputs changed during the turn — the turn never saw them, so the plan keeps
// its fresh eligibility — or when a matching observation already exists.
func (d PlanHandoffDomain) RecordUndecided(before, current *models.State, taskID string, now time.Time) *PlanHandoffUndecided {
	wake, task := before.FindTask(taskID), current.FindTask(taskID)
	if wake == nil || task == nil {
		return nil
	}
	fingerprint := d.undecidedFingerprint(current, task)
	if fingerprint == "" || fingerprint != d.undecidedFingerprint(before, wake) || d.UndecidedHandoff(current, task) != nil {
		return nil
	}
	class, blocker := d.Classify(current, task)
	observation := PlanHandoffUndecided{
		Version: handoffUndecidedVersion, TaskID: task.ID, Class: string(class),
		Blocker: secretmask.New().MaskText(blocker), Fingerprint: fingerprint,
	}
	encoded, _ := json.Marshal(observation)
	var extra map[string]any
	_ = json.Unmarshal(encoded, &extra)
	task.History = append(task.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventPlanHandoffUndecided, Extra: extra})
	return &observation
}

// pendingGatedTransition names the plan's first reviewed hand-off not yet run.
func (d PlanHandoffDomain) pendingGatedTransition(task *models.Task) string {
	for _, name := range d.gatedByPair[task.RolePair] {
		if !task.TransitionsExecuted[name] {
			return name
		}
	}
	return "<transition>"
}

// WriteUndecidedAlert routes an undecided plan to the operator once per
// observation identity.
func (d PlanHandoffDomain) WriteUndecidedAlert(projectRoot string, task *models.Task, observation PlanHandoffUndecided, now time.Time) error {
	reason := observation.Class
	if observation.Blocker != "" {
		reason += ": " + observation.Blocker
	}
	return alerts.Write(paths.New(projectRoot).AlertsLogPath(), alerts.Alert{
		Timestamp: now, Level: alerts.AlertLevelWarning, Category: "PLAN DISPOSITION MISSING",
		OnceKey: "PLAN DISPOSITION MISSING|" + observation.TaskID + "|" + observation.Fingerprint,
		Message: fmt.Sprintf("orchestrator left plan %s undecided (%s); PLANNING_COMPLETE will not wake for it again until its inputs change. Ask for a pass, hold or replan with %s, or expand it yourself with %s",
			observation.TaskID, reason,
			brand.Command("add-human-note", observation.TaskID, "--note-file", "<path>"),
			brand.Command("proceed", observation.TaskID, d.pendingGatedTransition(task))),
	})
}
