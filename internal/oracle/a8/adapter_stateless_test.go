//go:build !oraclestart

package a8

import (
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/prompts"
)

// Starting-tree adapter for the stateless wake surface. When the render call
// gains its matcher argument, its pre-edit content moves verbatim into an
// immutable oraclestart twin (ADR-0219 item 3).
func statelessWakeTriggers() []string {
	triggers := make([]string, 0, len(prompts.WakeTriggers))
	for _, trigger := range prompts.WakeTriggers {
		triggers = append(triggers, string(trigger))
	}
	return triggers
}

func renderStatelessWake(trigger string) (string, error) {
	return prompts.RenderWakeInstructions(prompts.WakeTrigger(trigger), "orchestrator-1", agent.SubmissionTestPatternsText())
}
