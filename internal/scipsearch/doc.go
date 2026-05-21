// Package scipsearch owns Liza's scip-search setup and runtime activation
// contracts.
//
// Init callers use ResolveInitConfig to validate external tools and persist a
// Config.ScipSearch language allowlist. Runtime lifecycle callers use
// RuntimeEnabled to combine that allowlist with LIZA_ENABLE_SCIP_SEARCH before
// generating indexes or injecting prompt guidance. The runtime contract is
// read-only: it does not validate tools, inspect worktrees, generate indexes,
// or render prompts.
package scipsearch
