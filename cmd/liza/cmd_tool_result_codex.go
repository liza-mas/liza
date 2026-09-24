package main

import (
	"encoding/json"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

// newToolResultCodexHookCmd always returns a valid controlling hook response
// on processing failure; a nonzero hook process exit could fail open in Codex.
func newToolResultCodexHookCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	return &cobra.Command{Use: "codex-hook", Short: "Apply the managed Codex tool-result boundary", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			const maxHookBytes = 128 << 20
			input, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), maxHookBytes+1))
			var output []byte
			if err == nil && len(input) <= maxHookBytes {
				var s *toolresult.Store
				var runner string
				s, err = store(cmd)
				if err == nil {
					runner, err = os.Executable()
				}
				if err == nil {
					output, err = toolresult.CodexHook(input, runner, s, toolresult.Result{TaskID: brand.LookupEnv(os.Getenv, "TASK_ID").Value, AgentID: brand.LookupEnv(os.Getenv, "AGENT_ID").Value, Role: brand.LookupEnv(os.Getenv, "AGENT_ROLE").Value})
				}
			}
			if err != nil || len(output) == 0 {
				output = codexHookFailure(input)
			}
			_, err = cmd.OutOrStdout().Write(output)
			return err
		},
	}
}

func codexHookFailure(input []byte) []byte {
	const reason = "Tool-result processing failed; the original output is withheld. Check the managed artifact store and runner configuration."
	var event struct {
		Event string `json:"hook_event_name"`
	}
	_ = json.Unmarshal(input, &event)
	var output []byte
	if event.Event == "PreToolUse" {
		output, _ = json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}})
	} else {
		output, _ = json.Marshal(map[string]any{"decision": "block", "reason": reason})
	}
	return output
}
