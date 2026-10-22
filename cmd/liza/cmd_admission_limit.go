package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/spf13/cobra"
)

var admissionLimitCmd = &cobra.Command{
	Use:   "admission-limit",
	Short: "Cap how many heavy canonical commands run at once",
	Long: fmt.Sprintf(`Manage admission limits: per resource class, how many canonical validation
commands may execute at once across canonical admission (submit-for-review,
update-review-commit, the orphaned-base heal) and the deferred-validation
runner. A command belongs to a class when its text contains any of the
class's --match substrings.

A command whose class is full waits in arrival order for a slot. The wait
does not count against its timeout_seconds budget. A submit waits up to the
engine cap of %[1]d s minus its budget, but at least %[3]g h; other bounded
callers keep wait plus budget within the cap. A submit waits before it
prepares or rebases anything: if the class stays full it is refused as
RETRYABLE, and the same submission is simply retried. The deferred runner
waits until a slot frees.

Read the current limits with %[2]s config.admission_limits.
Identified agent sessions cannot use this command.`, referencecontract.AcceptanceMaxTimeoutSeconds, brand.Command("get"), ops.SubmitSlotWaitFloor.Hours()),
}

var admissionLimitSetCmd = &cobra.Command{
	Use:   "set <class> --max-concurrent <n> --match <substring> [--match <substring>...]",
	Short: "Create or replace one admission limit",
	Long: fmt.Sprintf(`Store one admission limit. The class name is lowercase letters, digits and
dashes. An identical definition is a successful no-op; replacing a different
one requires --replace and --reason. A new --max-concurrent also reaches
commands already queued for the class within about 10 s; commands already
holding a slot keep it. Write a substring that starts with a dash as
--match=<substring>.

Example:
  %s db-suite --max-concurrent 2 --match=--decision-model-db`, brand.Command("admission-limit", "set")),
	Args: cobra.ExactArgs(1),
	RunE: admissionLimitRun(func(cmd *cobra.Command, args []string) (*ops.ConfigSetResult, error) {
		maxConcurrent, _ := cmd.Flags().GetInt("max-concurrent")
		match, _ := cmd.Flags().GetStringArray("match")
		replace, _ := cmd.Flags().GetBool("replace")
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return nil, err
		}
		return ops.SetAdmissionLimit(projectRoot, ops.SetAdmissionLimitInput{
			Limit:   models.AdmissionLimit{Class: args[0], MaxConcurrent: maxConcurrent, Match: match},
			Replace: replace, Reason: reason,
		})
	}),
}

var admissionLimitRemoveCmd = &cobra.Command{
	Use:   "remove <class> --reason <reason>",
	Short: "Remove one admission limit",
	Args:  cobra.ExactArgs(1),
	RunE: admissionLimitRun(func(cmd *cobra.Command, args []string) (*ops.ConfigSetResult, error) {
		reason, _ := cmd.Flags().GetString("reason")
		projectRoot, err := requireProjectRoot()
		if err != nil {
			return nil, err
		}
		return ops.RemoveAdmissionLimit(projectRoot, args[0], reason)
	}),
}

// admissionLimitRun refuses agent sessions and renders the result like config set.
func admissionLimitRun(run func(*cobra.Command, []string) (*ops.ConfigSetResult, error)) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (retErr error) {
		defer func() {
			if isJSON(cmd) && retErr != nil && !errors.Is(retErr, jsonout.ErrAlreadyWritten) {
				_ = jsonout.WriteResult(os.Stdout, nil, nil, retErr)
				retErr = jsonout.ErrAlreadyWritten
			}
		}()
		if brand.LookupEnv(os.Getenv, "AGENT_ID").Value != "" {
			return cliValidationError("admission-limit is operator-only; agent sessions cannot change admission limits")
		}
		result, err := run(cmd, args)
		if isJSON(cmd) {
			return jsonout.WriteResult(os.Stdout, result, nil, err)
		}
		if err != nil {
			return err
		}
		cmd.Printf("%s: %s\n", result.Key, result.Outcome)
		return nil
	}
}

func init() {
	admissionLimitSetCmd.Flags().Int("max-concurrent", 0, "How many commands of the class may execute at once")
	admissionLimitSetCmd.Flags().StringArray("match", nil, "Substring that puts a command in the class (repeatable)")
	admissionLimitSetCmd.Flags().Bool("replace", false, "Replace a different existing definition (requires --reason)")
	admissionLimitSetCmd.Flags().String("reason", "", "Reason for the change")
	_ = admissionLimitSetCmd.MarkFlagRequired("max-concurrent")
	_ = admissionLimitSetCmd.MarkFlagRequired("match")
	admissionLimitRemoveCmd.Flags().String("reason", "", "Reason for removing the limit")
	_ = admissionLimitRemoveCmd.MarkFlagRequired("reason")
	for _, cmd := range []*cobra.Command{admissionLimitSetCmd, admissionLimitRemoveCmd} {
		addJSONFlag(cmd)
		admissionLimitCmd.AddCommand(cmd)
	}
	rootCmd.AddCommand(admissionLimitCmd)
}
