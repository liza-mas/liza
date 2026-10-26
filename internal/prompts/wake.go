package prompts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// wakeTemplateData is used by wake trigger templates that need GoalSpecRef
type wakeTemplateData struct {
	AgentID                    string
	GoalSpecRef                string
	GoalEntryPoint             string         // set if --entry-point was specified
	ResolvedRolePair           string         // role-pair resolved from GoalEntryPoint
	ResolvedDisplayName        string         // display name of the resolved role-pair's doer
	ResolvedTaskIDPrefix       string         // configured task ID prefix, e.g., "ep"
	ResolvedTaskType           string         // resolved from doer role → TaskTypeForRole
	ResolvedEntryPoint         WakeEntryPoint // resolved route data for GoalEntryPoint
	ResolvedHasFanOutTarget    bool
	ResolvedFanOutRolePair     string
	ResolvedFanOutDisplayName  string
	ResolvedFanOutTaskIDPrefix string
	ResolvedFanOutTaskType     string
	EntryPoints                []WakeEntryPoint // available entry-points for LLM classification
	Integration                EffectiveIntegrationCompletion
	// TestFilePatterns is the submission test-file matcher display text.
	TestFilePatterns string
	// BlockedScope is the BLOCKED_TASKS wake's actionable set, computed from
	// state. Nil only on the stateless RenderWakeInstructions path; an empty
	// computed set means no assessment, never every BLOCKED task.
	BlockedScope *blockedScopeData
}

type blockedScopeData struct {
	TaskIDs []string
}

// EffectiveIntegrationCompletion is the read-only wake projection of the
// authoritative integration progress and reconciliation outcomes.
type EffectiveIntegrationCompletion struct {
	WakeTrigger    WakeTrigger
	Status         IntegrationViewStatus
	ReasonCode     string
	TaskIDs        []string
	RequestKeys    []string
	CreatedTaskIDs []string
	Guidance       string
	// DeferredValidationRequested marks a wait during which integration needs
	// a deferred-validation batch at HEAD, whatever the wait's reason.
	DeferredValidationRequested bool
}

// ProjectEffectiveIntegrationCompletion maps authoritative integration
// progress and reconciliation outcomes into deterministic wake vocabulary.
func ProjectEffectiveIntegrationCompletion(
	decision models.IntegrationProgressDecision,
	reconciliation *models.ReconcileIntegrationAnalysesResult,
	evaluationErr error,
) EffectiveIntegrationCompletion {
	if evaluationErr != nil {
		return EffectiveIntegrationCompletion{
			WakeTrigger: WakeIntegrationUnavailable,
			Status:      IntegrationViewUnavailable,
			ReasonCode:  ReasonIntegrationProgressUnavailable,
			Guidance:    evaluationErr.Error(),
		}
	}
	if decision.IntegrationComplete {
		return EffectiveIntegrationCompletion{
			WakeTrigger: WakeSprintComplete,
			Status:      IntegrationViewComplete,
		}
	}

	reason := decision.Blocked
	if reconciliation != nil && reconciliation.Reason != nil {
		reason = reconciliation.Reason
	}
	if reason != nil {
		trigger, status := WakeIntegrationBlocked, IntegrationViewBlocked
		if decision.Exhausted {
			trigger, status = WakeIntegrationExhausted, IntegrationViewExhausted
		}
		return EffectiveIntegrationCompletion{
			WakeTrigger: trigger,
			Status:      status,
			ReasonCode:  reason.Code,
			TaskIDs:     sortedStringsCopy(reason.TaskIDs),
			Guidance:    reason.Guidance,
		}
	}

	requestKeys := integrationRequestKeys(decision)
	var createdTaskIDs []string
	if reconciliation != nil {
		createdTaskIDs = sortedStringsCopy(reconciliation.CreatedTaskIDs)
	}
	if decision.FreezeContributingSet || len(requestKeys) > 0 {
		return EffectiveIntegrationCompletion{
			WakeTrigger:    WakeCodingComplete,
			Status:         IntegrationViewReconciliationNeeded,
			RequestKeys:    requestKeys,
			CreatedTaskIDs: createdTaskIDs,
		}
	}
	if decision.Waiting != nil {
		return EffectiveIntegrationCompletion{
			WakeTrigger:                 WakeIntegrationWaiting,
			Status:                      IntegrationViewWaiting,
			ReasonCode:                  decision.Waiting.Code,
			TaskIDs:                     sortedStringsCopy(decision.Waiting.TaskIDs),
			CreatedTaskIDs:              createdTaskIDs,
			Guidance:                    decision.Waiting.Guidance,
			DeferredValidationRequested: decision.DeferredValidationRequest != "",
		}
	}
	return EffectiveIntegrationCompletion{
		WakeTrigger: WakeIntegrationUnavailable,
		Status:      IntegrationViewUnavailable,
		ReasonCode:  ReasonIntegrationProgressIncomplete,
	}
}

