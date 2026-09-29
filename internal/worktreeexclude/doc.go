// Package worktreeexclude owns linked-worktree private exclude setup.
//
// Callers provide a target worktree root and relative ignore entries. The
// package resolves that worktree's private gitdir, ensures entries are present
// exactly once in its info/exclude file, and points worktree-specific
// core.excludesFile at that private file without taking over an existing
// conflicting excludesFile value.
//
// It also owns the repository-wide exclude: EnsureRepoExclude appends patterns
// to the shared info/exclude that every checkout of the repository reads,
// without changing Git config.
package worktreeexclude
