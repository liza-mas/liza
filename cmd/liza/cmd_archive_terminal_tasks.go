package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

type archiveTerminalTasksResult struct {
	Archived []string `json:"archived,omitempty"`
	Restored []string `json:"restored,omitempty"`
	Bytes    int      `json:"bytes"`
	Batches  int      `json:"batches"`
}

var archiveTerminalTasksCmd = &cobra.Command{
	Use:   "archive-terminal-tasks",
	Short: "Enable immutable terminal task archival and drain the backlog",
	Long: `Operator maintenance for complete terminal task records. Explicitly enables
terminal_task_archival and drains bounded batches, releasing the state lock
between them. Ordinary reads restore complete logical tasks from immutable
objects. Upgrade and restart all run processes together before enabling this
format; older binaries cannot read the physical terminal rows.

For an older-binary rollback, stop every run process and use --restore-inline
before downgrading. Restoration atomically disables archival and publishes the
full logical state inline; this one transaction costs the original full YAML
write. Immutable objects are retained for old snapshots.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return ops.WrapLifecycleError("archive-terminal-tasks", nil, fmt.Errorf("archive-terminal-tasks is an operator-only command; agent sessions archive terminal tasks only through authenticated merge maintenance"), models.LifecycleForbidden, "stop", "none")
		}
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		var total archiveTerminalTasksResult
		restore, _ := cmd.Flags().GetBool("restore-inline")
		if restore {
			if cmd.Flags().Changed("max-tasks") || cmd.Flags().Changed("max-bytes") {
				return fmt.Errorf("--restore-inline cannot be combined with batch limits")
			}
			total.Restored, err = ops.RestoreTerminalTasksInline(root)
			if err != nil {
				return err
			}
		} else {
			limit := ops.DefaultArchiveLimit
			limit.MaxTasks, _ = cmd.Flags().GetInt("max-tasks")
			limit.MaxBytes, _ = cmd.Flags().GetInt("max-bytes")
			if limit.MaxTasks < 1 || limit.MaxBytes < 1 {
				return fmt.Errorf("--max-tasks and --max-bytes must be positive")
			}
			for {
				batch, err := ops.EnableAndArchiveTerminalTasks(root, limit)
				if err != nil {
					return err
				}
				if len(batch.Archived) == 0 {
					break
				}
				total.Batches++
				total.Archived = append(total.Archived, batch.Archived...)
				total.Bytes += batch.Bytes
				if batch.Remaining == 0 {
					break
				}
			}
		}
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, total, nil, nil)
		}
		if restore {
			fmt.Fprintf(cmd.OutOrStdout(), "Restored %d terminal tasks inline; archival disabled\n", len(total.Restored))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Archived %d terminal tasks in %d batches (%d bytes)\n", len(total.Archived), total.Batches, total.Bytes)
		}
		return nil
	},
}

func init() {
	archiveTerminalTasksCmd.Flags().Int("max-tasks", ops.DefaultArchiveLimit.MaxTasks, "maximum tasks archived per transaction")
	archiveTerminalTasksCmd.Flags().Int("max-bytes", ops.DefaultArchiveLimit.MaxBytes, "soft byte budget of archive objects per transaction")
	archiveTerminalTasksCmd.Flags().Bool("restore-inline", false, "atomically restore all terminal tasks inline and disable archival before a stopped-run downgrade")
	addJSONFlag(archiveTerminalTasksCmd)
	rootCmd.AddCommand(archiveTerminalTasksCmd)
}
