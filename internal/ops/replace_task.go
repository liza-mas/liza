package ops

import (
	"fmt"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const replaceTaskOperation = "replace-task"

// PreservedTaskBase declares an existing claimable worktree and its base.
// Both fields are required: a base without a worktree is overwritten at claim.
type PreservedTaskBase struct {
	BaseCommit string `json:"base_commit"`
	Worktree   string `json:"worktree"`
}

// ReplaceTaskInput binds creation, consumer updates and retirement in one intent.
type ReplaceTaskInput struct {
	SourceTaskID  string                    `json:"source_task_id"`
	Reason        string                    `json:"reason"`
	Replacement   AddTaskInput              `json:"replacement"`
	Consumers     []models.DependencyUpdate `json:"consumers"`
	PreservedBase *PreservedTaskBase        `json:"preserved_base,omitempty"`
	// Changed states what differs from the blocked attempt. Required when the
	// source is BLOCKED (ADR-0171); recorded masked.
	Changed string `json:"changed,omitempty"`
}

// ReplaceTaskResult reports the committed replacement graph and source boundary.
type ReplaceTaskResult struct {
	models.LifecycleOutcome
	SourceTaskID         string            `json:"source_task_id"`
	ReplacementTaskID    string            `json:"replacement_task_id"`
	SourceOriginalStatus models.TaskStatus `json:"source_original_status"`
	RetargetedConsumers  []string          `json:"retargeted_consumers"`
	Warnings             []string          `json:"warnings,omitempty"`
}

// Per-blackboard barriers permit observing and rejecting a partially composed
// candidate in tests without changing the shared mutation/locking primitives.
var replaceTaskCandidateTestHooks sync.Map

type replaceTaskTestHooks struct {
	beforeLock   func()
	afterUpdates func(*models.State) error
}

func replaceTestHooks(bb *db.Blackboard) replaceTaskTestHooks {
	if hook, ok := replaceTaskCandidateTestHooks.Load(bb); ok {
		return hook.(replaceTaskTestHooks)
	}
	return replaceTaskTestHooks{}
}

// replacePass carries one replace-task request through its phases: admission
// before the lock, the locked commit, and the cleanup that follows persistence.
type replacePass struct {
	projectRoot string
	input       ReplaceTaskInput
	authority   models.AgentAuthority

	// Set by admit, before the lock.
	bb                  *db.Blackboard
	pb                  *pipelineBundle
	request             LifecycleRequest
	base                *PreservedTaskBase
	baseErr             error
	acceptance          acceptanceCreationCheck
	runtimeInputVerdict func(operation string, observed *models.Task) error

	// Set by the locked commit.
	observed    *models.Task
	hadWorktree bool
	result      *ReplaceTaskResult
}

// ReplaceTaskWithAuthorityAndOptions validates and commits one replacement under
// the source ownership lock and one generation-fenced state transaction. External
// worktree validation precedes locking; cleanup and telemetry follow persistence.
func ReplaceTaskWithAuthorityAndOptions(projectRoot string, input ReplaceTaskInput, authority models.AgentAuthority, opts LifecycleRequestOptions) (result *ReplaceTaskResult, retErr error) {
	if err := checkReplaceTaskRequest(input, opts); err != nil {
		return nil, err
	}
	invocation := NewLifecycleInvocation(projectRoot)
	pass := &replacePass{projectRoot: projectRoot, input: input, authority: authority}
	defer func() {
		retErr = WrapLifecycleError(replaceTaskOperation, pass.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(replaceTaskOperation, outcome, &retErr, warnings)
	}()
	if err := pass.admit(opts); err != nil {
		return nil, err
	}
	if hook := replaceTestHooks(pass.bb); hook.beforeLock != nil {
		hook.beforeLock()
	}
	err := withOwnershipTaskLock(projectRoot, input.SourceTaskID, replaceTaskOperation, func() error {
		return lifecycleMutation(pass.bb, &pass.authority)(pass.commit)
	})
	return pass.conclude(err)
}

// commit is the locked callback: it answers a retained receipt, decides the
// source and consumers, edits the candidate and projects the durable result.
func (p *replacePass) commit(candidate *models.State) (callbackErr error) {
	source := candidate.FindTask(p.input.SourceTaskID)
	if source == nil {
		return WrapLifecycleError(replaceTaskOperation, nil, fmt.Errorf("source task disappeared"), models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone)
	}
	// Preserve the durable observation: candidate mutation is not evidence
	// of a committed transition if any subsequent validation rejects it.
	observed := *source
	observed.Lifecycle = cloneTaskLifecycle(source.Lifecycle)
	p.observed = &observed
	defer func() {
		callbackErr = WrapLifecycleError(replaceTaskOperation, p.observed, callbackErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}()
	replayed, err := p.replayCompleted(source)
	if err != nil {
		return err
	}
	if replayed {
		return errLifecycleReplay
	}
	replacement, err := p.decide(candidate, source)
	if err != nil {
		return err
	}
	retired, audit, err := p.replace(candidate, replacement)
	if err != nil {
		return err
	}
	return p.project(candidate, retired, replacement.ID, audit)
}

// conclude maps the lock and callback outcome and, once the replacement is
// durable, runs the effects that must follow persistence.
func (p *replacePass) conclude(err error) (*ReplaceTaskResult, error) {
	if isLifecycleReplay(err) {
		return p.result, nil
	}
	if err != nil {
		// A nil callback followed by a persistence failure is uncertain, never
		// an INVALID_INPUT instruction to blindly rewrite the same intent.
		if p.result != nil {
			return nil, WrapLifecycleError(replaceTaskOperation, p.observed, err, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsUnknown)
		}
		return nil, err
	}
	p.cleanup()
	return p.result, nil
}

// cleanup removes the source worktree directory and its predecessor branches,
// then logs the activity; failures are warnings, never a failed replacement.
func (p *replacePass) cleanup() {
	gw := git.New(p.projectRoot)
	if p.hadWorktree {
		if err := gw.RemoveWorktreeDir(p.input.SourceTaskID); err != nil {
			p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("failed to remove source worktree directory: %v", err))
		}
	}
	// A replacement exists, so keep the source branch for successor salvage.
	p.result.Warnings = append(p.result.Warnings, cleanupPredecessorBranches(p.bb, gw, p.input.SourceTaskID)...)
	entry := log.Entry{Timestamp: time.Now().UTC(), Agent: p.authority.ID, Action: replaceTaskOperation, Task: &p.input.SourceTaskID, Detail: "replacement=" + p.input.Replacement.ID}
	if err := log.New(paths.New(p.projectRoot).LogPath()).Append(entry); err != nil {
		p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
}
