package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/spf13/cobra"
)

var planCheckCmd = &cobra.Command{
	Use:   "plan-check <task-id> (--pass | --hold <ask> | --clear)",
	Short: "Record the orchestrator's disposition of a merged plan before its children exist",
	Long: `Record whether a merged plan may be expanded into child tasks.

Automatic paths (auto-resume and supervisor transition passes) expand a merged
plan's manual per-subtask or one-to-one hand-off only after the orchestrator
passed it. An operator resume or proceed authorizes it directly. Held plans are
expanded by no path until an operator clears the hold.

  --pass         orchestrator: the plan's declared checks can run; refused
                 while an upstream plan is replanned, held or unreviewed, and
                 on a held plan
  --hold <ask>   orchestrator: the plan needs a human action first (the ask);
                 raises an AWAITING HUMAN alert
  --clear        operator only: remove the disposition after the human action;
                 the plan returns to orchestrator review

A plan the planner must correct is replanned instead (replan --reason).`,
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
		if input.Action == ops.PlanCheckActionClear {
			if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
				return cliValidationError("plan-check --clear is operator-only; agent sessions cannot release a human hold")
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

	selected := 0
	for _, set := range []bool{pass, clear, holdSet} {
		if set {
			selected++
		}
	}
	if selected != 1 {
		return ops.PlanCheckInput{}, cliValidationError("exactly one of --pass, --hold <ask>, --clear is required")
	}
	input := ops.PlanCheckInput{TaskID: taskID}
	switch {
	case pass:
		input.Action = ops.PlanCheckActionPass
	case clear:
		input.Action = ops.PlanCheckActionClear
	default:
		if strings.TrimSpace(ask) == "" {
			return ops.PlanCheckInput{}, cliValidationError("--hold requires the human action being awaited")
		}
		input.Action = ops.PlanCheckActionHold
		input.Ask = ask
	}
	return input, nil
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
	planCheckCmd.Flags().String("hold", "", "orchestrator: hold the plan for the given human action")
	planCheckCmd.Flags().Bool("clear", false, "operator: remove the disposition after the human action")
	planCheckCmd.Flags().String("agent-id", "", "orchestrator agent ID (auto-resolved if not provided)")
}
