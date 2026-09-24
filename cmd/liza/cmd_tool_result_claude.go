package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

func newToolResultClaudeCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	return &cobra.Command{Use: "claude-hook", Short: "Apply native Claude pre/post tool-result hooks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		var output map[string]any
		if hook.Event == "PreToolUse" && hook.Tool == "Bash" {
			command, ok := hook.Input["command"].(string)
			if !ok {
				return stopped()
			}
			binary, err := os.Executable()
			if err != nil {
				return stopped()
			}
			root, _ := cmd.Flags().GetString("root")
			if root == "" {
				root = filepath.Join(hook.CWD, paths.ProjectDirName(), "tool-results")
			}
			collector := []string{binary, "tool-result", "--root", root}
			for _, flag := range []string{"threshold-bytes", "digest-bytes"} {
				if cmd.Flags().Changed(flag) {
					value, _ := cmd.Flags().GetInt(flag)
					collector = append(collector, "--"+flag, strconv.Itoa(value))
				}
			}
			collector = append(collector, "claude-capture")
			wrapped, err := toolresult.ClaudeShellCommand(command, collector, metadata)
			if err != nil {
				output = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": err.Error()}}
			} else {
				hook.Input["command"] = wrapped
				output = map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "updatedInput": hook.Input}}
			}
		} else {
			output, err = toolresult.ClaudePostToolResult(s, hook, metadata)
			if err != nil {
				return stopped()
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(output)
	}}
}

func newToolResultClaudeCaptureCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	return &cobra.Command{Use: "claude-capture DIRECTORY", Short: "Read a private native-shell capture FIFO", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		dir := args[0]
		defer toolresult.CleanupClaudeCapture(dir)
		fail := func() error {
			_, _ = io.WriteString(cmd.OutOrStdout(), "Tool-result boundary failed: captured output withheld.\n")
			return fmt.Errorf("claude output capture failed")
		}
		input, err := os.Open(filepath.Join(dir, "output"))
		if err != nil {
			return fail()
		}
		defer input.Close()
		result, err := toolresult.ReadClaudeCapture(input, filepath.Join(dir, "status"))
		if err != nil {
			return fail()
		}
		s, err := store(cmd)
		if err != nil {
			return fail()
		}
		output, err := s.Process(result)
		if err != nil {
			return fail()
		}
		_, err = io.WriteString(cmd.OutOrStdout(), output)
		if err != nil {
			return err
		}
		if closer, ok := cmd.OutOrStdout().(io.Closer); ok {
			_ = closer.Close()
		}
		if closer, ok := cmd.ErrOrStderr().(io.Closer); ok {
			_ = closer.Close()
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), []byte("0"), 0600); err != nil {
			return fmt.Errorf("claude capture acknowledgement failed")
		}
		toolresult.DrainClaudeBackground(dir, input)
		return nil
	}}
}
