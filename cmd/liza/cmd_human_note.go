package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/spf13/cobra"
)

var addHumanNoteCmd = &cobra.Command{
	Use:   "add-human-note <task-id|all> --note-file <path>",
	Short: "Record bounded operator input for an existing task or all tasks",
	Long: `Append an operator note without changing task status, clearing blocks,
or granting approvals. A note newer than an assessment makes its blocked target
actionable for the orchestrator, and any note also wakes an idle orchestrator
once (HUMAN_NOTE): the turn renders the note verbatim and marks it seen on
success. Input must be a nonempty UTF-8 file of at most
4096 bytes; reference larger evidence by path. Note content is not echoed.
Identified agent sessions cannot use this command. As with local operator
recovery, absence of an agent identity is not authentication of a human author.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return cliValidationError("add-human-note is operator-only; agent sessions cannot author operator input")
		}
		notePath, _ := cmd.Flags().GetString("note-file")
		file, err := os.Open(notePath)
		if err != nil {
			return cliValidationWrap("opening --note-file", err)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, statehygiene.MaxStateTextBytes+1))
		if err != nil {
			return cliValidationWrap("reading --note-file", err)
		}
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		result, err := ops.AddHumanNote(projectRoot, args[0], string(data))
		if isJSON(cmd) {
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Operator note recorded for %s (%d bytes).\n", result.Target, result.Bytes)
		for _, warning := range result.Warnings {
			fmt.Fprintln(cmd.ErrOrStderr(), warning)
		}
		return nil
	},
}

func init() {
	addHumanNoteCmd.Flags().String("note-file", "", "UTF-8 file containing bounded operator input")
	_ = addHumanNoteCmd.MarkFlagRequired("note-file")
	addJSONFlag(addHumanNoteCmd)
	rootCmd.AddCommand(addHumanNoteCmd)
}
