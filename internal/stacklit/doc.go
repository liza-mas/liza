// Package stacklit owns the optional Stacklit runtime indexing contract.
//
// Runtime lifecycle callers use RuntimeEnabled, RefreshIndex, and
// AvailableIndexes to guard Stacklit indexes with the branded
// ENABLE_STACKLIT variable,
// copy the repo-root stacklit.json into a task worktree, and expose only existing
// absolute index paths for prompt guidance. stacklit.json may be tracked or
// ignored; task worktree provisioning rejects only the unsafe middle state where
// it is neither. The runtime does not create or mutate stacklit-insights.json or
// .stacklitrc.json; Stacklit consumes those committed operator-curated files when
// they exist in the target tree.
package stacklit
