package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// A stable integration wait never wakes the orchestrator, so its poll is the
// live route that retries a pending deferred-validation batch (ADR-0204),
// including while a global analysis runs alongside it (D-230).
func TestOrchestratorWaitForWorkRetriesPendingDeferredValidation(t *testing.T) {
	for name, tc := range map[string]struct {
		reason    string
		requested bool
		want      bool
	}{
		"deferred validation pending":            {reason: ops.IntegrationProgressWaitingDeferredValidation, requested: true, want: true},
		"global analysis alongside the batch":    {reason: "global_analysis_pending", requested: true, want: true},
		"global analysis without a batch":        {reason: "global_analysis_pending"},
		"other integration wait without a batch": {reason: "integration_closure_pending"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())

			prevDetector, prevStart := orchestratorWaitForWorkDetector, orchestratorStartDeferredValidation
			t.Cleanup(func() { orchestratorWaitForWorkDetector, orchestratorStartDeferredValidation = prevDetector, prevStart })
			orchestratorWaitForWorkDetector = func(string, *models.State, []models.TaskStatus, map[string]bool, []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
				return OrchestratorWakeResult{Trigger: WakeTriggerIntegrationWaiting, Count: 1,
					Integration: prompts.EffectiveIntegrationCompletion{WakeTrigger: prompts.WakeTrigger(WakeTriggerIntegrationWaiting), Status: "waiting", ReasonCode: tc.reason, DeferredValidationRequested: tc.requested}}
			}
			var starts atomic.Int32
			orchestratorStartDeferredValidation = func(projectRoot string) (bool, error) {
				if projectRoot != root {
					t.Errorf("start root = %q, want %q", projectRoot, root)
				}
				starts.Add(1)
				return true, nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			s := &orchestratorStrategy{}
			hasWork, _ := s.WaitForWork(ctx, db.New(statePath), SupervisorConfig{ProjectRoot: root}, 50*time.Millisecond, 200*time.Millisecond)
			if hasWork {
				t.Fatal("a stable integration wait woke the orchestrator")
			}
			if got := starts.Load() > 0; got != tc.want {
				t.Fatalf("batch retried = %v (%d starts), want %v", got, starts.Load(), tc.want)
			}
		})
	}
}
