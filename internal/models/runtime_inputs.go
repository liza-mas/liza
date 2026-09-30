package models

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
)

// Runtime-input consumption modes (ADR-0169). A single_use instance is spent by
// the first gate run that uses it; a reusable one may serve several tasks.
const (
	RuntimeInputSingleUse = "single_use"
	RuntimeInputReusable  = "reusable"
)

// Ledger instance states. Consumed and invalidated are terminal: nothing
// returns an instance to available.
const (
	RuntimeInputAvailable   = "available"
	RuntimeInputConsumed    = "consumed"
	RuntimeInputInvalidated = "invalidated"
)

// Invalidation reasons recorded on a ledger instance.
const (
	RuntimeInputInvalidatedArtifactChanged = "artifact_changed"
	RuntimeInputInvalidatedTaskRetired     = "task_retired"
)

// AnomalyTypeRuntimeInputUnavailable is the operator signal a gate refusal
// writes when a declared runtime input has no usable instance. It is a
// provisioning request, not an agent failure pattern: the circuit breaker does
// not count it.
const AnomalyTypeRuntimeInputUnavailable = "runtime_input_unavailable"

// RuntimeInput declares one live-validation input a task's canonical commands
// read (ADR-0169). Env holds variable names only; values come from an
// operator-recorded ledger instance and are delivered to gate and run-live
// subprocesses, never to agent sessions.
type RuntimeInput struct {
	ID          string   `yaml:"id" json:"id"`
	Commands    []string `yaml:"commands" json:"commands"`
	Recipe      string   `yaml:"recipe" json:"recipe"`
	Consumption string   `yaml:"consumption" json:"consumption"`
	Secret      bool     `yaml:"secret,omitempty" json:"secret,omitempty"`
	Env         []string `yaml:"env" json:"env"`
	// Files names the Env variables whose value is an absolute path to a file
	// artifact. The file's content, not its path, is part of the instance's
	// identity.
	Files []string `yaml:"files,omitempty" json:"files,omitempty"`
	After []string `yaml:"after,omitempty" json:"after,omitempty"`
}

// RuntimeInputKeys is the closed key set of one declaration. Any other key
// (for example the not-yet-supported "binding") is refused at admission.
var RuntimeInputKeys = []string{"id", "commands", "recipe", "consumption", "secret", "env", "files", "after"}

var runtimeInputIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// RuntimeInputRecipePattern is the registry name grammar: dotted lower-case
// segments such as "project.w03-fixture".
var RuntimeInputRecipePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9][a-z0-9_-]*)*$`)

const runtimeInputLimit = 64

// Runtime-input names are stripped from every session and gate, so they must
// never name what processes or the engine itself depend on (ADR-0169).
var (
	runtimeInputReservedNames    = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "PWD", "TMPDIR", "TEMP", "TMP", "TERM", "LANG", "TZ"}
	runtimeInputReservedPrefixes = []string{"LC_", "LD_", "DYLD_"}
)

// runtimeInputNameReserved reports whether name is a process-critical
// variable or lies in the engine's own agent namespace (branded or legacy).
func runtimeInputNameReserved(name string) bool {
	upper := strings.ToUpper(name)
	if slices.Contains(runtimeInputReservedNames, upper) {
		return true
	}
	prefixes := append(slices.Clone(runtimeInputReservedPrefixes), strings.ToUpper(brand.EnvName("")), strings.ToUpper(brand.LegacyEnvName("")))
	return slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(upper, prefix) })
}

// ValidateRuntimeInputs checks the structural rules of a declaration set
// against the carrier's canonical commands. Errors name fields and indices
// without echoing declared values.
func ValidateRuntimeInputs(commands []string, inputs []RuntimeInput) error {
	return firstError(RuntimeInputViolations(commands, inputs))
}

// RuntimeInputViolations returns every structural defect of inputs, in the
// order ValidateRuntimeInputs checks them.
func RuntimeInputViolations(commands []string, inputs []RuntimeInput) []error {
	if len(inputs) == 0 {
		return nil
	}
	var errs []error
	if len(inputs) > runtimeInputLimit {
		errs = append(errs, fmt.Errorf("runtime_inputs exceeds %d declarations", runtimeInputLimit))
	}
	canonical := make(map[string]bool, len(commands))
	for _, command := range commands {
		canonical[command] = true
	}
	ids := make(map[string]int, len(inputs))
	for i, input := range inputs {
		if !runtimeInputIDPattern.MatchString(input.ID) || len(input.ID) > 128 {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].id must match [a-z0-9][a-z0-9._-]* (at most 128 bytes)", i))
		} else if _, dup := ids[input.ID]; dup {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].id duplicates another declaration", i))
		} else {
			ids[input.ID] = i
		}
	}
	names := make(map[string]int)
	for i, input := range inputs {
		if len(input.Commands) == 0 || len(input.Commands) > runtimeInputLimit {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].commands requires 1 to %d canonical commands", i, runtimeInputLimit))
		}
		seenCommands := make(map[string]bool, len(input.Commands))
		for j, command := range input.Commands {
			if !canonical[command] {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].commands[%d] must equal one of the entry's validation commands", i, j))
			}
			if seenCommands[command] {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].commands[%d] duplicates another command", i, j))
			}
			seenCommands[command] = true
		}
		if len(input.Recipe) > 256 || !RuntimeInputRecipePattern.MatchString(input.Recipe) {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].recipe must be a dotted registry name", i))
		}
		if input.Consumption != RuntimeInputSingleUse && input.Consumption != RuntimeInputReusable {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].consumption must be single_use or reusable", i))
		}
		if len(input.Env) == 0 || len(input.Env) > runtimeInputLimit {
			errs = append(errs, fmt.Errorf("runtime_inputs[%d].env requires 1 to %d variable names", i, runtimeInputLimit))
		}
		own := make(map[string]bool, len(input.Env))
		for j, name := range input.Env {
			if len(name) > 256 || !prerequisiteEnvName.MatchString(name) {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].env[%d] must be a variable identifier of at most 256 bytes", i, j))
				continue
			}
			if runtimeInputNameReserved(name) {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].env[%d] is reserved: process variables and the %s namespace are never stripped from sessions", i, j, brand.EnvName("")))
				continue
			}
			if other, taken := names[name]; taken {
				if other == i {
					errs = append(errs, fmt.Errorf("runtime_inputs[%d].env[%d] duplicates another variable of this declaration", i, j))
				} else {
					errs = append(errs, fmt.Errorf("runtime_inputs[%d].env[%d] is already delivered by runtime_inputs[%d]", i, j, other))
				}
				continue
			}
			names[name] = i
			own[name] = true
		}
		seenFiles := make(map[string]bool, len(input.Files))
		for j, name := range input.Files {
			if !own[name] || seenFiles[name] {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].files[%d] must name a distinct variable of this declaration's env", i, j))
			}
			seenFiles[name] = true
		}
		for j, dependency := range input.After {
			other, known := ids[dependency]
			if !known || other == i {
				errs = append(errs, fmt.Errorf("runtime_inputs[%d].after[%d] must name another declaration of this entry", i, j))
			}
		}
	}
	if runtimeInputAfterCycle(inputs, ids) {
		errs = append(errs, fmt.Errorf("runtime_inputs after ordering contains a cycle"))
	}
	return errs
}

func runtimeInputAfterCycle(inputs []RuntimeInput, ids map[string]int) bool {
	const (
		unvisited = iota
		visiting
		done
	)
	marks := make([]int, len(inputs))
	var visit func(int) bool
	visit = func(i int) bool {
		switch marks[i] {
		case visiting:
			return true
		case done:
			return false
		}
		marks[i] = visiting
		for _, dependency := range inputs[i].After {
			if next, ok := ids[dependency]; ok && next != i && visit(next) {
				return true
			}
		}
		marks[i] = done
		return false
	}
	for i := range inputs {
		if visit(i) {
			return true
		}
	}
	return false
}

// CloneRuntimeInputs prevents producers from sharing mutable slices with
// persisted tasks or generated children.
func CloneRuntimeInputs(inputs []RuntimeInput) []RuntimeInput {
	cloned := slices.Clone(inputs)
	for i := range cloned {
		cloned[i].Commands = slices.Clone(cloned[i].Commands)
		cloned[i].Env = slices.Clone(cloned[i].Env)
		cloned[i].Files = slices.Clone(cloned[i].Files)
		cloned[i].After = slices.Clone(cloned[i].After)
	}
	return cloned
}

// RuntimeInputsEqual compares two declaration sets independent of declaration
// order. Everything else, including list order within a declaration, is part
// of the reviewed contract.
func RuntimeInputsEqual(a, b []RuntimeInput) bool {
	if len(a) != len(b) {
		return false
	}
	byID := make(map[string]RuntimeInput, len(a))
	for _, input := range a {
		byID[input.ID] = input
	}
	for _, input := range b {
		other, ok := byID[input.ID]
		if !ok || !RuntimeInputEqual(other, input) {
			return false
		}
	}
	return true
}

// RuntimeInputEqual reports field-for-field equality, treating nil and empty
// lists alike.
func RuntimeInputEqual(a, b RuntimeInput) bool {
	return a.ID == b.ID && a.Recipe == b.Recipe && a.Consumption == b.Consumption && a.Secret == b.Secret &&
		slices.Equal(a.Commands, b.Commands) && slices.Equal(a.Env, b.Env) &&
		slices.Equal(a.Files, b.Files) && slices.Equal(a.After, b.After)
}

// FindRuntimeInput returns the declaration with id, or nil.
func FindRuntimeInput(inputs []RuntimeInput, id string) *RuntimeInput {
	for i := range inputs {
		if inputs[i].ID == id {
			return &inputs[i]
		}
	}
	return nil
}

// RuntimeInputEnvNames returns every variable name the declarations deliver.
func RuntimeInputEnvNames(inputs []RuntimeInput) []string {
	var names []string
	for _, input := range inputs {
		names = append(names, input.Env...)
	}
	return names
}

// RuntimeInputConsumption records the gate run that spent a single_use instance.
type RuntimeInputConsumption struct {
	Task  string    `yaml:"task" json:"task"`
	RunID string    `yaml:"run_id" json:"run_id"`
	Agent string    `yaml:"agent,omitempty" json:"agent,omitempty"`
	At    time.Time `yaml:"at" json:"at"`
}

// RuntimeInputInvalidation records why an instance can no longer be used.
type RuntimeInputInvalidation struct {
	Reason string    `yaml:"reason" json:"reason"`
	At     time.Time `yaml:"at" json:"at"`
}

// RuntimeInputInstance is one operator-recorded materialization in the
// consumption ledger (state.runtime_inputs), keyed by its keyed identity
// digest. The ledger stores locators and names, never values.
type RuntimeInputInstance struct {
	Recipe      string `yaml:"recipe" json:"recipe"`
	InputID     string `yaml:"input_id" json:"input_id"`
	Consumption string `yaml:"consumption" json:"consumption"`
	Secret      bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
	// KeyID names the operator key the identity was computed with. It reveals
	// nothing about any value.
	KeyID string `yaml:"key_id" json:"key_id"`
	// Envelope is the absolute path of the operator-owned KEY=VALUE file that
	// transports the values. It is a locator, not part of identity.
	Envelope string `yaml:"envelope" json:"envelope"`
	// Names lists the variables the instance delivers, sorted.
	Names        []string                  `yaml:"names" json:"names"`
	Tasks        []string                  `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	State        string                    `yaml:"state" json:"state"`
	RegisteredAt time.Time                 `yaml:"registered_at" json:"registered_at"`
	Consumed     *RuntimeInputConsumption  `yaml:"consumed,omitempty" json:"consumed,omitempty"`
	Invalidated  *RuntimeInputInvalidation `yaml:"invalidated,omitempty" json:"invalidated,omitempty"`
}

