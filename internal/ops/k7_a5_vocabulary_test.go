package ops

import (
	"crypto/sha256"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// TestA5AcceptanceFaultKeepsWireSpelling pins the one A5-owned typed set no
// other test pinned: the fault class is persisted into the claim-block
// anomaly (faultClassExtraKey) and logged by the supervisor, so each value is
// compared with the literal it carries on the wire, independently of the
// constant.
func TestA5AcceptanceFaultKeepsWireSpelling(t *testing.T) {
	t.Parallel()
	for want, got := range map[string]AcceptanceFault{
		"content":        AcceptanceFaultContent,
		"allocation":     AcceptanceFaultAllocation,
		"runtime_input":  AcceptanceFaultRuntimeInput,
		"admission_slot": AcceptanceFaultAdmissionSlot,
		"batch_timeout":  AcceptanceFaultBatchTimeout,
	} {
		if string(got) != want {
			t.Errorf("AcceptanceFault %q = %q", want, got)
		}
	}
}

// TestA5NamedValuesKeepTheirSpelling pins each value the K7 sweep named, against
// the literal it replaced. Lock labels and ID formats name files and persisted
// IDs; operation names reach role capabilities and refusal text.
func TestA5NamedValuesKeepTheirSpelling(t *testing.T) {
	t.Parallel()
	for name, pair := range map[string][2]string{
		"reconcile operation":          {reconcileVerdictOperation, "reconcile-verdict"},
		"task review lock key format":  {taskReviewLockKeyFormat, "task-review-%x"},
		"quarantined verdict ID":       {quarantinedVerdictIDFormat, "qv-%x"},
		"refuted disposition":          {reconcileDispositionRefuted, "refuted"},
		"superseded disposition":       {reconcileDispositionSuperseded, "superseded"},
		"reaffirm operation":           {reaffirmProofOperation, "reaffirm-proof"},
		"await verdict operation":      {awaitVerdictOperation, "await-verdict"},
		"archive lock operation":       {archiveAcceptanceReceiptsLockOperation, "archive-acceptance-receipts"},
		"publish pending lock":         {publishPendingVerdictLockOperation, "publish-pending-verdict"},
		"remove pending lock":          {removePendingVerdictLockOperation, "remove-pending-verdict"},
		"pending verdict extension":    {pendingVerdictFileExt, ".json"},
		"pending verdict file name":    {pendingVerdictFileNameFormat, "%x.json"},
		"pending verdict request ID":   {pendingVerdictRequestIDFormat, "verdict-%x"},
		"in-flight marker extension":   {inflightSubmitFileExt, ".json"},
		"submit finalize lock":         {submitFinalizeLockOperation, "submit-for-review-finalize"},
		"agent ID env suffix":          {agentIDEnvSuffix, "AGENT_ID"},
		"agent generation env suffix":  {agentGenerationEnvSuffix, "AGENT_GENERATION"},
		"connection URL suffix":        {acceptanceEnvSuffixURL, "URL"},
		"connection DSN suffix":        {acceptanceEnvSuffixDSN, "DSN"},
		"preflight anomaly operation":  {runtimeInputPreflightOperation, "preflight"},
		"A3 not-found task entity":     {notFoundEntityTask, "task"},
		"A3 not-found agent entity":    {notFoundEntityAgent, "agent"},
		"A3 system actor":              {actorSystem, "system"},
		"credential mask replacement":  {acceptanceMaskedValue, "***"},
		"claim operation (A5 adopted)": {claimReviewerTaskOperation, "claim-reviewer-task"},
		"attempt transition marker":    {attemptTransitionReasonMarker, "transitioned to attempt"},
		"reference anchor separator":   {referenceAnchorSeparator, "#"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
	if orphanedBaseHealCauseLimit != 500 {
		t.Errorf("orphanedBaseHealCauseLimit = %d, want 500", orphanedBaseHealCauseLimit)
	}
	if defaultSubmitHeartbeatInterval != 5*time.Minute {
		t.Errorf("defaultSubmitHeartbeatInterval = %v, want 5m", defaultSubmitHeartbeatInterval)
	}
}

// TestA5ReconcileDispositionsAreOwnerAccepted ties the two names to the
// owner's validation: models.IsVerdictDisposition is the set ReconcileVerdict
// accepts, so a spelling that drifted from it would stop releasing holds.
func TestA5ReconcileDispositionsAreOwnerAccepted(t *testing.T) {
	t.Parallel()
	for _, disposition := range []string{reconcileDispositionRefuted, reconcileDispositionSuperseded} {
		if !models.IsVerdictDisposition(disposition) {
			t.Errorf("models.IsVerdictDisposition(%q) = false", disposition)
		}
	}
}

// TestA5ComposedTextKeepsItsBytes covers the sites where a constant now builds
// persisted or user-visible text, so the composed bytes must equal the old
// literal.
func TestA5ComposedTextKeepsItsBytes(t *testing.T) {
	t.Parallel()
	if got, want := reaffirmProofOperation+" requires a registered orchestrator with the "+reaffirmProofOperation+" capability",
		"reaffirm-proof requires a registered orchestrator with the reaffirm-proof capability"; got != want {
		t.Errorf("reaffirm refusal = %q, want %q", got, want)
	}
	sum := sha256.Sum256([]byte("envelope"))
	digest := hex.EncodeToString(sum[:])
	if got, want := fmt.Sprintf(pendingVerdictFileNameFormat, sum), digest+".json"; got != want {
		t.Errorf("pending verdict file name = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf(pendingVerdictRequestIDFormat, sum), "verdict-"+digest; got != want {
		t.Errorf("pending verdict request ID = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf(quarantinedVerdictIDFormat, sum), "qv-"+digest; got != want {
		t.Errorf("quarantined verdict ID = %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf(taskReviewLockKeyFormat, sum), "task-review-"+digest; got != want {
		t.Errorf("task review lock key = %q, want %q", got, want)
	}
	if got, want := brand.EnvName(agentIDEnvSuffix), brand.EnvName("AGENT_ID"); got != want {
		t.Errorf("agent ID variable = %q, want %q", got, want)
	}
}

// TestA5AttemptTransitionMarkerIsInTheClaimRefusal pins the coupling the await
// depends on: reclaimFailureResult recognises a new-attempt refusal of the
// doer's auto-reclaim by this marker in the claim's PreconditionError reason,
// so the real producer (an exhausted-limits claim) must keep containing it. If
// the text drifted, the branch would silently fall through to the history
// recovery instead of failing, so the producer is exercised here.
func TestA5AttemptTransitionMarkerIsInTheClaimRefusal(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	registerClaimTaskTestAgents(state)
	state.Config.MaxCoderIterations = 3
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusRejected, now)
	task.Iteration = 3
	task.Attempt = 1
	state.Tasks = []models.Task{task}
	taskRef := "task-1"
	agent := testhelpers.RegisteredTestAgent("coder")
	agent.Status = models.AgentStatusWorking
	agent.CurrentTask = &taskRef
	agent.Heartbeat = now
	state.Agents["coder-1"] = agent
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := ClaimTask(tmpDir, "task-1", "coder-1")
	var refusal *PreconditionError
	if !stderrors.As(err, &refusal) {
		t.Fatalf("ClaimTask error = %T %v, want a PreconditionError", err, err)
	}
	if !strings.Contains(refusal.Reason, attemptTransitionReasonMarker) {
		t.Errorf("refusal reason does not contain the await marker %q", attemptTransitionReasonMarker)
	}
}
