package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/spf13/cobra"
)

// toolResultExit preserves the wrapped command's exit status without adding an
// engine error banner to evidence that already represents the command failure.
type toolResultExit struct{ code int }

func (e *toolResultExit) Error() string { return "wrapped tool command exited unsuccessfully" }

func newToolResultRunCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	var result toolresult.Result
	cmd := &cobra.Command{Use: "run [flags] -- EXECUTABLE [ARGS...]", Short: "Capture a command before native truncation or streaming", Args: cobra.MinimumNArgs(1), SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			input := result
			if !cmd.Flags().Changed("task-id") {
				input.TaskID = brand.LookupEnv(os.Getenv, "TASK_ID").Value
			}
			if !cmd.Flags().Changed("agent-id") {
				input.AgentID = brand.LookupEnv(os.Getenv, "AGENT_ID").Value
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			code, err := toolresult.Run(ctx, s, input, args, cmd.InOrStdin(), cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if code != 0 {
				return &toolResultExit{code: code}
			}
			return nil
		}}
	cmd.Flags().StringVar(&result.Tool, "tool", "exec", "Tool name")
	cmd.Flags().StringVar(&result.Command, "command", "", "Original command identity")
	cmd.Flags().StringVar(&result.Role, "role", "", "Agent role")
	cmd.Flags().StringVar(&result.CommandClass, "command-class", "", "Command class")
	cmd.Flags().StringVar(&result.TaskID, "task-id", "", "Task identifier")
	cmd.Flags().StringVar(&result.AgentID, "agent-id", "", "Agent identifier")
	cmd.Flags().StringVar(&result.SessionID, "session-id", "", "Session identifier")
	return cmd
}
