package models

import (
	"fmt"
)

// State represents the complete Liza state.yaml structure
type State struct {
	Version             int                                       `yaml:"version"`
	PipelineVersion     int                                       `yaml:"pipeline_version,omitempty"`
	Goal                Goal                                      `yaml:"goal"`
	Tasks               []Task                                    `yaml:"tasks"`
	Agents              map[string]Agent                          `yaml:"agents"`
	AgentHealth         map[string]AgentHealth                    `yaml:"agent_health,omitempty"`
	ValidationReadiness map[string]map[string]ValidationReadiness `yaml:"validation_readiness,omitempty"`
	Discovered          []Discovery                               `yaml:"discovered"`
	HumanNotes          []HumanNote                               `yaml:"human_notes"`
	SpecChanges         []SpecChange                              `yaml:"spec_changes"`
	Anomalies           []Anomaly                                 `yaml:"anomalies"`
	QuarantinedVerdicts []QuarantinedVerdict                      `yaml:"quarantined_verdicts,omitempty"`
	ProofReaffirmations []ProofReaffirmation                      `yaml:"proof_reaffirmations,omitempty"`
	Sprint              Sprint                                    `yaml:"sprint"`
	SprintHistory       []SprintSummary                           `yaml:"sprint_history,omitempty"`
	CircuitBreaker      CircuitBreaker                            `yaml:"circuit_breaker"`
	Config              Config                                    `yaml:"config"`
	Extra               map[string]any                            `yaml:",inline"`
}

// FindTask returns a pointer to the task with the given ID, or nil if not found.
// The returned pointer refers to the element within s.Tasks, so mutations are
// reflected in the state (useful inside Blackboard.Modify closures).
func (s *State) FindTask(taskID string) *Task {
	for i := range s.Tasks {
		if s.Tasks[i].ID == taskID {
			return &s.Tasks[i]
		}
	}
	return nil
}

// FindTaskIndex returns the index of the task with the given ID, or -1 if not found.
// Use when you need to remove a task from the slice.
func (s *State) FindTaskIndex(taskID string) int {
	for i := range s.Tasks {
		if s.Tasks[i].ID == taskID {
			return i
		}
	}
	return -1
}

// FindOrchestratorID returns the agent ID of the registered orchestrator.
// Returns an error if zero or more than one orchestrator is registered,
// since map iteration order is nondeterministic.
//
// Lease expiry is intentionally not checked here. This function answers
// "who is the orchestrator?" not "is the orchestrator alive?". The
// registration guard in agent.registerAgent prevents two live orchestrators;
// stale agents should be cleaned up via delete-agent.
func (s *State) FindOrchestratorID() (string, error) {
	var found string
	for id, agent := range s.Agents {
		if agent.Role == "orchestrator" {
			if found != "" {
				return "", fmt.Errorf("multiple orchestrators registered (%s, %s); pass --agent-id explicitly", found, id)
			}
			found = id
		}
	}
	if found == "" {
		return "", fmt.Errorf("no orchestrator agent registered; pass --agent-id explicitly")
	}
	return found, nil
}
