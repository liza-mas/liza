package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/runtimeinputs"
)

// runtimeInputsStrictReason refuses runtime inputs on anything but a strict
// acceptance task: only its submit gate runs the canonical commands that
// consume them (ADR-0169).
const runtimeInputsStrictReason = "runtime_inputs require a strict acceptance contract: a coding task allocated by an independently reviewed planning output"

// runtimeInputCarrier is one declaration set under admission: a task being
// created or replaced, or one output entry of a planning submission.
type runtimeInputCarrier struct {
	// field is the JSON-pointer prefix diagnostics are attributed under,
	// "" for a task and "/output/<i>" for an output entry.
	field         string
	inputs        []models.RuntimeInput
	prerequisites []models.ValidationPrerequisite
}

// runtimeInputCarriersOfOutput lists a planning task's output entries.
func runtimeInputCarriersOfOutput(output []models.OutputEntry) []runtimeInputCarrier {
	carriers := make([]runtimeInputCarrier, len(output))
	for i, entry := range output {
		carriers[i] = runtimeInputCarrier{field: fmt.Sprintf("/output/%d", i), inputs: entry.RuntimeInputs, prerequisites: entry.ValidationPrerequisites}
	}
	return carriers
}

// checkRuntimeInputDeclarations applies the admission rules that need state
// or Git: every recipe resolves in the configured registry at the integration
// commit, and runtime-input variable names stay disjoint from session
// prerequisite names, across tasks and within the candidate (ADR-0169). owners
// are the tasks the carriers belong to or replace, excluded from the
// state-side comparison because their declarations are being replaced. Git runs here,
// so callers invoke it outside the state lock. A nil error with diagnostics is
// a refusal; an error means the check could not run.
func checkRuntimeInputDeclarations(root string, state *models.State, owners []string, carriers []runtimeInputCarrier) ([]models.FieldDiagnostic, error) {
	declares := false
	prerequisiteNames := false
	for _, carrier := range carriers {
		declares = declares || len(carrier.inputs) > 0
		prerequisiteNames = prerequisiteNames || len(carrier.prerequisites) > 0
	}
	if !declares && !prerequisiteNames {
		return nil, nil
	}
	var diagnostics []models.FieldDiagnostic
	if declares {
		recipeDiagnostics, err := checkRuntimeInputRecipes(root, state, carriers)
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, recipeDiagnostics...)
	}
	return append(diagnostics, runtimeInputNameCollisions(state, owners, carriers)...), nil
}

func checkRuntimeInputRecipes(root string, state *models.State, carriers []runtimeInputCarrier) ([]models.FieldDiagnostic, error) {
	registryPath := state.Config.RuntimeInputRegistry
	var registry *runtimeinputs.Registry
	registryProblem := ""
	if registryPath == "" {
		registryProblem = "no runtime-input registry is configured (config.runtime_input_registry)"
	} else {
		g := git.New(root)
		commit, err := g.GetCommitSHA(state.Config.IntegrationBranch)
		if err != nil {
			return nil, fmt.Errorf("resolve integration commit for the runtime-input registry: %w", err)
		}
		entry, present, err := g.TreeEntryAt(commit, registryPath)
		if err != nil {
			return nil, fmt.Errorf("inspect the runtime-input registry at integration: %w", err)
		}
		switch {
		case !present:
			registryProblem = "the runtime-input registry is missing from integration"
		case entry.Mode != "100644" && entry.Mode != "100755":
			registryProblem = "the runtime-input registry must be a regular committed file"
		default:
			data, err := g.ReadBlob(commit, registryPath)
			if err != nil {
				return nil, fmt.Errorf("read the runtime-input registry at integration: %w", err)
			}
			parsed, err := runtimeinputs.ParseRegistry([]byte(data))
			if err != nil {
				registryProblem = err.Error()
			}
			registry = parsed
		}
	}
	var diagnostics []models.FieldDiagnostic
	for _, carrier := range carriers {
		for i, input := range carrier.inputs {
			field := fmt.Sprintf("%s/runtime_inputs/%d/recipe", carrier.field, i)
			switch {
			case registryProblem != "":
				diagnostics = append(diagnostics, runtimeInputDiagnostic(field, registryProblem, models.FieldValueClassConflict))
			case !registry.Has(input.Recipe):
				diagnostics = append(diagnostics, runtimeInputDiagnostic(field, "recipe is not defined in the runtime-input registry at integration; land the registry entry before declaring it", models.FieldValueClassUnknownEnum))
			}
		}
	}
	return diagnostics, nil
}

