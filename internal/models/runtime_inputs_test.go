package models

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

func validRuntimeInputs() []RuntimeInput {
	return []RuntimeInput{
		{ID: "forms-fixture", Commands: []string{"make canary"}, Recipe: "project.forms", Consumption: RuntimeInputSingleUse, Env: []string{"FORMS_FIXTURE"}},
		{ID: "w03-fixture", Commands: []string{"make canary"}, Recipe: "project.w03", Consumption: RuntimeInputSingleUse, Env: []string{"W03_FIXTURE", "W03_FILE"}, Files: []string{"W03_FILE"}, After: []string{"forms-fixture"}},
		{ID: "principals", Commands: []string{"make canary", "make eplane"}, Recipe: "project.principals", Consumption: RuntimeInputReusable, Secret: true, Env: []string{"MEMBER_A_CREDENTIAL"}},
	}
}

func TestRuntimeInputViolationsAcceptsAValidDeclaration(t *testing.T) {
	if err := ValidateRuntimeInputs([]string{"make canary", "make eplane"}, validRuntimeInputs()); err != nil {
		t.Fatalf("ValidateRuntimeInputs: %v", err)
	}
	if err := ValidateRuntimeInputs(nil, nil); err != nil {
		t.Fatalf("empty declaration: %v", err)
	}
}