// Terminal reports whether the instance can never be used again.
func (i RuntimeInputInstance) Terminal() bool {
	return i.State == RuntimeInputConsumed || i.State == RuntimeInputInvalidated
}

// BoundTo reports whether taskID is one of the instance's bindings.
func (i RuntimeInputInstance) BoundTo(taskID string) bool {
	return slices.Contains(i.Tasks, taskID)
}

// RuntimeInputInstanceViolations returns the static defects of the ledger,
// in instance-ID order, for validate.
func RuntimeInputInstanceViolations(instances map[string]RuntimeInputInstance) []error {
	var errs []error
	for _, id := range sortedRuntimeInputIDs(instances) {
		instance := instances[id]
		switch instance.State {
		case RuntimeInputAvailable:
			if instance.Consumed != nil || instance.Invalidated != nil {
				errs = append(errs, fmt.Errorf("runtime_inputs[%s] is available but carries a terminal record", id))
			}
		case RuntimeInputConsumed:
			if instance.Consumed == nil {
				errs = append(errs, fmt.Errorf("runtime_inputs[%s] is consumed without a consumption record", id))
			}
		case RuntimeInputInvalidated:
			if instance.Invalidated == nil {
				errs = append(errs, fmt.Errorf("runtime_inputs[%s] is invalidated without an invalidation record", id))
			}
		default:
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] has unknown state", id))
		}
		if instance.Consumption != RuntimeInputSingleUse && instance.Consumption != RuntimeInputReusable {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] has unknown consumption", id))
		}
		if instance.Consumption == RuntimeInputSingleUse && len(instance.Tasks) > 1 {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] is single_use but bound to more than one task", id))
		}
		if instance.KeyID == "" || instance.Envelope == "" || len(instance.Names) == 0 {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] is missing key_id, envelope or names", id))
		}
	}
	return errs
}

