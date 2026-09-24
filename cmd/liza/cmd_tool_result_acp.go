package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/toolresult"
	"github.com/liza-mas/liza/internal/toolresultacp"
	"github.com/spf13/cobra"
)

func newToolResultACPCmd(store func(*cobra.Command) (*toolresult.Store, error)) *cobra.Command {
	var metadata toolresult.Result
	cmd := &cobra.Command{Use: "acp-proxy [flags] -- EXECUTABLE [ARGS...]", Short: "Budget native ACP host responses before agent context", Args: cobra.MinimumNArgs(1), SilenceErrors: true, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store(cmd)
			if err != nil {
				return err
			}
			input := metadata
			if !cmd.Flags().Changed("task-id") {
				input.TaskID = brand.LookupEnv(os.Getenv, "TASK_ID").Value
			}
			if !cmd.Flags().Changed("agent-id") {
				input.AgentID = brand.LookupEnv(os.Getenv, "AGENT_ID").Value
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return toolresultacp.Run(ctx, toolresultacp.Config{Command: args, Store: s, Metadata: input, In: cmd.InOrStdin(), Out: cmd.OutOrStdout(), Err: cmd.ErrOrStderr()})
		}}
	cmd.Flags().StringVar(&metadata.TaskID, "task-id", "", "Task identifier")
	cmd.Flags().StringVar(&metadata.AgentID, "agent-id", "", "Agent identifier")
	cmd.Flags().StringVar(&metadata.SessionID, "session-id", "", "Engine session identifier (native session retained when available)")
	cmd.Flags().StringVar(&metadata.Role, "role", "", "Agent role")
	return cmd
}
