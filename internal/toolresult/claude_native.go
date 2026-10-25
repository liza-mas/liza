package toolresult

import (
	"encoding/json"
	"fmt"
)

// claudeNativeSupported validates a native PostToolUse response for the tool
// and reports whether the transformer handles it. Unsupported tools and
// non-text Read results are a no-op, malformed supported shapes are refused.
func claudeNativeSupported(tool HookTool, response map[string]any) (bool, error) {
	switch tool {
	case HookToolRead:
		if response["type"] != "text" {
			return false, nil
		}
		file, ok := response["file"].(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid Claude Read output shape")
		}
		if _, ok := file["content"].(string); !ok {
			return false, fmt.Errorf("invalid Claude Read content shape")
		}
	case HookToolGrep, HookToolGlob:
	default:
		return false, nil
	}
	if response == nil {
		return false, fmt.Errorf("missing Claude tool response")
	}
	return true, nil
}

// claudeNativeMetadata completes the store metadata from the hook: the encoded
// response becomes the content and the encoded input the command. The content
// is encoded first, so its refusal wins over the input's.
func claudeNativeMetadata(metadata Result, hook ClaudeHookInput, response map[string]any) (Result, error) {
	content, err := json.Marshal(response)
	if err != nil {
		return Result{}, fmt.Errorf("invalid Claude response encoding")
	}
	command, err := json.Marshal(hook.Input)
	if err != nil {
		return Result{}, fmt.Errorf("invalid Claude input encoding")
	}
	metadata.Tool, metadata.Command, metadata.SessionID = string(hook.Tool), string(command), hook.SessionID
	metadata.Content, metadata.ContentJSON = string(content), true
	if value, ok := response["truncated"].(bool); ok {
		metadata.Truncated = value
	}
	file, _ := response["file"].(map[string]any)
	if claudeLinesTruncated(response) || claudeLinesTruncated(file) {
		metadata.Truncated = true
	}
	return metadata, nil
}

// claudeLinesTruncated reports a shown-lines count below the total. Only
// float64 and int counts are read; any other type counts as zero.
func claudeLinesTruncated(m map[string]any) bool {
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
	return number(m["totalLines"]) > number(m["numLines"]) && number(m["numLines"]) > 0
}

// claudeNativeReplacement shapes the updatedToolOutput for processed store
// output that differs from the original content.
func claudeNativeReplacement(store *Store, tool HookTool, content, processed string) (map[string]any, error) {
	var replacement map[string]any
	if err := json.Unmarshal([]byte(processed), &replacement); err != nil {
		return nil, fmt.Errorf("invalid processed Claude result")
	}
	// Small secret-bearing objects retain their exact native shape after masking.
	// Only externalized digests need embedding in the native textual display field.
	if _, externalized := replacement["artifact_id"]; !externalized {
		return replacement, nil
	}
	replacement = nil
	if err := json.Unmarshal([]byte(store.sanitizeContent(content, true)), &replacement); err != nil {
		return nil, err
	}
	switch tool {
	case HookToolRead:
		file := replacement["file"].(map[string]any)
		file["content"] = processed
		file["numLines"] = 1
		file["startLine"] = 1
	case HookToolGrep:
		replacement["mode"], replacement["content"] = "content", processed
		replacement["filenames"], replacement["numFiles"] = []any{}, 0
		replacement["numLines"] = 1
	case HookToolGlob:
		// Glob serializes only filenames. A single explicitly marked JSON digest
		// carries the artifact reference rather than inventing a real matching path.
		replacement["filenames"], replacement["numFiles"] = []any{processed}, 1
		replacement["truncated"] = true
	}
	return replacement, nil
}