// runtimeInputNameCollisions keeps the two variable classes disjoint:
// a runtime-input name is scrubbed from every session, so no task may also
// require it as a session prerequisite.
func runtimeInputNameCollisions(state *models.State, owners []string, carriers []runtimeInputCarrier) []models.FieldDiagnostic {
	runtimeOwners := map[string]string{}
	prerequisiteOwners := map[string]string{}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if slices.Contains(owners, task.ID) {
			continue
		}
		for _, name := range models.RuntimeInputEnvNames(task.RuntimeInputs) {
			runtimeOwners[name] = task.ID
		}
		for _, entry := range task.Output {
			for _, name := range models.RuntimeInputEnvNames(entry.RuntimeInputs) {
				runtimeOwners[name] = task.ID
			}
		}
		if !task.Status.IsTerminal() {
			for _, prerequisite := range task.ValidationPrerequisites {
				for _, name := range prerequisite.Env {
					prerequisiteOwners[name] = task.ID
				}
			}
		}
		// Output entries that can still generate children carry their
		// prerequisites into sessions later.
		if models.RuntimeInputOutputPending(task) {
			for _, entry := range task.Output {
				for _, prerequisite := range entry.ValidationPrerequisites {
					for _, name := range prerequisite.Env {
						prerequisiteOwners[name] = task.ID
					}
				}
			}
		}
	}
	for _, instance := range state.RuntimeInputs {
		for _, name := range instance.Names {
			if _, known := runtimeOwners[name]; !known {
				runtimeOwners[name] = "the runtime-input ledger"
			}
		}
	}
	// Names the candidate itself declares, in either class.
	candidateRuntime := map[string]bool{}
	candidatePrerequisite := map[string]bool{}
	for _, carrier := range carriers {
		for _, name := range models.RuntimeInputEnvNames(carrier.inputs) {
			candidateRuntime[name] = true
		}
		for _, prerequisite := range carrier.prerequisites {
			for _, name := range prerequisite.Env {
				candidatePrerequisite[name] = true
			}
		}
	}
	var diagnostics []models.FieldDiagnostic
	for _, carrier := range carriers {
		for i, input := range carrier.inputs {
			for j, name := range input.Env {
				field := fmt.Sprintf("%s/runtime_inputs/%d/env/%d", carrier.field, i, j)
				if other, taken := prerequisiteOwners[name]; taken {
					diagnostics = append(diagnostics, runtimeInputDiagnostic(field, fmt.Sprintf("variable %s is a validation prerequisite of task %s; runtime-input names are scrubbed from sessions and cannot also be session prerequisites", name, other), models.FieldValueClassConflict))
				} else if candidatePrerequisite[name] {
					diagnostics = append(diagnostics, runtimeInputDiagnostic(field, fmt.Sprintf("variable %s is also declared as a validation prerequisite; runtime-input names cannot be session prerequisites", name), models.FieldValueClassConflict))
				}
			}
		}
		for i, prerequisite := range carrier.prerequisites {
			for j, name := range prerequisite.Env {
				field := fmt.Sprintf("%s/validation_prerequisites/%d/env/%d", carrier.field, i, j)
				if other, taken := runtimeOwners[name]; taken && !candidateRuntime[name] {
					diagnostics = append(diagnostics, runtimeInputDiagnostic(field, fmt.Sprintf("variable %s is a runtime input of %s and is scrubbed from sessions; deliver it as a runtime input instead", name, runtimeOwnerLabel(other)), models.FieldValueClassConflict))
				}
			}
		}
	}
	return diagnostics
}

func runtimeOwnerLabel(owner string) string {
	if owner == "the runtime-input ledger" {
		return owner
	}
	return "task " + owner
}

func runtimeInputDiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{SchemaVersion: 1, Field: field, Constraint: constraint, ValueClass: valueClass, SafeAction: models.FieldDiagnosticCorrectInput}
}

