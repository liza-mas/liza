package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/spf13/cobra"
)

var writeCheckpointCmd = &cobra.Command{
	Use:   "write-checkpoint <task-id>",
	Short: "Write pre-execution checkpoint before submitting for review",
	Long: `Record implementation intent, validation plan, and scope before submission.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status (resolved from pipeline config)
  - Task must be assigned to the submitting agent

Updates:
  - Appends pre_execution_checkpoint event to task history
  - Does not change task status`,
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

		taskID := args[0]

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "write-checkpoint"); err != nil {
			return err
		}

		intent, _ := cmd.Flags().GetString("intent")
		validationPlan, _ := cmd.Flags().GetString("validation-plan")
		filesToModify, _ := cmd.Flags().GetStringSlice("files-to-modify")
		assumptions, _ := cmd.Flags().GetStringSlice("assumptions")
		risks, _ := cmd.Flags().GetString("risks")
		tddNotRequired, _ := cmd.Flags().GetString("tdd-not-required")
		impact, _ := cmd.Flags().GetString("impact")

		input := &ops.WriteCheckpointInput{
			TaskID:         taskID,
			AgentID:        authority.ID,
			Intent:         intent,
			ValidationPlan: validationPlan,
			FilesToModify:  filesToModify,
			Assumptions:    assumptions,
			Risks:          risks,
			TDDNotRequired: tddNotRequired,
			Impact:         impact,
		}

		// Parse scope-extensions from JSON if provided
		if scopeJSON, _ := cmd.Flags().GetString("scope-extensions"); scopeJSON != "" {
			var entries []ops.ScopeExtensionEntry
			if err := json.Unmarshal([]byte(scopeJSON), &entries); err != nil {
				return &ops.PreconditionError{Reason: fmt.Sprintf(
					"--scope-extensions must be a JSON array: %v", err)}
			}
			input.ScopeExtensions = entries
		}

		if isJSON(cmd) {
			err := ops.WriteCheckpointWithAuthority(projectRoot, input, authority)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.WriteCheckpointWithAuthorityCommand(projectRoot, input, authority)
	},
}

var setTaskOutputCmd = &cobra.Command{
	Use:   "set-task-output <task-id> --output <path>",
	Short: "Set output entries for downstream task generation",
	Long: `Define output entries that will become downstream tasks after merge.

Reads output entries from a JSON file. Each entry must have desc, done_when,
scope, and spec_ref. Optional fields: epic_ref, plan_ref, arch_ref, validation,
destructive_db, rca_required, depends_on, task_depends_on, provider_dependencies,
descendant_dependencies, decomposition.

depends_on contains sibling output indexes, e.g. "0" for output[0].
task_depends_on contains existing concrete task IDs to copy onto generated
child tasks.
provider_dependencies retains an explicit wait for selected provider outputs:
  [{"provider_task":"provider-architecture","transition":"architecture-to-code-plan","outputs":[0,2]}]
Names are configured examples, not fixed pipeline roles. The provider must
exist and the transition must be per-subtask from its role-pair. Select a
nonempty set of distinct nonnegative output indexes; children may be unborn.
Claim waits for the provider MERGED, its transition executed, and every selected
child MERGED with the expected parent/target role-pair. APPROVED is insufficient.
Bounds are checked once output is known; selected nonempty kind outputs are
refused because deduplication can change child identity. Only this typed field
permits explicit cross-stage prerequisites; ordinary downstream task_depends_on
remains invalid. Cycles through future child production/inheritance are refused.
Every scope task/code-plan precondition must agree with structured fields;
runtime does not parse prose. Legacy adoption requires reviewed reauthoring.
A wait that orders the generated plan's writers, not the plan itself, goes in
descendant_dependencies, applied at that plan's per-subtask transition:
  [{"at_transition":"code-plan-to-coding","provider_dependencies":[...]}]
The generated plan does not wait on it; every output it writes receives it in
provider_dependencies. One intermediate plan on a single per-subtask path
only; such outputs cannot set kind, and a provider named there cannot be
retired while the declaration is live. A provider wait that would hold a
generated plan until a provider's non-planning children merge is warned.
inherit_inputs declares whether this child waits for a whole upstream phase or
only for selected upstream outputs. Omitting it inherits every child of every
upstream dependency, which is the default. To narrow it:
  {"mode": "selected", "selections": [{"upstream_task": "plan-1", "outputs": [0, 2]}]}
outputs are indexes into that upstream task's own output[], not task IDs — the
upstream's children usually do not exist yet. upstream_task must be a
dependency of the producing task. An unresolvable or out-of-range selection
fails the transition rather than dropping the dependency. Use
{"mode": "all"} to record a deliberate whole-phase barrier.
An explicit output value overrides the parent task's rca_required value; an
omitted value inherits the parent default. A decomposition root whose configured
consumer is code planning must provide rca_required on every output entry, along
with its required artifact reference and typed decomposition metadata.

Requirements:
  - Agent ID must be provided (via --agent-id flag or ` + brand.EnvName("AGENT_ID") + ` env var)
  - Task must be in an executing status
  - Task must be assigned to the submitting agent
  - At least one output entry required

Updates:
  - Sets task.output to provided entries (overwrites existing, idempotent)

Example:
  cat > outputs.json <<'EOF'
  [
    {"desc": "Subtask 1", "done_when": "Tests pass", "scope": "internal/pkg"},
    {"desc": "Subtask 2", "done_when": "API works", "scope": "internal/api", "validation": ["make test"], "destructive_db": false, "depends_on": ["0"], "task_depends_on": ["existing-task-id"]}
  ]
  EOF
  ` + brand.Command("set-task-output", "task-1") + ` --output outputs.json`,
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

		invocation := beginLifecycleCLI(cmd, args)
		defer invocation.finish(&retErr)
		requestOpts, err := lifecycleRequestOptions(cmd)
		if err != nil {
			return err
		}

		taskID := args[0]

		authority, err := requireAgentAuthority(cmd)
		if err != nil {
			return err
		}

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		resolver, err := loadResolverForRBAC(projectRoot)
		if err != nil {
			return err
		}
		if err := validateAllowedOperation(resolver, authority.ID, "set-task-output"); err != nil {
			return err
		}

		outputFile, _ := cmd.Flags().GetString("output")
		if outputFile == "" {
			return cliValidationError("--output is required")
		}

		data, err := os.ReadFile(outputFile)
		if err != nil {
			return cliValidationWrap("reading output file", err)
		}

		var entries []models.OutputEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			hint := "output file must contain a JSON array [...]"
			if len(data) > 0 && data[0] == '{' {
				hint += "; got a JSON object — remove the wrapper and pass a bare array"
			}
			return &ops.PreconditionError{Reason: fmt.Sprintf("%s: %v", hint, err)}
		}

		// Preserve the file's null/[] distinction before the typed API
		// normalizes nil slices into explicit empty manifests.
		_, diagnostics, err := payloadschema.Validate(payloadschema.SetTaskOutputOperation, map[string]any{"output": json.RawMessage(data)})
		if err != nil {
			return err
		}
		if len(diagnostics) > 0 {
			return ops.NewLifecycleInvalidInputError(payloadschema.SetTaskOutputOperation, nil, diagnostics,
				&ops.PreconditionError{Reason: "output file must contain a valid task-output manifest array"})
		}

		input := &ops.SetTaskOutputInput{
			Request: requestOpts,
			TaskID:  taskID,
			AgentID: authority.ID,
			Output:  entries,
		}

		invocation.calledOps = true
		if isJSON(cmd) {
			result, err := ops.SetTaskOutputWithAuthorityAndOptions(projectRoot, input, authority)
			var warnings []string
			if result != nil {
				warnings = result.Warnings
			}
			return jsonout.WriteResult(os.Stdout, result, warnings, err)
		}
		return commands.SetTaskOutputWithAuthorityCommand(projectRoot, input, authority)
	},
}

