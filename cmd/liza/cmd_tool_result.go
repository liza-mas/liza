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

// newToolResultCmd builds the explicit agent-visible result boundary. It does
// not filter unrelated CLI output or provider-owned tool notifications.
func newToolResultCmd() *cobra.Command {
	var root string
	var config toolresult.Config
	group := &cobra.Command{Use: "tool-result", Short: "Budget tool results and retrieve sanitized artifacts"}
	group.PersistentFlags().StringVar(&root, "root", "", "Artifact directory (default: project state directory/tool-results)")
	group.PersistentFlags().IntVar(&config.ThresholdBytes, "threshold-bytes", 32768, "Externalize results larger than this many bytes")
	group.PersistentFlags().IntVar(&config.DigestBytes, "digest-bytes", 4096, "Maximum externalized result digest bytes")
	store := func(cmd *cobra.Command) (*toolresult.Store, error) {
		resolved := config
		for _, option := range []struct {
			flag, suffix string
			value        *int
		}{
			{"threshold-bytes", "TOOL_RESULT_THRESHOLD_BYTES", &resolved.ThresholdBytes},
			{"digest-bytes", "TOOL_RESULT_DIGEST_BYTES", &resolved.DigestBytes},
		} {
			if cmd.Flags().Changed(option.flag) {
				continue
			}
			if raw := brand.LookupEnv(os.Getenv, option.suffix).Value; raw != "" {
				number, err := strconv.Atoi(raw)
				if err != nil {
					return nil, fmt.Errorf("invalid %s budget setting", option.flag)
				}
				*option.value = number
			}
		}
		// The OpenCode boundary has the same documented 64 KiB safety ceiling.
		if resolved.ThresholdBytes < 1024 || resolved.ThresholdBytes > 65536 || resolved.DigestBytes < 1024 {
			return nil, fmt.Errorf("budget settings require 1024 <= digest-bytes <= threshold-bytes <= 65536")
		}
		dir := root
		if dir == "" {
			project, _ := cmd.Flags().GetString("project-root")
			if project == "" {
				var err error
				project, err = os.Getwd()
				if err != nil {
					return nil, err
				}
			}
			dir = filepath.Join(project, paths.ProjectDirName(), "tool-results")
		}
		return toolresult.New(dir, resolved, nil)
	}
	var input toolresult.Result
	var jsonInput bool
	var exitCode int
	filter := &cobra.Command{Use: "filter", Short: "Read a complete tool result from stdin", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result := input
			if jsonInput {
				decoder := json.NewDecoder(cmd.InOrStdin())
				decoder.DisallowUnknownFields()
				var decoded *toolresult.Result
				if err := decoder.Decode(&decoded); err != nil {
					// Decoder diagnostics can echo attacker-controlled object keys.
					return fmt.Errorf("invalid tool result JSON object")
				}
				if decoded == nil {
					return fmt.Errorf("expected a tool result JSON object")
				}
				result = *decoded
				var extra any
				if err := decoder.Decode(&extra); err != io.EOF {
					return fmt.Errorf("expected exactly one tool result JSON object")
				}
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
	stats := &cobra.Command{Use: "stats", Short: "Report persisted tool-result budget telemetry as JSON", Args: cobra.NoArgs,
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
	group.AddCommand(filter, read, stats, newToolResultRunCmd(store), newToolResultACPCmd(store), newToolResultCodexHookCmd(store))
	group.AddCommand(newToolResultClaudeCmd(store), newToolResultClaudeCaptureCmd(store))
	return group
}

func init() { rootCmd.AddCommand(newToolResultCmd()) }
