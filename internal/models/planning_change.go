package models

import (
	"sort"
	"strings"
)

const (
	PlanningChangeCorrection = "correction"
	PlanningChangeReplan     = "replan"
)

// PlanningChange records commissioning intent, independent of task names or
// free-form reasons. Created, on its task, is the measurement timestamp.
type PlanningChange struct {
	Kind           string `yaml:"kind" json:"kind"`
	Trigger        string `yaml:"trigger" json:"trigger"`
	OriginalTaskID string `yaml:"original_task_id" json:"original_task_id"`
}

func NewPlanningChange(kind, trigger, originalID string) *PlanningChange {
	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		trigger = "unknown"
	}
	return &PlanningChange{Kind: kind, Trigger: trigger, OriginalTaskID: originalID}
}

type PlanningChangeDay struct {
	Date         string `yaml:"date" json:"date"`
	Kind         string `yaml:"kind" json:"kind"`
	Trigger      string `yaml:"trigger" json:"trigger"`
	Architecture bool   `yaml:"architecture" json:"architecture"`
	Count        int    `yaml:"count" json:"count"`
}

type PlanningChangeMetrics struct {
	Daily              []PlanningChangeDay `yaml:"daily" json:"daily"`
	UnknownAttribution int                 `yaml:"unknown_attribution" json:"unknown_attribution"`
	MissingCreated     int                 `yaml:"missing_created" json:"missing_created"`
}

// ComputePlanningChanges counts each logical commissioned task once. Applying
// a correction, retrying review, and history length do not add occurrences.
// Historical lineage establishes kind only; it never invents a trigger.
func (s *State) ComputePlanningChanges() PlanningChangeMetrics {
	result := PlanningChangeMetrics{Daily: []PlanningChangeDay{}}
	if s == nil {
		return result
	}
	counts := make(map[PlanningChangeDay]int)
	for i := range s.Tasks {
		task := &s.Tasks[i]
		change := task.PlanningChange
		if change == nil && task.AmendsPlan != "" {
			change = NewPlanningChange(PlanningChangeCorrection, "", task.AmendsPlan)
		} else if change == nil && task.Supersedes != nil && *task.Supersedes != "" {
			original := s.FindTask(*task.Supersedes)
			if original != nil && original.TransitionsExecuted["replanned"] {
				change = NewPlanningChange(PlanningChangeReplan, "", original.ID)
			}
		}
		if change == nil {
			continue
		}
		trigger := strings.TrimSpace(change.Trigger)
		original := s.FindTask(change.OriginalTaskID)
		if trigger == "" || trigger == "unknown" || original == nil {
			result.UnknownAttribution++
		}
		if trigger == "" {
			trigger = "unknown"
		}
		if task.Created.IsZero() {
			result.MissingCreated++
			continue
		}
		key := PlanningChangeDay{Date: task.Created.UTC().Format("2006-01-02"),
			Kind: change.Kind, Trigger: trigger, Architecture: original != nil && original.Type == TaskTypeArchitecture}
		counts[key]++
	}
	for key, count := range counts {
		key.Count = count
		result.Daily = append(result.Daily, key)
	}
	sort.Slice(result.Daily, func(i, j int) bool {
		a, b := result.Daily[i], result.Daily[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Trigger != b.Trigger {
			return a.Trigger < b.Trigger
		}
		return !a.Architecture && b.Architecture
	})
	return result
}
