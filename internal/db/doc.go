// Package db implements the atomic YAML database layer with file locking.
// It provides safe concurrent access to state.yaml using flock-based mutual exclusion.
//
// Operator inspection and observation-only reads (TUI, watch, validation and
// usage reports, lifecycle metrics capture) use [Blackboard.ReadSnapshot]: an
// uncached, lock-free read of one atomically published file, decoded after the
// file is closed.
// A snapshot may be stale immediately; it does not authorize later mutations.
// [Blackboard.Read] retains its locking contract, and [Blackboard.Modify]
// revalidates and publishes within an exclusive lock. In-place external writes
// do not provide the atomic publication required by snapshot readers.
//
// # Instance Management
//
// Production code should use [For] to obtain a process-level singleton
// Blackboard for a given state path. This ensures all callers in the same
// process share cache state, preventing silent fragmentation if Blackboard
// gains in-process state (metrics, write batching, subscriptions) in the
// future.
//
// [New] creates an independent instance and is intended for tests that need
// isolation. Each test uses a unique temp directory, so [For] would also
// provide natural isolation, but [New] makes the independence explicit.
package db
