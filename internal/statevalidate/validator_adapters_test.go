package statevalidate

import (
	"io"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// Error-returning adapters over the collecting validators, so unit tests of a
// single validator assert on one error as they did before validation stopped
// failing at the first violation. The error is nil iff the validator found
// nothing; otherwise it lists every violation it found.

func taskInvariantsErr(state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) error {
	return collectErr(func(v *violations) {
		validateTaskInvariants(v, state, projectRoot, skipSpecFileCheck, resolver, cfg)
	})
}

func taskStatesErr(state *models.State, _ string, _ bool, resolver *pipeline.Resolver) error {
	return collectErr(func(v *violations) { validateTaskStates(v, state, resolver) })
}

func dependenciesErr(state *models.State, _ string, _ bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig, warnWriter io.Writer) error {
	return collectErr(func(v *violations) { validateDependencies(v, state, resolver, cfg, warnWriter) })
}

func agentInvariantsErr(state *models.State, _ string, _ bool, warnWriter io.Writer, resolver *pipeline.Resolver) error {
	return collectErr(func(v *violations) {
		validateAgentInvariants(v, state, warnWriter, resolver, time.Now().UTC())
	})
}

func anomaliesErr(state *models.State, _ string, _ bool) error {
	return collectErr(func(v *violations) { validateAnomalies(v, state) })
}

func handoffEventsErr(state *models.State, _ string, _ bool) error {
	return collectErr(func(v *violations) { validateHandoffEvents(v, state) })
}

func roleNamesErr(state *models.State, _ string, _ bool) error {
	return collectErr(func(v *violations) { validateRoleNames(v, state) })
}

func sprintErr(state *models.State, _ string, _ bool) error {
	return collectErr(func(v *violations) { validateSprint(v, state) })
}

func sprintHistoryErr(state *models.State) error {
	return collectErr(func(v *violations) { validateSprintHistory(v, state) })
}

func acceptanceStateErr(task *models.Task) error {
	return collectErr(func(v *violations) { validateAcceptanceState(v, task) })
}

func taskOutputErr(task *models.Task, validateArtifactRefs bool) error {
	return collectErr(func(v *violations) { validateTaskOutput(v, task, validateArtifactRefs) })
}

func integrationLifecycleErr(state *models.State, _ string, _ bool) error {
	return collectErr(func(v *violations) { validateIntegrationLifecycle(v, state) })
}