var setDiscoveryDispositionCmd = &cobra.Command{
	Use:   "set-discovery-disposition <discovery-id> <disposition>",
	Short: "Set the disposition of a discovered item",
	Long: `Set how a discovered item should be handled.

Disposition values:
  - A task ID (e.g. "task-5"): converts the discovery into that task
  - "deferred": defer for later consideration
  - "dismissed": dismiss the discovery`,
	Args: cobra.ExactArgs(2),
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

		discoveryID := args[0]
		disposition := args[1]

		projectRoot, err := requireProjectRoot()
		if err != nil {
			return err
		}

		if isJSON(cmd) {
			err := ops.SetDiscoveryDisposition(projectRoot, discoveryID, disposition)
			return jsonout.WriteResult(os.Stdout, nil, nil, err)
		}
		return commands.SetDiscoveryDispositionCommand(projectRoot, discoveryID, disposition)
	},
}

func init() {
	addLifecycleFlags(setTaskOutputCmd)
	rootCmd.AddCommand(writeCheckpointCmd)
	rootCmd.AddCommand(setTaskOutputCmd)
	rootCmd.AddCommand(setDiscoveryDispositionCmd)

	writeCheckpointCmd.ValidArgsFunction = completeTaskIDArgs(1)
	setTaskOutputCmd.ValidArgsFunction = completeTaskIDArgs(1)

	addJSONFlag(writeCheckpointCmd)
	addJSONFlag(setTaskOutputCmd)
	addJSONFlag(setDiscoveryDispositionCmd)

	// Write-checkpoint command flags
	addAgentIDFlag(writeCheckpointCmd)
	writeCheckpointCmd.Flags().String("intent", "", "specific, observable intent of implementation (required)")
	writeCheckpointCmd.Flags().String("validation-plan", "", "concrete validation command and expected output (required)")
	writeCheckpointCmd.Flags().StringSlice("files-to-modify", nil, "files that will be modified (omit for read-only analysis tasks)")
	writeCheckpointCmd.Flags().StringSlice("assumptions", nil, "tagged assumptions")
	writeCheckpointCmd.Flags().String("risks", "", "identified risks")
	writeCheckpointCmd.Flags().String("tdd-not-required", "", "justification for skipping new test files")
	writeCheckpointCmd.Flags().String("impact", "", "impact classification (standard, significant, architecture)")
	writeCheckpointCmd.Flags().String("scope-extensions", "", `scope extensions as JSON array, e.g. [{"file":"path","justification":"why"}]`)
	writeCheckpointCmd.MarkFlagRequired("intent")
	writeCheckpointCmd.MarkFlagRequired("validation-plan")

	// Set-task-output command flags
	addAgentIDFlag(setTaskOutputCmd)
	setTaskOutputCmd.Flags().String("output", "", "path to JSON file with output entries array (required)")
}
