// Package models mirrors the persisted entities the guard recognizes by name.
package models

// State is the persisted state.
type State struct{ Mode string }

// Task is a persisted task.
type Task struct{ ID string }

// Agent is a persisted agent.
type Agent struct{ ID string }
