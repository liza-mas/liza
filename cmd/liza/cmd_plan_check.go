package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var planCheckCmd = &cobra.Command{
	Use:   "plan-check <task-id> (--pass | --hold <ask> | --clear | --replaced-by <merged-correction>)",
	Short: "Record the orchestrator's disposition of a merged plan before its children exist",
	Long: `Record whether a merged plan may be expanded into child tasks.

Automatic paths (auto-resume and supervisor transition passes) expand a merged
plan's manual per-subtask or one-to-one hand-off only after the orchestrator
passed it. An operator resume or proceed authorizes it directly. Held plans are
expanded by no path until an operator clears the hold.

  --pass         orchestrator: the plan's declared checks can run; refused
                 while an upstream plan is replanned, held or unreviewed, and
                 on a held plan
  --notes-file <file>
                 pass only: JSON [{"output_index":0,"message":"guidance"}];
                 advisory child validation guidance, never contract changes;
                 at most 4096 bytes per message and 16384 bytes per file
  --hold <ask>   orchestrator: the plan needs a human action first (the ask);
                 raises an AWAITING HUMAN alert
  --clear        operator only: remove the disposition after the human action;
                 the plan returns to orchestrator review
  --replaced-by <merged-correction>
                 operator only: retire an unused hand-off by an existing
                 merged correction; preserves status and ordinary dependencies

Material corrections require reviewed amend-plan work or replan --reason.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (retErr error) {
		if isJSON(cmd) {
			log.SetOutput(io.Discard)
			defer log.SetOutput(os.Stderr)
			defer func() {
				if retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
					_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
					retErr = jsonout.ErrAlreadyWritten
				}
			}()
		}

		input, err := planCheckInputFromFlags(cmd, args[0])
		if err != nil {
			return err
		}
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}
		if input.Action == ops.PlanCheckActionClear || input.Action == ops.PlanCheckActionReplace {
			if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
				return cliValidationError("plan-check --clear/--replaced-by is operator-only; agent sessions cannot release a hold or retire a hand-off")
			}
			input.ChangedBy = resolveChangedBy(cmd)
		} else {
			authority, err := resolveOrchestratorAuthority(cmd)
			if err != nil {
				return err
			}
			resolver, err := loadResolverForRBAC(projectRoot)
			if err != nil {
				return err
			}
			if err := validateRoleType(resolver, authority.ID, "orchestrator"); err != nil {
				return err
			}
			input.Authority = &authority
		}

		result, err := ops.RecordPlanCheck(projectRoot, input)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, resultWarnings(result), err)
		}
		if err != nil {
			return err
		}
		printPlanCheckResult(result)
		return nil
	},
}

func planCheckInputFromFlags(cmd *cobra.Command, taskID string) (ops.PlanCheckInput, error) {
	pass, _ := cmd.Flags().GetBool("pass")
	clear, _ := cmd.Flags().GetBool("clear")
	holdSet := cmd.Flags().Changed("hold")
	ask, _ := cmd.Flags().GetString("hold")
	replacementSet := cmd.Flags().Changed("replaced-by")
	replacement, _ := cmd.Flags().GetString("replaced-by")

	selected := 0
	for _, set := range []bool{pass, clear, holdSet, replacementSet} {
		if set {
			selected++
		}
	}
	if selected != 1 {
		return ops.PlanCheckInput{}, cliValidationError("exactly one of --pass, --hold <ask>, --clear, --replaced-by <merged-correction> is required")
	}
	input := ops.PlanCheckInput{TaskID: taskID}
	switch {
	case pass:
		input.Action = ops.PlanCheckActionPass
	case clear:
		input.Action = ops.PlanCheckActionClear
	case replacementSet:
		if strings.TrimSpace(replacement) == "" {
			return ops.PlanCheckInput{}, cliValidationError("--replaced-by requires a merged correction task ID")
		}
		input.Action = ops.PlanCheckActionReplace
		input.ReplacedBy = strings.TrimSpace(replacement)
	default:
		if strings.TrimSpace(ask) == "" {
			return ops.PlanCheckInput{}, cliValidationError("--hold requires the human action being awaited")
		}
		input.Action = ops.PlanCheckActionHold
		input.Ask = ask
	}
	if cmd.Flags().Changed("notes-file") {
		if !pass {
			return ops.PlanCheckInput{}, cliValidationError("--notes-file requires --pass")
		}
		path, _ := cmd.Flags().GetString("notes-file")
		var err error
		input.Notes, err = readPlanValidationNotes(path)
		if err != nil {
			return ops.PlanCheckInput{}, err
		}
	}
	return input, nil
}

func readPlanValidationNotes(path string) ([]models.PlanValidationNote, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, cliValidationWrap("opening --notes-file", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, models.MaxValidationNotesBytes+1))
	if err != nil {
		return nil, cliValidationWrap("reading --notes-file", err)
	}
	if len(data) > models.MaxValidationNotesBytes || !utf8.Valid(data) {
		return nil, cliValidationError("--notes-file must be UTF-8 and at most 16384 bytes")
	}
	var records []struct {
		OutputIndex *int    `json:"output_index"`
		Message     *string `json:"message"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&records); err != nil {
		return nil, cliValidationWrap("decoding --notes-file", err)
	}
	if len(records) == 0 {
		return nil, cliValidationError("--notes-file requires a nonempty JSON array")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, cliValidationError("--notes-file must contain exactly one JSON array")
	}
	notes := make([]models.PlanValidationNote, len(records))
	for i, record := range records {
		if record.OutputIndex == nil || record.Message == nil {
			return nil, cliValidationError("each validation note requires output_index and message")
		}
		notes[i] = models.PlanValidationNote{OutputIndex: *record.OutputIndex, Message: *record.Message}
	}
	return notes, nil
}

func printPlanCheckResult(result *ops.PlanCheckResult) {
	verdict := string(result.Verdict)
	if verdict == "" {
		verdict = "none"
	}
	state := "unchanged"
	if result.Changed {
		state = "recorded"
	}
	fmt.Printf("Plan check %s for %s: verdict %s, class %s\n", state, result.TaskID, verdict, result.Class)
	if result.Blocker != "" {
		fmt.Printf("  Blocker: %s\n", result.Blocker)
	}
	if result.ReplacedBy != "" {
		fmt.Printf("  Replaced by: %s\n", result.ReplacedBy)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

func init() {
	rootCmd.AddCommand(planCheckCmd)
	planCheckCmd.ValidArgsFunction = completeTaskIDArgs(1)
	addJSONFlag(planCheckCmd)
	addChangedByFlag(planCheckCmd)
	planCheckCmd.Flags().Bool("pass", false, "orchestrator: admit the plan's hand-off")
	planCheckCmd.Flags().String("notes-file", "", "pass only: bounded JSON advisory notes selected by output_index")
	planCheckCmd.Flags().String("hold", "", "orchestrator: hold the plan for the given human action")
	planCheckCmd.Flags().Bool("clear", false, "operator: remove the disposition after the human action")
	planCheckCmd.Flags().String("replaced-by", "", "operator: retire an unused hand-off by a merged correction")
	planCheckCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
}
