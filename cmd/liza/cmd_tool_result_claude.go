package main

import (
	"encoding/json"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

func newToolResultClaudeCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	return &cobra.Command{Use: "claude-hook", Short: "Apply native Claude post tool-result hooks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		stopped := func() error {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"continue": false, "stopReason": "Tool-result boundary failed; stop rather than forwarding unfiltered output."})
		}
		var hook toolresult.ClaudeHookInput
		if err := json.NewDecoder(cmd.InOrStdin()).Decode(&hook); err != nil {
			return stopped()
		}
		s, err := store(cmd)
		if err != nil {
			return stopped()
		}
		metadata := toolresult.Result{TaskID: brand.LookupEnv(os.Getenv, "TASK_ID").Value, AgentID: brand.LookupEnv(os.Getenv, "AGENT_ID").Value, Role: brand.LookupEnv(os.Getenv, "AGENT_ROLE").Value, SessionID: hook.SessionID}
		output, err := toolresult.ClaudePostToolResult(s, hook, metadata)
		if err != nil {
			return stopped()
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
	}}
}
