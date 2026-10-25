package ops

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// validationCode is a readiness refusal the preflight decides itself, as opposed
// to a prerequisite probe failure, whose codes sessionvalidation owns. The set
// is closed: Error renders each as "validation preflight: <code>".
type validationCode string

const (
	validationCodeRegistrationRequired        validationCode = "registration_required"
	validationCodeWorktreeUnavailable         validationCode = "worktree_unavailable"
	validationCodePipelineUnavailable         validationCode = "pipeline_unavailable"
	validationCodeIntegrationUnavailable      validationCode = "integration_unavailable"
	validationCodeConfigurationChanged        validationCode = "configuration_changed"
	validationCodeCurrentSessionRequired      validationCode = "current_session_required"
	validationCodeRetryPending                validationCode = "retry_pending"
	validationCodeArtifactUnsupported         validationCode = "artifact_unsupported"
	validationCodeExecutionPolicyRequired     validationCode = "execution_policy_required"
	validationCodeContextChanged              validationCode = "context_changed"
	validationCodeOwnershipChanged            validationCode = "ownership_changed"
	validationCodeCommitChanged               validationCode = "commit_changed"
	validationCodeIntegrationChanged          validationCode = "integration_changed"
	validationCodeCandidateChanged            validationCode = "candidate_changed"
	validationCodeExecutableOwnershipRequired validationCode = "executable_ownership_required"
)

// validationResult and validationMethod are the persisted readiness record's
// result and method. The record keeps plain strings (models owns it); ops
// converts at that boundary.
type (
	validationResult string
	validationMethod string
)

const (
	validationResultPassed validationResult = "passed"
	validationResultFailed validationResult = "failed"
	// validationMethodDirect is the only method: the prerequisites were probed
	// by the preflight itself, not attested.
	validationMethodDirect validationMethod = "direct"
)

// validation_execution policy values a session and a tool's configuration
// carry. models keeps the configured value as a plain string, so ops names the
// two values its checks compare.
const (
	validationExecutionLocal        = "local"
	validationExecutionArtifactOnly = "artifact-only"
)

// ValidationPreflightFailedCategory is the alert category of a new failed
// validation preflight observation.
const ValidationPreflightFailedCategory = "VALIDATION PREFLIGHT FAILED"

// recordObservation sets the readiness record's result and the sanitized fields
// of the check outcome. It stores no command, probe or environment content.
func (p *ValidationPreflight) recordObservation(checkErr error) {
	p.record.Result = string(validationResultPassed)
	if checkErr == nil {
		return
	}
	p.record.Result = string(validationResultFailed)
	p.record.Code = string(sessionvalidation.CodePrerequisiteFailed)
	var probe *sessionvalidation.Error
	var local *validationFailure
	if errors.As(checkErr, &probe) {
		p.record.Code = string(probe.Code)
		p.record.CommandIndex = probe.CommandIndex
		p.record.CheckIndex = probe.CheckIndex
		p.record.Variable = probe.Variable
	}
	if errors.As(checkErr, &local) {
		p.record.Code = string(local.code)
	}
}

// recordReadiness persists the observation for this agent and task in one
// transaction and reports whether it is a new failure. K1-E5: p.CheckCurrent,
// which runs Git, is the transaction's inherited final revalidation and stays
// inside it; no probe runs under the lock.
func (p *ValidationPreflight) recordReadiness(bb *db.Blackboard) (newFailure bool, err error) {
	agentID, taskID := p.agentID, p.record.TaskID
	err = bb.Modify(func(current *models.State) error {
		if err := p.CheckCurrent(current); err != nil {
			return err
		}
		if current.ValidationReadiness == nil {
			current.ValidationReadiness = make(map[string]map[string]models.ValidationReadiness)
		}
		if current.ValidationReadiness[agentID] == nil {
			current.ValidationReadiness[agentID] = make(map[string]models.ValidationReadiness)
		}
		previous, recorded := current.ValidationReadiness[agentID][taskID]
		newFailure = p.record.Result == string(validationResultFailed) && !(recorded && sameValidationObservation(previous, p.record))
		current.ValidationReadiness[agentID][taskID] = p.record
		return nil
	})
	return newFailure, err
}

// sameValidationObservation compares the sanitized fields that tell an operator
// what to repair. An unchanged failure retried every interval is one condition;
// a different code, check or contract is a new one.
func sameValidationObservation(a, b models.ValidationReadiness) bool {
	return a.Result == b.Result && a.Code == b.Code && a.CommandIndex == b.CommandIndex &&
		a.CheckIndex == b.CheckIndex && a.Variable == b.Variable && a.Digest == b.Digest
}

// writeValidationPreflightAlert surfaces a failed preflight: the claim is
// released and retried silently otherwise. It carries only the sanitized
// record fields, never command, probe or environment content.
func writeValidationPreflightAlert(projectRoot, agentID string, record models.ValidationReadiness) {
	detail := fmt.Sprintf("command=%d check=%d", record.CommandIndex, record.CheckIndex)
	if record.Variable != "" {
		detail += " variable=" + record.Variable
	}
	err := alerts.Write(paths.New(projectRoot).AlertsLogPath(), alerts.Alert{
		Timestamp: time.Now().UTC(),
		Level:     alerts.AlertLevelWarning,
		Category:  ValidationPreflightFailedCategory,
		Message: fmt.Sprintf("%s — agent %s failed validation preflight: %s (%s); repair that prerequisite in the agent's launch environment, then let it retry (%s)",
			record.TaskID, agentID, record.Code, detail, brand.Command("repair-agent-pool", "--dry-run")),
	})
	if err != nil {
		log.Printf("WARNING: validation preflight alert for %s: %v", record.TaskID, err)
	}
}
