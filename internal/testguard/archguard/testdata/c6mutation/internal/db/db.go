// Package db mirrors the Blackboard method set the guard classifies.
package db

import (
	"time"

	"c6fixture/internal/models"
)

// Blackboard is the state handle.
type Blackboard struct{ path string }

// For returns the handle of a state path.
func For(path string) *Blackboard { return &Blackboard{path: path} }

// Modify mutates the state.
func (bb *Blackboard) Modify(fn func(*models.State) error) error { return fn(&models.State{}) }

// Write replaces the state.
func (bb *Blackboard) Write(state *models.State) error { return nil }

// UpdateTask mutates one task.
func (bb *Blackboard) UpdateTask(id string, fn func(*models.Task) error) error {
	return fn(&models.Task{ID: id})
}

// UpdateAgent mutates one agent.
func (bb *Blackboard) UpdateAgent(id string, fn func(*models.Agent) error) error {
	return fn(&models.Agent{ID: id})
}

// ArchiveTerminalTask publishes an archive object and rewrites the task.
func (bb *Blackboard) ArchiveTerminalTask(task *models.Task, at time.Time) (int, error) {
	return 0, nil
}

// ReadSnapshot reads the state.
func (bb *Blackboard) ReadSnapshot() (*models.State, error) { return &models.State{}, nil }

// Patient returns a handle with another acquisition policy.
func (bb *Blackboard) Patient() *Blackboard { return bb }

// GetStatePath returns the path.
func (bb *Blackboard) GetStatePath() string { return bb.path }

// WriteLivenessRecord publishes an ADR-0177 side record, not canonical state.
func WriteLivenessRecord(path, agent string) error { return nil }

// Inside the boundary every use is legal.
func mutateInside(bb *Blackboard) error {
	return bb.Modify(func(*models.State) error { return nil })
}

var _ = mutateInside
