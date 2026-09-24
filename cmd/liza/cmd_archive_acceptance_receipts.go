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

type archiveAcceptanceReceiptsResult struct {
	Archived []string `json:"archived"`
	Bytes    int      `json:"bytes"`
	Batches  int      `json:"batches"`
}

var archiveAcceptanceReceiptsCmd = &cobra.Command{
	Use:   "archive-acceptance-receipts",
	Short: "Move terminal tasks' acceptance receipts out of live state into the archive",
	Long: `Operator maintenance that shrinks live state. Each terminal task's acceptance
receipt moves into an immutable, content-addressed archive object, and the task
keeps a digest reference; inspection restores the receipt from the archive.
Works in bounded batches, releasing the state lock between them, until no
eligible receipt remains. Merges already archive after each new merge; use this
to drain an existing backlog.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return ops.WrapLifecycleError("archive-acceptance-receipts", nil, fmt.Errorf("archive-acceptance-receipts is an operator-only command; agent sessions archive receipts only through their authenticated merge"), models.LifecycleForbidden, "stop", "none")
		}
		root, err := requireProjectRoot()
		if err != nil {
			return err
		}
		limit := ops.DefaultArchiveLimit
		limit.MaxTasks, _ = cmd.Flags().GetInt("max-tasks")
		limit.MaxBytes, _ = cmd.Flags().GetInt("max-bytes")
		if limit.MaxTasks < 1 || limit.MaxBytes < 1 {
			return fmt.Errorf("--max-tasks and --max-bytes must be positive")
		}
		var total archiveAcceptanceReceiptsResult
		for {
			batch, err := ops.ArchiveTerminalAcceptanceReceipts(root, nil, limit)
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
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, total, nil, nil)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Archived %d acceptance receipts in %d batches (%d bytes)\n", len(total.Archived), total.Batches, total.Bytes)
		return nil
	},
}

func init() {
	archiveAcceptanceReceiptsCmd.Flags().Int("max-tasks", ops.DefaultArchiveLimit.MaxTasks, "maximum tasks archived per state transaction")
	archiveAcceptanceReceiptsCmd.Flags().Int("max-bytes", ops.DefaultArchiveLimit.MaxBytes, "soft byte budget of archive objects per state transaction")
	addJSONFlag(archiveAcceptanceReceiptsCmd)
	rootCmd.AddCommand(archiveAcceptanceReceiptsCmd)
}
