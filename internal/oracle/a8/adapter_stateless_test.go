package a8

import "github.com/liza-mas/liza/internal/prompts"

// Starting-tree adapter for the stateless wake surface. When the render call
// gains its matcher argument, its pre-edit content moves verbatim into an
// immutable oraclestart twin (ADR-0219 item 3).
func statelessWakeTriggers() []string {
	return append([]string(nil), prompts.WakeTriggers...)
}

func renderStatelessWake(trigger string) (string, error) {
	return prompts.RenderWakeInstructions(trigger, "orchestrator-1")
}
