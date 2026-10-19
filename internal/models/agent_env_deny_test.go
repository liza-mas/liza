package models

import (
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
)

// config.agent_env_deny is stripped from every session and supervisor, so it
// must never name what processes or the engine depend on (ADR-0213).
func TestValidateAgentEnvDenyRefusesProcessEngineAndMalformedNames(t *testing.T) {
	if err := ValidateAgentEnvDeny([]string{"OPENAI_API_KEY", "NANGO_SECRET_KEY"}); err != nil {
		t.Fatalf("valid names refused: %v", err)
	}
	if err := ValidateAgentEnvDeny(nil); err != nil {
		t.Fatalf("empty list refused: %v", err)
	}
	for name, names := range map[string][]string{
		"process variable":        {"PATH"},
		"locale prefix":           {"LC_ALL"},
		"engine namespace":        {brand.EnvName("AGENT_ID")},
		"windows home":            {"USERPROFILE"},
		"windows home lower case": {"userprofile"},
		"windows system root":     {"SystemRoot"},
		"windows shell":           {"COMSPEC"},
		"leading digit":           {"1BAD"},
		"dash":                    {"A-B"},
		"empty":                   {""},
		"duplicate":               {"OPENAI_API_KEY", "OPENAI_API_KEY"},
	} {
		if err := ValidateAgentEnvDeny(names); err == nil {
			t.Errorf("%s: %q accepted", name, names)
		}
	}
}

// Names differing only in case are distinct variables except on Windows.
func TestValidateAgentEnvDenyDuplicatesFollowPlatformCase(t *testing.T) {
	previous := envNamesFoldCase
	t.Cleanup(func() { envNamesFoldCase = previous })
	names := []string{"OPENAI_API_KEY", "openai_api_key"}

	envNamesFoldCase = false
	if err := ValidateAgentEnvDeny(names); err != nil {
		t.Fatalf("case-sensitive platform refused distinct names: %v", err)
	}
	envNamesFoldCase = true
	if err := ValidateAgentEnvDeny(names); err == nil {
		t.Fatal("case-insensitive platform accepted the same name twice")
	}
}

func TestReservedEnvNamesUnionsRuntimeInputsAndAgentEnvDeny(t *testing.T) {
	state := &State{
		Config: Config{AgentEnvDeny: []string{"OPENAI_API_KEY"}},
		Tasks:  []Task{{ID: "live", RuntimeInputs: []RuntimeInput{{ID: "w03", Env: []string{"W03_FIXTURE"}}}}},
	}
	got := ReservedEnvNames(state)
	if !got["OPENAI_API_KEY"] || !got["W03_FIXTURE"] || len(got) != 2 {
		t.Fatalf("ReservedEnvNames = %v, want the runtime-input and denied names", got)
	}
	if RuntimeInputDenyNames(state)["OPENAI_API_KEY"] {
		t.Fatal("RuntimeInputDenyNames must stay runtime-input only")
	}
}

// Windows environment names are case-insensitive: a denied name must match
// whatever spelling the host uses there, and only there.
func TestScrubEnvironmentFoldsNameCaseOnlyWhereNamesAreCaseInsensitive(t *testing.T) {
	previous := envNamesFoldCase
	t.Cleanup(func() { envNamesFoldCase = previous })
	env := []string{"OpenAI_Api_Key=host", "OTHER=kept"}
	deny := map[string]bool{"OPENAI_API_KEY": true}

	envNamesFoldCase = true
	if got := ScrubEnvironment(env, deny); strings.Join(got, ",") != "OTHER=kept" {
		t.Fatalf("case-insensitive scrub = %v, want the mixed-case spelling removed", got)
	}
	lowerDeny := map[string]bool{"openai_api_key": true}
	if got := ScrubEnvironment(env, lowerDeny); strings.Join(got, ",") != "OTHER=kept" {
		t.Fatalf("case-insensitive scrub with a lower-case deny key = %v, want the mixed-case spelling removed", got)
	}
	envNamesFoldCase = false
	if got := ScrubEnvironment(env, deny); !slices.Equal(got, env) {
		t.Fatalf("case-sensitive scrub = %v, want a differently spelled name kept", got)
	}
}

// A denied name required as a session prerequisite could never pass, like a
// runtime-input name: both are reported as collisions.
func TestRuntimeInputNameCollisionsIncludeAgentEnvDeny(t *testing.T) {
	state := &State{
		Config: Config{AgentEnvDeny: []string{"OPENAI_API_KEY"}},
		Tasks:  []Task{{ID: "live", Status: TaskStatusReady, ValidationPrerequisites: []ValidationPrerequisite{{Command: "check", Env: []string{"OPENAI_API_KEY"}}}}},
	}
	if got := RuntimeInputNameCollisions(state); !slices.Equal(got, []string{"OPENAI_API_KEY"}) {
		t.Fatalf("collisions = %v, want the denied prerequisite", got)
	}
}
