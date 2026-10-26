package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

// newToolResultCmd builds the explicit agent-visible result boundary. It does
// not filter unrelated CLI output or provider-owned tool notifications.
func newToolResultCmd() *cobra.Command {
	var root string
	var config toolresult.Config
	group := &cobra.Command{Use: "tool-result", Short: "Budget tool results and retrieve sanitized artifacts"}
	group.PersistentFlags().StringVar(&root, "root", "", "Artifact directory (default: project state directory/tool-results)")
	group.PersistentFlags().IntVar(&config.ThresholdBytes, "threshold-bytes", toolResultDefaultThresholdBytes, "Externalize results larger than this many bytes")
	group.PersistentFlags().IntVar(&config.DigestBytes, "digest-bytes", toolResultDefaultDigestBytes, "Maximum externalized result digest bytes")
	store := toolResultStoreResolver(&root, &config)
	group.AddCommand(newToolResultFilterCmd(store), newToolResultReadCmd(store), newToolResultStatsCmd(store), newToolResultRunCmd(store), newToolResultACPCmd(store), newToolResultCodexHookCmd(store))
	group.AddCommand(newToolResultClaudeCmd(store))
	return group
}

func newToolResultFilterCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	var input toolresult.Result
	var jsonInput bool
	var exitCode int
	filter := &cobra.Command{Use: "filter", Short: "Read a complete tool result from stdin", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := input
			if jsonInput {
				decoded, err := decodeToolResultJSON(cmd.InOrStdin())
				if err != nil {
					return err
				}
				result = decoded
			} else {
				content, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return err
				}
				result.Content = string(content)
				if cmd.Flags().Changed("exit-code") {
					result.ExitCode = &exitCode
				}
			}
			s, err := store(cmd)
			if err != nil {
				return err
			}
			output, err := s.Process(result)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), output)
			return err
		}}
	filter.Flags().BoolVar(&jsonInput, "json-input", false, "Read a JSON result envelope instead of plain text")
	filter.Flags().StringVar(&input.Tool, "tool", "", "Tool name")
	filter.Flags().StringVar(&input.Command, "command", "", "Command (use JSON stdin for sensitive metadata)")
	filter.Flags().StringVar(&input.Role, "role", "", "Agent role")
	filter.Flags().StringVar(&input.CommandClass, "command-class", "", "Command class")
	filter.Flags().StringVar(&input.TaskID, "task-id", "", "Task identifier")
	filter.Flags().StringVar(&input.AgentID, "agent-id", "", "Agent identifier")
	filter.Flags().StringVar(&input.SessionID, "session-id", "", "Session identifier")
	filter.Flags().IntVar(&exitCode, "exit-code", 0, "Original tool exit code (filter success still exits zero)")
	filter.Flags().BoolVar(&input.Truncated, "truncated", false, "Source result was already truncated")
	return filter
}

// decodeToolResultJSON reads exactly one result envelope. Decoder diagnostics
// can echo attacker-controlled object keys, so failures use fixed messages.
func decodeToolResultJSON(in io.Reader) (toolresult.Result, error) {
	decoder := json.NewDecoder(in)
	decoder.DisallowUnknownFields()
	var decoded *toolresult.Result
	if err := decoder.Decode(&decoded); err != nil {
		return toolresult.Result{}, fmt.Errorf("invalid tool result JSON object")
	}
	if decoded == nil {
		return toolresult.Result{}, fmt.Errorf("expected a tool result JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return toolresult.Result{}, fmt.Errorf("expected exactly one tool result JSON object")
	}
	return *decoded, nil
}

func newToolResultReadCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	var offset, limit int64
	read := &cobra.Command{Use: "read HASH", Short: "Read a bounded byte range from a sanitized artifact", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			readLimit := limit
			if readLimit == 0 {
				readLimit = int64(s.ThresholdBytes())
			}
			content, err := s.Read(args[0], offset, readLimit)
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), content)
			return err
		}}
	read.Flags().Int64Var(&offset, "offset", 0, "Starting byte offset")
	read.Flags().Int64Var(&limit, "limit", 0, "Maximum bytes to read (defaults to the active threshold; 1..65536)")
	return read
}

func newToolResultStatsCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	return &cobra.Command{Use: "stats", Short: "Report persisted tool-result budget telemetry as JSON", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			value, err := s.Stats()
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(value)
		}}
}

func init() { rootCmd.AddCommand(newToolResultCmd()) }
