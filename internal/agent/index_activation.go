package agent

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
)

// ensureProjectRootIndexActivation installs or updates the lifecycle hooks
// that refresh project-root indexes when they are missing or no longer match
// the current env gates, scip_search config and repository layout.
//
// No agent refreshes project-root indexes; the hooks do, and MAS init installs
// them. This repair covers projects initialized before init did, and config or
// layout drift since. Re-running MAS init is not a remedy: it recreates the
// project. Only the orchestrator runs this, after registration has made it the
// project's single orchestrator, so no two supervisors write the hooks
// concurrently and a rejected duplicate never rewrites them. A failure never
// stops the supervisor: it is alerted, because the cost is stale indexes, not
// lost work.
func ensureProjectRootIndexActivation(bb *db.Blackboard, projectRoot string) {
	logger := GetLogger()
	state, err := bb.Read()
	if err != nil {
		logger.Warn("Cannot check project-root index hooks", "error", err)
		return
	}
	plan, err := pairingindex.PlanActivation(pairingindex.MASActivationOptions(projectRoot, state.Config.ScipSearch))
	if err != nil {
		alertIndexActivationFailure(projectRoot, err)
		return
	}
	if !plan.Active() {
		return
	}
	status, err := pairingindex.CheckActivation(plan.Install)
	if err != nil {
		alertIndexActivationFailure(projectRoot, err)
		return
	}
	if status == pairingindex.ActivationCurrent {
		return
	}
	if _, err := pairingindex.InstallActivation(plan.Install); err != nil {
		alertIndexActivationFailure(projectRoot, err)
		return
	}
	logger.Info("Repaired project-root index hooks", "previous_status", string(status))
}

func alertIndexActivationFailure(projectRoot string, err error) {
	logger := GetLogger()
	logger.Warn("Project-root index hooks unavailable", "error", err)
	if alertErr := alerts.Write(paths.New(projectRoot).AlertsLogPath(), alerts.Alert{
		Timestamp: time.Now().UTC(),
		Level:     alerts.AlertLevelWarning,
		Category:  "INDEX HOOKS UNAVAILABLE",
		Message:   fmt.Sprintf("project-root indexes will not refresh: %v", err),
	}); alertErr != nil {
		logger.Warn("Failed to alert on index hook failure", "error", alertErr)
	}
}
