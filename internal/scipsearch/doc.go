// Package scipsearch owns Liza's scip-search setup and runtime activation
// contracts.
//
// Init callers use ResolveInitConfig to validate external tools and persist a
// Config.ScipSearch language allowlist. Runtime lifecycle callers use
// RuntimeEnabled, PlanRuntimeCommands, RefreshIndexes, and AvailableIndexes to
// combine that allowlist with LIZA_ENABLE_SCIP_SEARCH, detect target-root
// languages from git-tracked files, execute fixed indexer command plans, and
// expose only existing successful index paths for later prompt guidance. The
// runtime contract does not validate tools or render prompts.
package scipsearch