func integrationRequestKeys(decision models.IntegrationProgressDecision) []string {
	keys := make([]string, 0, len(decision.SliceRequests)+1)
	for _, request := range decision.SliceRequests {
		keys = append(keys, request.Key)
	}
	if decision.GlobalRequest != nil {
		keys = append(keys, decision.GlobalRequest.Key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringsCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func writeIntegrationProgressDiagnostic(b *strings.Builder, projection EffectiveIntegrationCompletion) {
	if projection.Status == "" {
		return
	}
	b.WriteString("\nINTEGRATION PROGRESS:\n")
	b.WriteString(fmt.Sprintf("- Status: %s\n", projection.Status))
	if projection.ReasonCode != "" {
		b.WriteString(fmt.Sprintf("- Reason: %s\n", projection.ReasonCode))
	}
	if len(projection.TaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("- Related tasks: %s\n", strings.Join(projection.TaskIDs, ", ")))
	}
	if len(projection.RequestKeys) > 0 {
		b.WriteString(fmt.Sprintf("- Requested analyses: %s\n", strings.Join(projection.RequestKeys, ", ")))
	}
	if len(projection.CreatedTaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("- Reconciled tasks: %s\n", strings.Join(projection.CreatedTaskIDs, ", ")))
	}
	if projection.Guidance != "" {
		b.WriteString(fmt.Sprintf("- Guidance: %s\n", projection.Guidance))
	}
}

func integrationOutcomeInstructions(projection EffectiveIntegrationCompletion) string {
	var b strings.Builder
	b.WriteString("Authoritative integration completion is not effective.\n")
	if projection.ReasonCode != "" {
		b.WriteString(fmt.Sprintf("Reason: %s\n", projection.ReasonCode))
	}
	if len(projection.TaskIDs) > 0 {
		b.WriteString(fmt.Sprintf("Related tasks: %s\n", strings.Join(projection.TaskIDs, ", ")))
	}
	if projection.Guidance != "" {
		b.WriteString(fmt.Sprintf("Guidance: %s\n", projection.Guidance))
	}
	b.WriteString("Preserve this outcome; do not create analysis tasks or advance terminal state manually.")
	return b.String()
}

// wakePlanningCompleteData is used by the PLANNING_COMPLETE wake template.
// Plans are split by hand-off class so a decided plan is never re-reviewed.
type wakePlanningCompleteData struct {
	AgentID    string
	ToReview   []PlanningTask // needs_review
	Reconcile  []PlanningTask // needs_reconciliation
	Ready      []PlanningTask // passed, or outside the reviewed hand-off
	Amendments []PlanningTask // terminal pending correction, generation fenced
	// TestFilePatterns is the submission test-file matcher display text.
	TestFilePatterns string
}

func buildWakePlanningCompleteData(agentID, testFilePatterns string, planningTasks []PlanningTask) wakePlanningCompleteData {
	data := wakePlanningCompleteData{AgentID: agentID, TestFilePatterns: testFilePatterns}
	for _, task := range planningTasks {
		switch task.Class {
		case models.PlanHandoffAmendmentReady:
			data.Amendments = append(data.Amendments, task)
			data.ToReview = append(data.ToReview, task)
		case models.PlanHandoffNeedsReview:
			data.ToReview = append(data.ToReview, task)
		case models.PlanHandoffNeedsReconciliation:
			data.Reconcile = append(data.Reconcile, task)
		case models.PlanHandoffPassed, models.PlanHandoffOutOfDomain:
			data.Ready = append(data.Ready, task)
		}
	}
	return data
}

// wakeHumanNoteData feeds the HUMAN_NOTE wake template.
type wakeHumanNoteData struct {
	Notes []wakeHumanNoteEntry
}

type wakeHumanNoteEntry struct {
	Timestamp string
	For       string
	Message   string
}

func buildWakeHumanNoteData(notes []models.HumanNote) wakeHumanNoteData {
	var data wakeHumanNoteData
	for _, note := range notes {
		data.Notes = append(data.Notes, wakeHumanNoteEntry{
			Timestamp: note.Timestamp.UTC().Format(time.RFC3339),
			For:       note.For,
			Message:   note.Message,
		})
	}
	return data
}

func determineWakeTrigger(totalTasks, blocked, hypothesisExhausted, immediateDiscoveries, unseenNotes int, sprintComplete, codingComplete bool, planningTasks []PlanningTask, m2oReadyCount int) WakeTrigger {
	switch {
	case totalTasks == 0:
		return WakeInitialPlanning
	case blocked > 0:
		return WakeBlockedTasks
	case hypothesisExhausted > 0:
		return WakeHypothesisExhausted
	case immediateDiscoveries > 0:
		return WakeImmediateDiscovery
	case unseenNotes > 0:
		return WakeHumanNote
	case len(planningTasks) > 0:
		return WakePlanningComplete
	case m2oReadyCount > 0:
		return WakeManyToOneReady
	case sprintComplete && codingComplete:
		return WakeCodingComplete
	case sprintComplete:
		return WakeSprintComplete
	default:
		return WakeUnknown
	}
}

func buildInstructionsForWakeTrigger(wakeTrigger WakeTrigger, agentID string, wakeData wakeTemplateData, planningTasks []PlanningTask, unseenNotes []models.HumanNote) (string, error) {
	agentData := wakeTemplateData{AgentID: agentID}
	switch wakeTrigger {
	case WakeHumanNote:
		return executeTemplate("wake_human_note", buildWakeHumanNoteData(unseenNotes))
	case WakeInitialPlanning:
		wakeData.AgentID = agentID
		return executeTemplate("wake_initial_planning", wakeData)
	case WakeBlockedTasks:
		agentData.BlockedScope = wakeData.BlockedScope
		return executeTemplate("wake_blocked_tasks", agentData)
	case WakeHypothesisExhausted:
		return executeTemplate("wake_hypothesis_exhausted", agentData)
	case WakeImmediateDiscovery:
		return executeTemplate("wake_immediate_discovery", agentData)
	case WakePlanningComplete:
		return executeTemplate("wake_planning_complete", buildWakePlanningCompleteData(agentID, wakeData.TestFilePatterns, planningTasks))
	case WakeManyToOneReady:
		return executeTemplate("wake_many_to_one_ready", agentData)
	case WakeCodingComplete:
		wakeData.AgentID = agentID
		return executeTemplate("wake_coding_complete", wakeData)
	case WakeIntegrationWaiting, WakeIntegrationBlocked, WakeIntegrationExhausted, WakeIntegrationUnavailable:
		return integrationOutcomeInstructions(wakeData.Integration), nil
	case WakeSprintComplete:
		return executeTemplate("wake_sprint_complete", agentData)
	default:
		return "", nil
	}
}

// WakeTriggers lists the wake triggers that select an orchestrator
// instruction template, in a stable order.
var WakeTriggers = []WakeTrigger{
	WakeInitialPlanning, WakeBlockedTasks, WakeHypothesisExhausted, WakeImmediateDiscovery,
	WakePlanningComplete, WakeManyToOneReady, WakeCodingComplete, WakeSprintComplete, WakeHumanNote,
}

// RenderWakeInstructions renders the instruction template for one wake
// trigger with empty run data and the given test-file matcher text, so the
// instruction text can be measured on its own. It is not the compositor's
// path; RenderOrchestratorDashboard is.
func RenderWakeInstructions(wakeTrigger WakeTrigger, agentID, testFilePatterns string) (string, error) {
	return buildInstructionsForWakeTrigger(wakeTrigger, agentID, wakeTemplateData{TestFilePatterns: testFilePatterns}, nil, nil)
}