// runtimeInputConsumerDiagnostics refuses output runtime inputs unless the
// producer is a planning task, the only parent that can allocate a strict
// acceptance contract (parentAllocatesTask), and every pair consuming its
// output generates coding children: only a strict coding task's gate runs the
// commands that consume them, so any other child could never be claimed
// (ADR-0169).
func runtimeInputConsumerDiagnostics(resolver *pipeline.Resolver, producer *models.Task, output []models.OutputEntry) []models.FieldDiagnostic {
	var declaring []int
	for i, entry := range output {
		if len(entry.RuntimeInputs) > 0 {
			declaring = append(declaring, i)
		}
	}
	if len(declaring) == 0 {
		return nil
	}
	consumers, err := resolver.OutputConsumerRolePairsForOutput(producer.RolePair, output)
	coding := (producer.EffectiveType() == models.TaskTypePlanning ||
		(producer.EffectiveType() == models.TaskTypeArchitecture && models.DirectCodingAllocation(output))) && err == nil && len(consumers) > 0
	for _, consumer := range consumers {
		pair, pairErr := resolver.RolePair(consumer)
		coding = coding && pairErr == nil && models.TaskTypeForRole(pair.Doer) == models.TaskTypeCoding
	}
	if coding {
		return nil
	}
	diagnostics := make([]models.FieldDiagnostic, len(declaring))
	for i, index := range declaring {
		diagnostics[i] = runtimeInputDiagnostic(fmt.Sprintf("/output/%d/runtime_inputs", index),
			"runtime_inputs are allowed only on a planning task's output that generates coding tasks: "+runtimeInputsStrictReason, models.FieldValueClassConflict)
	}
	return diagnostics
}

// checkOutputRuntimeInputs runs, inside the state transaction that persists an
// output, the admission rules that need no Git: consumers that can adopt a
// strict contract, and names disjoint from session prerequisites. The
// transaction is where the output's names first enter the deny set.
func checkOutputRuntimeInputs(state *models.State, resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry) error {
	diagnostics := runtimeInputConsumerDiagnostics(resolver, task, output)
	diagnostics = append(diagnostics, runtimeInputNameCollisions(state, []string{task.ID}, runtimeInputCarriersOfOutput(output))...)
	return runtimeInputAdmissionError("set-task-output", task, diagnostics, nil)
}

// runtimeInputAdmissionError reports refusals as field-attributed
// INVALID_INPUT, and a check that could not run as retryable.
func runtimeInputAdmissionError(operation string, observed *models.Task, diagnostics []models.FieldDiagnostic, err error) error {
	if err != nil {
		return &LifecycleError{
			Outcome: NewLifecycleOutcome(operation, observed, models.LifecycleRetryable, "retry", "none"),
			Err:     fmt.Errorf("the runtime-input admission check could not run; retry: %w", err),
		}
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return NewLifecycleInvalidInputError(operation, observed, diagnostics, &PreconditionError{Reason: diagnostics[0].Field + ": " + diagnostics[0].Constraint})
}

// runtimeInputsNeedStrictCarrier refuses declarations on a task whose
// allocation reference cannot carry a strict acceptance contract at all.
// Tasks it passes are judged by loadAcceptanceInput, which refuses runtime
// inputs on any carrier that turns out not to be strict.
func runtimeInputsNeedStrictCarrier(task *models.Task) []models.FieldDiagnostic {
	if len(task.RuntimeInputs) == 0 || acceptanceCreationApplies(task) {
		return nil
	}
	return []models.FieldDiagnostic{runtimeInputDiagnostic("/runtime_inputs", runtimeInputsStrictReason, models.FieldValueClassConflict)}
}

// checkReplacementRuntimeInputs runs the runtime-input admission rules on the
// replacement as the transaction will build it. Like the acceptance verdict,
// it is reported only after replay.
func checkReplacementRuntimeInputs(projectRoot string, state *models.State, source *models.Task, input ReplaceTaskInput, pb *pipelineBundle) func(operation string, observed *models.Task) error {
	replacementInput := input.Replacement
	replacement, err := buildReplacementTask(&replacementInput, pb.resolver)
	if err != nil {
		// The transaction rebuilds it and reports this error in its own order.
		return func(string, *models.Task) error { return nil }
	}
	inheritReplacementLineage(&replacement, source)
	if diagnostics := runtimeInputsNeedStrictCarrier(&replacement); diagnostics != nil {
		return func(operation string, observed *models.Task) error {
			return runtimeInputAdmissionError(operation, observed, diagnostics, nil)
		}
	}
	diagnostics, checkErr := checkRuntimeInputDeclarations(projectRoot, state, []string{source.ID, replacement.ID},
		[]runtimeInputCarrier{{inputs: replacement.RuntimeInputs, prerequisites: replacement.ValidationPrerequisites}})
	for i := range diagnostics {
		diagnostics[i].Field = "/replacement" + diagnostics[i].Field
	}
	return func(operation string, observed *models.Task) error {
		return runtimeInputAdmissionError(operation, observed, diagnostics, checkErr)
	}
}
