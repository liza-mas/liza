package toolresult

import (
	"fmt"
	"strings"
)

// ClaudeHookInput is the provider-owned hook envelope. Unknown provider fields
// deliberately remain accepted: the transformer preserves the original output
// object rather than attempting to own Claude's evolving tool schema.
type ClaudeHookInput struct {
	ToolCalls []ClaudeHookInput `json:"tool_calls"`
	Event     string            `json:"hook_event_name"`
	Tool      string            `json:"tool_name"`
	Input     map[string]any    `json:"tool_input"`
	Response  any               `json:"tool_response"`
	SessionID string            `json:"session_id"`
	CWD       string            `json:"cwd"`
	Error     string            `json:"error"`
	ToolUseID string            `json:"tool_use_id"`
}

// ClaudePostToolResult rewrites supported textual native outputs using Claude
// Code's PostToolUse updatedToolOutput contract. Returning an empty object is an
// intentional no-op. Callers must stop the session on error, never emit raw data.
// Claude Bash is never rewritten: native permission matching must see the
// original command (ADR-0178), so its output is not engine-controlled.
func ClaudePostToolResult(store *Store, hook ClaudeHookInput, metadata Result) (map[string]any, error) {
	empty := map[string]any{}
	if hook.Event == "PostToolBatch" {
		return claudeBatchGuard(store, hook, metadata)
	}
	// Claude invokes PostToolUseFailure before PostToolBatch, but does not
	// apply its stop response to failed MCP output. PostToolBatch is the one
	// provider boundary that stops before a further model sample, so it owns
	// persistence and telemetry for failures to avoid double-counting.
	if hook.Event == "PostToolUseFailure" {
		return empty, nil
	}
	if hook.Event != "PostToolUse" {
		return empty, nil
	}
	if strings.HasPrefix(hook.Tool, "mcp__") {
		return claudeMCPPostToolResult(store, hook, metadata)
	}
	response, ok := hook.Response.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid native Claude response shape")
	}
	supported, err := claudeNativeSupported(hook.Tool, response)
	if err != nil {
		return nil, err
	}
	if !supported {
		return empty, nil
	}
	metadata, err = claudeNativeMetadata(metadata, hook, response)
	if err != nil {
		return nil, err
	}
	processed, err := store.Process(metadata)
	if err != nil {
		return nil, err
	}
	if processed == metadata.Content {
		return empty, nil
	}
	replacement, err := claudeNativeReplacement(store, hook.Tool, metadata.Content, processed)
	if err != nil {
		return nil, err
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "updatedToolOutput": replacement}}, nil
}
