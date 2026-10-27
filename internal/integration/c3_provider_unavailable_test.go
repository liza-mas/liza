package integration

import (
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providersignal"
)

// C3 P1: a real supervisor raises the unavailable signal from the failed launch
// tool and a supervisor of the other Codex tool stops on it, in both
// directions, under the project's configuration at the time it checks.

func TestC3ProviderUnavailableRaiseStopsAliasSupervisorBothWays(t *testing.T) {
	for _, tc := range []struct{ raiser, stopped string }{
		{"codex", "codex-acp"},
		{"codex-acp", "codex"},
	} {
		t.Run(tc.raiser+" raise stops "+tc.stopped, func(t *testing.T) {
			fixture := newProviderUnavailableSupervisorFixture(t)
			failing := &deterministicLLMAgent{result: agent.LLMAgentRunResult{ExitCode: 7, Output: codexSessionStartupFailure}}
			runProviderUnavailableSupervisor(t, fixture, "coder-1", tc.raiser, failing)
			if got := failing.recordedBackends(); len(got) != 1 {
				t.Fatalf("raising supervisor executions = %v, want one before the provider classification stops it", got)
			}
			signals := providersignal.ListProviderUnavailableSignals(fixture.projectRoot)
			if len(signals) != 1 || signals[0].Tool != tc.raiser || signals[0].Provider != "codex" {
				t.Fatalf("records = %+v, want the %s launch tool displayed as codex", signals, tc.raiser)
			}

			observer := &deterministicLLMAgent{}
			runProviderUnavailableSupervisor(t, fixture, "coder-2", tc.stopped, observer)
			if got := observer.recordedBackends(); len(got) != 0 {
				t.Fatalf("%s executions = %v, want none: %s raised the signal", tc.stopped, got, tc.raiser)
			}
		})
	}
}

func TestC3ProviderUnavailableOverrideSeparatesAliasSupervisor(t *testing.T) {
	fixture := newProviderUnavailableSupervisorFixture(t)
	if err := fixture.blackboard.Modify(func(s *models.State) error {
		s.Config.AgentTools = map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	failing := &deterministicLLMAgent{result: agent.LLMAgentRunResult{ExitCode: 7, Output: codexSessionStartupFailure}}
	runProviderUnavailableSupervisor(t, fixture, "coder-1", "codex", failing)

	observer := &deterministicLLMAgent{result: agent.LLMAgentRunResult{ExitCode: 1, Output: "unrelated provider transport failure"}}
	runProviderUnavailableSupervisor(t, fixture, "coder-2", "codex-acp", observer)
	if got := observer.recordedBackends(); len(got) == 0 {
		t.Fatal("codex-acp has its own provider key under the override and must not be stopped by a codex signal")
	}
}