func TestRuntimeInputViolationsRefuseEachStructuralDefect(t *testing.T) {
	commands := []string{"make canary", "make eplane"}
	cases := map[string]struct {
		mutate func([]RuntimeInput) []RuntimeInput
		want   string
	}{
		"bad id":          {func(in []RuntimeInput) []RuntimeInput { in[0].ID = "Bad"; return in }, "runtime_inputs[0].id"},
		"duplicate id":    {func(in []RuntimeInput) []RuntimeInput { in[1].ID = in[0].ID; return in }, "duplicates another declaration"},
		"command outside": {func(in []RuntimeInput) []RuntimeInput { in[0].Commands = []string{"make other"}; return in }, "must equal one of the entry's validation commands"},
		"no command":      {func(in []RuntimeInput) []RuntimeInput { in[0].Commands = nil; return in }, "commands requires"},
		"bad recipe":      {func(in []RuntimeInput) []RuntimeInput { in[0].Recipe = "Project"; return in }, "recipe must be"},
		"bad consumption": {func(in []RuntimeInput) []RuntimeInput { in[0].Consumption = "once"; return in }, "consumption must be"},
		"no env":          {func(in []RuntimeInput) []RuntimeInput { in[0].Env = nil; return in }, "env requires"},
		"env shared":      {func(in []RuntimeInput) []RuntimeInput { in[2].Env = []string{"FORMS_FIXTURE"}; return in }, "already delivered by runtime_inputs[0]"},
		"file not in env": {func(in []RuntimeInput) []RuntimeInput { in[0].Files = []string{"OTHER"}; return in }, "files[0] must name"},
		"after unknown":   {func(in []RuntimeInput) []RuntimeInput { in[0].After = []string{"nope"}; return in }, "after[0] must name"},
		"after cycle":     {func(in []RuntimeInput) []RuntimeInput { in[0].After = []string{"w03-fixture"}; return in }, "cycle"},
		"after self":      {func(in []RuntimeInput) []RuntimeInput { in[0].After = []string{"forms-fixture"}; return in }, "after[0] must name"},
		"duplicate command": {func(in []RuntimeInput) []RuntimeInput {
			in[2].Commands = []string{"make canary", "make canary"}
			return in
		}, "duplicates another command"},
		"env repeated local": {func(in []RuntimeInput) []RuntimeInput { in[0].Env = []string{"A", "A"}; return in }, "duplicates another variable"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := ValidateRuntimeInputs(commands, tc.mutate(CloneRuntimeInputs(validRuntimeInputs())))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRuntimeInputsEqualIgnoresDeclarationOrderOnly(t *testing.T) {
	a := validRuntimeInputs()
	b := CloneRuntimeInputs(a)
	b[0], b[2] = b[2], b[0]
	if !RuntimeInputsEqual(a, b) {
		t.Fatal("declaration order changed equality")
	}
	for name, mutate := range map[string]func([]RuntimeInput){
		"consumption": func(in []RuntimeInput) { in[0].Consumption = RuntimeInputReusable },
		"secret":      func(in []RuntimeInput) { in[2].Secret = false },
		"env":         func(in []RuntimeInput) { in[1].Env = []string{"W03_FILE", "W03_FIXTURE"} },
		"removed":     func(in []RuntimeInput) {},
	} {
		changed := CloneRuntimeInputs(a)
		mutate(changed)
		if name == "removed" {
			changed = changed[:2]
		}
		if RuntimeInputsEqual(a, changed) {
			t.Errorf("%s: change not detected", name)
		}
	}
}

func TestRuntimeInputDenyNamesAndScrub(t *testing.T) {
	state := &State{
		Tasks: []Task{
			{ID: "a", RuntimeInputs: validRuntimeInputs()[:1]},
			{ID: "b", Output: []OutputEntry{{RuntimeInputs: validRuntimeInputs()[2:]}}},
		},
		RuntimeInputs: map[string]RuntimeInputInstance{"x": {Names: []string{"LEGACY_TOKEN"}}},
	}
	deny := RuntimeInputDenyNames(state)
	for _, name := range []string{"FORMS_FIXTURE", "MEMBER_A_CREDENTIAL", "LEGACY_TOKEN"} {
		if !deny[name] {
			t.Errorf("deny set misses %s", name)
		}
	}
	env := ScrubEnvironment([]string{"PATH=/bin", "FORMS_FIXTURE=abc", "MEMBER_A_CREDENTIAL=secret"}, deny)
	if strings.Join(env, ",") != "PATH=/bin" {
		t.Fatalf("scrubbed env = %v", env)
	}
}

// Runtime-input names are stripped from every session and gate, so a
// declaration can never name a process-critical or engine identity variable.
func TestRuntimeInputViolationsRefuseReservedNames(t *testing.T) {
	for _, name := range []string{"PATH", "HOME", "LD_PRELOAD", "DYLD_LIBRARY_PATH", "LC_ALL", "path", brand.EnvName("AGENT_ID"), brand.LegacyEnvName("AGENT_GENERATION")} {
		inputs := validRuntimeInputs()
		inputs[0].Env = []string{name}
		err := ValidateRuntimeInputs([]string{"make canary", "make eplane"}, inputs)
		if err == nil || !strings.Contains(err.Error(), "runtime_inputs[0].env[0] is reserved") {
			t.Errorf("%s: err = %v, want reserved", name, err)
		}
	}
}

func TestRuntimeInputNameCollisions(t *testing.T) {
	prerequisite := []ValidationPrerequisite{{Command: "make canary", Env: []string{"FORMS_FIXTURE"}}}
	state := &State{Tasks: []Task{
		{ID: "declares", Status: TaskStatusReady, RuntimeInputs: validRuntimeInputs()},
		{ID: "needs", Status: TaskStatusSuperseded, ValidationPrerequisites: prerequisite},
	}}
	if got := RuntimeInputNameCollisions(state); len(got) != 0 {
		t.Fatalf("terminal task prerequisite collides: %v", got)
	}
	state.Tasks[1].Status = TaskStatusReady
	if got := strings.Join(RuntimeInputNameCollisions(state), ","); got != "FORMS_FIXTURE" {
		t.Fatalf("collisions = %q", got)
	}
	state.Tasks[1] = Task{ID: "plan", Status: TaskStatusMerged, Output: []OutputEntry{{ValidationPrerequisites: prerequisite}}}
	if got := RuntimeInputNameCollisions(state); len(got) != 1 {
		t.Fatalf("pending output prerequisite does not collide: %v", got)
	}
	state.Tasks[1].TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	if got := RuntimeInputNameCollisions(state); len(got) != 0 {
		t.Fatalf("consumed output prerequisite collides: %v", got)
	}
}
