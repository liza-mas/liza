// Package scipsearch owns Liza's scip-search setup and runtime activation
// contracts.
//
// Init callers use ResolveInitConfig to validate external tools and persist a
// Config.ScipSearch language allowlist. Runtime lifecycle callers use
// RuntimeEnabled and PlanRuntimeCommands to combine that allowlist with
// LIZA_ENABLE_SCIP_SEARCH, detect target-root languages from git-tracked files,
// and build fixed indexer command plans before later execution or prompt
// guidance. The runtime planning contract does not validate tools, execute
// indexers, write index files, or render prompts.
package scipsearch
