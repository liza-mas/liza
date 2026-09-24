package toolresult

import (
	"encoding/json"
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
// Bash must be captured before execution to cover failed and background output;
// the post hook deliberately does not double-count that managed return path.
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
	switch hook.Tool {
	case "Read":
		if response["type"] != "text" {
			return empty, nil
		}
		file, ok := response["file"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Claude Read output shape")
		}
		if _, ok := file["content"].(string); !ok {
			return nil, fmt.Errorf("invalid Claude Read content shape")
		}
	case "Grep", "Glob":
	default:
		return empty, nil
	}
	if response == nil {
		return nil, fmt.Errorf("missing Claude tool response")
	}
	content, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("invalid Claude response encoding")
	}
	command, err := json.Marshal(hook.Input)
	if err != nil {
		return nil, fmt.Errorf("invalid Claude input encoding")
	}
	metadata.Tool, metadata.Command, metadata.SessionID = hook.Tool, string(command), hook.SessionID
	metadata.Content, metadata.ContentJSON = string(content), true
	if value, ok := response["truncated"].(bool); ok {
		metadata.Truncated = value
	}
	number := func(v any) float64 {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		default:
			return 0
		}
	}
	if number(response["totalLines"]) > number(response["numLines"]) && number(response["numLines"]) > 0 {
		metadata.Truncated = true
	}
	if file, ok := response["file"].(map[string]any); ok && number(file["totalLines"]) > number(file["numLines"]) && number(file["numLines"]) > 0 {
		metadata.Truncated = true
	}
	processed, err := store.Process(metadata)
	if err != nil {
		return nil, err
	}
	if processed == string(content) {
		return empty, nil
	}
	var replacement map[string]any
	if err := json.Unmarshal([]byte(processed), &replacement); err != nil {
		return nil, fmt.Errorf("invalid processed Claude result")
	}
	// Small secret-bearing objects retain their exact native shape after masking.
	// Only externalized digests need embedding in the native textual display field.
	if _, externalized := replacement["artifact_id"]; externalized {
		replacement = nil
		if err := json.Unmarshal([]byte(store.sanitizeContent(string(content), true)), &replacement); err != nil {
			return nil, err
		}
		switch hook.Tool {
		case "Read":
			file := replacement["file"].(map[string]any)
			file["content"] = processed
			file["numLines"] = 1
			file["startLine"] = 1
		case "Grep":
			replacement["mode"], replacement["content"] = "content", processed
			replacement["filenames"], replacement["numFiles"] = []any{}, 0
			replacement["numLines"] = 1
		case "Glob":
			// Glob serializes only filenames. A single explicitly marked JSON digest
			// carries the artifact reference rather than inventing a real matching path.
			replacement["filenames"], replacement["numFiles"] = []any{processed}, 1
			replacement["truncated"] = true
		}
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "updatedToolOutput": replacement}}, nil
}