// RuntimeInputTransitionViolations compares a ledger with its locked
// pre-image. Instances are never removed, terminal instances never change,
// and an available instance may change only its task bindings or move to a
// terminal state. New instances start available. Every returned violation is
// introduced by the transaction, since the rules compare the two images.
func RuntimeInputTransitionViolations(before, after map[string]RuntimeInputInstance) []error {
	var errs []error
	for _, id := range sortedRuntimeInputIDs(before) {
		previous := before[id]
		current, exists := after[id]
		if !exists {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] was removed; ledger instances are permanent", id))
			continue
		}
		if previous.Terminal() {
			if !reflect.DeepEqual(previous, current) {
				errs = append(errs, fmt.Errorf("runtime_inputs[%s] is %s and cannot change", id, previous.State))
			}
			continue
		}
		frozen := current
		frozen.Tasks = previous.Tasks
		frozen.State = previous.State
		frozen.Consumed = previous.Consumed
		frozen.Invalidated = previous.Invalidated
		if !reflect.DeepEqual(previous, frozen) {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] changed an immutable field", id))
		}
	}
	for _, id := range sortedRuntimeInputIDs(after) {
		if _, existed := before[id]; !existed && after[id].State != RuntimeInputAvailable {
			errs = append(errs, fmt.Errorf("runtime_inputs[%s] must be recorded available", id))
		}
	}
	return errs
}

func sortedRuntimeInputIDs(instances map[string]RuntimeInputInstance) []string {
	ids := make([]string, 0, len(instances))
	for id := range instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// RuntimeInputDenyNames returns every variable name any runtime-input
// declaration or ledger instance in state delivers. These names are reserved
// project-wide: they are scrubbed from agent sessions and from every
// subprocess environment except where an authorized value is overlaid.
func RuntimeInputDenyNames(state *State) map[string]bool {
	names := make(map[string]bool)
	if state == nil {
		return names
	}
	for i := range state.Tasks {
		for _, name := range RuntimeInputEnvNames(state.Tasks[i].RuntimeInputs) {
			names[name] = true
		}
		for _, entry := range state.Tasks[i].Output {
			for _, name := range RuntimeInputEnvNames(entry.RuntimeInputs) {
				names[name] = true
			}
		}
	}
	for _, instance := range state.RuntimeInputs {
		for _, name := range instance.Names {
			names[name] = true
		}
	}
	return names
}

// RuntimeInputOutputPending reports whether a task's output entries can still
// generate children: its transitions have not fired and it is live or merged.
func RuntimeInputOutputPending(task *Task) bool {
	return len(task.Output) > 0 && len(task.TransitionsExecuted) == 0 &&
		(!task.Status.IsTerminal() || task.Status == TaskStatusMerged)
}

// SessionPrerequisiteNames returns the validation_prerequisites env names a
// session may still be required to hold: those of live tasks and of output
// entries that can still generate children.
func SessionPrerequisiteNames(state *State) map[string]bool {
	names := make(map[string]bool)
	if state == nil {
		return names
	}
	add := func(prerequisites []ValidationPrerequisite) {
		for _, prerequisite := range prerequisites {
			for _, name := range prerequisite.Env {
				names[name] = true
			}
		}
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if !task.Status.IsTerminal() {
			add(task.ValidationPrerequisites)
		}
		if RuntimeInputOutputPending(task) {
			for _, entry := range task.Output {
				add(entry.ValidationPrerequisites)
			}
		}
	}
	return names
}

// RuntimeInputNameCollisions returns, sorted, the names that are both
// runtime-input names (stripped from sessions) and session prerequisites
// (required in sessions). Any such name makes its prerequisite unsatisfiable.
func RuntimeInputNameCollisions(state *State) []string {
	deny := RuntimeInputDenyNames(state)
	var collisions []string
	for name := range SessionPrerequisiteNames(state) {
		if deny[name] {
			collisions = append(collisions, name)
		}
	}
	sort.Strings(collisions)
	return collisions
}

// ScrubEnvironment returns env without entries whose name is denied.
func ScrubEnvironment(env []string, deny map[string]bool) []string {
	if len(deny) == 0 {
		return slices.Clone(env)
	}
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if deny[name] {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}
