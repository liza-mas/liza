package toolresult

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Claude passes successful MCP output as a content-block array, not the
// transport CallToolResult envelope (observed with Claude 2.1.267). Preserve
// non-text blocks verbatim; compact only textual blocks as one invocation.
func claudeMCPPostToolResult(store *Store, hook ClaudeHookInput, metadata Result) (map[string]any, error) {
	blocks, ok := hook.Response.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid Claude MCP content-block shape")
	}
	var texts []any
	for _, block := range blocks {
		value, ok := block.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Claude MCP block")
		}
		if value["type"] == "text" {
			texts = append(texts, value)
		}
	}
	if len(texts) == 0 {
		return map[string]any{}, nil
	}
	encoded, err := json.Marshal(texts)
	if err != nil {
		return nil, fmt.Errorf("invalid Claude MCP text encoding")
	}
	command, err := json.Marshal(hook.Input)
	if err != nil {
		return nil, err
	}
	metadata.Tool, metadata.Command, metadata.SessionID, metadata.Content = hook.Tool, string(command), hook.SessionID, string(encoded)
	metadata.ContentJSON = true
	processed, err := store.Process(metadata)
	if err != nil {
		return nil, err
	}
	if processed == string(encoded) {
		return map[string]any{}, nil
	}
	var textBlocks []any
	if err := json.Unmarshal([]byte(processed), &textBlocks); err != nil {
		textBlocks = []any{map[string]any{"type": "text", "text": processed}}
	}
	replacement := make([]any, 0, len(blocks))
	inserted := false
	for _, block := range blocks {
		if block.(map[string]any)["type"] == "text" {
			if !inserted {
				replacement = append(replacement, textBlocks...)
				inserted = true
			}
		} else {
			replacement = append(replacement, block)
		}
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "updatedToolOutput": replacement}}, nil
}

// Failed provider tools do not support output replacement. Externalize the
// available error and stop BEFORE another sampling request instead of leaking
// an oversized/redacted original. Small ordinary failures retain native flow.
func claudeFailureResult(store *Store, hook ClaudeHookInput, metadata Result) (map[string]any, error) {
	if hook.Error == "" {
		return map[string]any{}, nil
	}
	command, err := json.Marshal(hook.Input)
	if err != nil {
		return nil, err
	}
	metadata.Tool, metadata.Command, metadata.SessionID, metadata.Content = hook.Tool, string(command), hook.SessionID, hook.Error
	metadata.Truncated = strings.Contains(hook.Error, "characters truncated")
	processed, err := store.Process(metadata)
	if err != nil {
		return nil, err
	}
	if processed == hook.Error {
		return map[string]any{}, nil
	}
	return map[string]any{"continue": false, "stopReason": "Tool-result boundary stopped this session: provider failure output cannot be replaced safely. Sanitized error: " + processed}, nil
}

// Claude 2.1.267 ignores continue:false on PostToolUseFailure; PostToolBatch
// actually stops before sampling. Check only residual unfiltered native/MCP
// content, so successful already-compacted results do not generate duplicate
// telemetry. This is a fail-closed stop, not a failed-tool output rewrite.
// Batch entries carry no failure flag, and native entries arrive as longer
// model-facing text (line-numbered Read), so native byte budgets belong to
// PostToolUse: the size check applies to MCP only, sanitization to both.
func claudeBatchGuard(store *Store, hook ClaudeHookInput, metadata Result) (map[string]any, error) {
	for _, call := range hook.ToolCalls {
		native := call.Tool == "Read" || call.Tool == "Grep" || call.Tool == "Glob"
		if !native && !strings.HasPrefix(call.Tool, "mcp__") {
			continue
		}
		content, ok := call.Response.(string)
		if !ok {
			encoded, err := json.Marshal(call.Response)
			if err != nil {
				return nil, err
			}
			content = string(encoded)
		}
		oversized := !native && len(content) > store.config.ThresholdBytes
		if !oversized && store.sanitizeContent(content, !ok) == content {
			continue
		}
		call.Event, call.Error, call.SessionID = "PostToolUseFailure", content, hook.SessionID
		metadata.ContentJSON = !ok
		return claudeFailureResult(store, call, metadata)
	}
	return map[string]any{}, nil
}
