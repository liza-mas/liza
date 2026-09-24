package toolresult

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// CodexHook applies the native Codex synchronous hook contract. Shell output
// must be captured before execution: post hooks see truncated or final chunks.
// Hosted tools do not traverse this boundary.
func CodexHook(input []byte, runner string, store *Store, metadata ...Result) ([]byte, error) {
	var event struct {
		Event          string          `json:"hook_event_name"`
		Tool           string          `json:"tool_name"`
		PermissionMode string          `json:"permission_mode"`
		SessionID      string          `json:"session_id"`
		Input          json.RawMessage `json:"tool_input"`
		Response       json.RawMessage `json:"tool_response"`
	}
	if json.Unmarshal(input, &event) != nil || event.Tool == "" {
		return nil, fmt.Errorf("invalid Codex tool hook input")
	}
	var result Result
	if len(metadata) > 0 {
		result = metadata[0]
	}
	result.Tool = event.Tool
	result.Command = string(event.Input)
	result.SessionID = event.SessionID
	switch event.Event {
	case "PreToolUse":
		if event.Tool != "Bash" {
			return []byte("{}"), nil
		}
		// The rewrite is only sound for the permission mode the managed Codex
		// configuration renders: approval_policy="never" reports
		// "bypassPermissions", and the same config keeps sandbox_mode
		// "workspace-write". Any other mode means the session is not the
		// managed baseline — accepting it would either elevate a constrained
		// session or run outside the trusted posture.
		if event.PermissionMode != "bypassPermissions" {
			return codexDeny("The managed tool-result boundary requires the permission mode rendered by the managed Codex configuration.")
		}
		var args map[string]json.RawMessage
		var command string
		if json.Unmarshal(event.Input, &args) != nil || json.Unmarshal(args["command"], &command) != nil {
			return codexDeny("Invalid shell command for the managed tool-result boundary.")
		}
		if !filepath.IsAbs(runner) || store == nil {
			return codexDeny("The managed tool-result runner is unavailable.")
		}
		// $0 is expanded by the actual native shell, retaining an explicit shell
		// override as well as the cwd/environment initialized by its login startup.
		replacement := "exec " + codexQuote(runner) + " tool-result --root " + codexQuote(store.path) +
			" --threshold-bytes " + strconv.Itoa(store.config.ThresholdBytes) + " --digest-bytes " + strconv.Itoa(store.config.DigestBytes) +
			" run --tool Bash --command " + codexQuote(command) + " --session-id " + codexQuote(event.SessionID) +
			" --role " + codexQuote(result.Role) + " -- \"$0\" -c " + codexQuote(command)
		args["command"], _ = json.Marshal(replacement)
		return json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "allow", "updatedInput": args,
		}})
	case "PostToolUse":
		// Bash already passed through the full-output runner. Processing it again
		// would deduplicate the digest rather than the underlying result.
		if event.Tool == "Bash" {
			return []byte("{}"), nil
		}
		if store == nil || len(event.Response) == 0 {
			return nil, fmt.Errorf("missing Codex tool result")
		}
		content, isJSON := string(event.Response), true
		var text string
		if json.Unmarshal(event.Response, &text) == nil {
			content, isJSON = text, false
		}
		result.Content, result.ContentJSON = content, isJSON
		output, err := store.Process(result)
		if err != nil {
			return nil, err
		}
		if output == content {
			return []byte("{}"), nil
		}
		// A blocking decision also withholds the raw value from nested code-mode
		// promises. continue:false changes only model history, not that promise.
		return json.Marshal(map[string]any{"decision": "block", "reason": output})
	default:
		return nil, fmt.Errorf("unsupported Codex hook event")
	}
}

func codexDeny(reason string) ([]byte, error) {
	return json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
}

func codexQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
