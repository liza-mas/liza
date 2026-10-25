package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// ValidationSession is the immutable environment the supervisor will pass to the
// provider. A nil session requires proof that this command is inside the current
// registered provider session; an operator cannot attest another process's env.
type ValidationSession struct {
	Environment      []string
	Execution        string
	PreparationError error
	ToolName         string
	ConfigDigest     string
	ForceCheck       bool
}

// ValidationConfigDigest binds a prepared context to the runtime configuration.
// It is an internal comparison token and contains no configuration values.
func ValidationConfigDigest(config models.Config) string {
	payload, _ := json.Marshal(struct {
		Tools    map[string]models.AgentToolConfig
		Profiles map[string]models.AgentProfileConfig
		Codex    string
		Deny     []string
	}{config.AgentTools, config.AgentProfiles, config.CodexPackageVersion, config.AgentEnvDeny})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type validationFailure struct{ code validationCode }

func (e *validationFailure) Error() string {
	if e.code == validationCodeArtifactUnsupported {
		return "validation preflight: artifact_unsupported: validation artifacts are not implemented in this version"
	}
	return "validation preflight: " + string(e.code)
}
func (e *validationFailure) Unwrap() error { return sessionvalidation.ErrPreflight }

func validationError(code validationCode) error { return &validationFailure{code: code} }

// ValidationPreflight binds one observation to state and filesystem identities.
// It never stores environment values or process output.
type ValidationPreflight struct {
	projectRoot, worktree, agentID, reviewCommit, integrationBranch, policy string
	toolName, configDigest                                                  string
	taskStatus                                                              models.TaskStatus
	handoffPending                                                          bool
	assignedTo, reviewingBy, baseCommit, taskWorktree                       string
	resolver                                                                models.PipelineResolver
	record                                                                  models.ValidationReadiness
}

func (p *ValidationPreflight) SessionScope() string {
	if p == nil {
		return ""
	}
	h := sha256.Sum256([]byte(sessionvalidation.Domain() + "\x00" + p.projectRoot + "\x00" + p.worktree + "\x00" + p.agentID + "\x00" + p.record.Generation + "\x00" + p.record.TaskID + "\x00" + p.record.Commit + "\x00" + p.record.IntegrationSHA + "\x00" + p.record.Digest + "\x00" + p.record.Fingerprint + "\x00" + p.toolName + "\x00" + p.configDigest))
	return hex.EncodeToString(h[:])
}

// CheckCurrent runs in the final assignment/start transaction. No prerequisite
// probes run under the blackboard lock; identity rechecks include git rev-parse.
func (p *ValidationPreflight) CheckCurrent(state *models.State) error {
	if p == nil {
		return nil
	}
	if err := RequireAgentAuthority(state, models.AgentAuthority{ID: p.agentID, Generation: p.record.Generation}); err != nil {
		return err
	}
	task := state.FindTask(p.record.TaskID)
	if task == nil || models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites) != p.record.Digest || optionalValue(task.ReviewCommit) != p.reviewCommit || state.Config.IntegrationBranch != p.integrationBranch || state.Config.AgentTools[p.toolName].ValidationExecution != p.policy || ValidationConfigDigest(state.Config) != p.configDigest {
		return validationError(validationCodeContextChanged)
	}
	if task.Status != p.taskStatus || task.HandoffPending != p.handoffPending || optionalValue(task.AssignedTo) != p.assignedTo || optionalValue(task.ReviewingBy) != p.reviewingBy || optionalValue(task.BaseCommit) != p.baseCommit || optionalValue(task.Worktree) != p.taskWorktree {
		return validationError(validationCodeOwnershipChanged)
	}
	if task.Worktree != nil && filepath.Clean(filepath.Join(p.projectRoot, *task.Worktree)) != p.worktree {
		return validationError(validationCodeContextChanged)
	}
	git := gitpkg.New(p.projectRoot)
	head, err := gitpkg.New(p.worktree).GetCommitSHA("HEAD")
	if err != nil || head != p.record.Commit {
		return validationError(validationCodeCommitChanged)
	}
	integration, err := git.GetCommitSHA(p.integrationBranch)
	if err != nil || integration != p.record.IntegrationSHA {
		return validationError(validationCodeIntegrationChanged)
	}
	return nil
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// CheckLaunchCurrent additionally requires executable task ownership, excluding
// a reviewer's passive WAITING reservation. Claim transactions use CheckCurrent
// before assignment; launch transactions use this stronger gate after assignment.
func (p *ValidationPreflight) CheckLaunchCurrent(state *models.State) error {
	if err := p.CheckCurrent(state); err != nil {
		return err
	}
	if p == nil {
		return nil
	}
	task := state.FindTask(p.record.TaskID)
	role := state.Agents[p.agentID].Role
	doer, err := p.resolver.DoerRole(task.RolePair)
	if err == nil && role == doer {
		executing, err := p.resolver.ExecutingStatus(task.RolePair)
		if err == nil && task.Status == executing && !task.HandoffPending && optionalValue(task.AssignedTo) == p.agentID {
			return nil
		}
	}
	reviewer, err := p.resolver.ReviewerRole(task.RolePair)
	if err == nil && role == reviewer && optionalValue(task.ReviewingBy) == p.agentID {
		active, _, err := ResolveReviewerReleaseStatus(task, p.resolver)
		if err == nil && active != "" && task.Status == active {
			return nil
		}
	}
	return validationError(validationCodeExecutableOwnershipRequired)
}

var validationFailures = struct {
	sync.Mutex
	until map[string]time.Time
}{until: make(map[string]time.Time)}

func validationFailureCoolingDown(key string, now time.Time) bool {
	validationFailures.Lock()
	defer validationFailures.Unlock()
	for k, until := range validationFailures.until {
		if !until.After(now) {
			delete(validationFailures.until, k)
		}
	}
	return validationFailures.until[key].After(now)
}

// ValidationRetryPending lets automatic selection try unrelated work during an
// unchanged failure's cooldown. It consults only this process's failure map;
// persisted fingerprints are never used as cross-process equality evidence.
func ValidationRetryPending(projectRoot string, state *models.State, task *models.Task, agentID string, session *ValidationSession) bool {
	if state == nil || task == nil || session == nil || session.ForceCheck || len(task.ValidationPrerequisites) == 0 {
		return false
	}
	agent, ok := state.Agents[agentID]
	if !ok {
		return false
	}
	g := gitpkg.New(projectRoot)
	worktree := g.GetWorktreePath(task.ID)
	if task.Worktree != nil && *task.Worktree != "" {
		worktree = *task.Worktree
		if !filepath.IsAbs(worktree) {
			worktree = filepath.Join(projectRoot, worktree)
		}
	}
	head, err := gitpkg.New(worktree).GetCommitSHA("HEAD")
	if err != nil {
		return false
	}
	integration, err := g.GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return false
	}
	tool := session.ToolName
	if tool == "" {
		tool = agent.Provider
		if profile, ok := state.Config.AgentProfiles[tool]; ok {
			tool = profile.CLI
		}
	}
	p := &ValidationPreflight{projectRoot: projectRoot, worktree: filepath.Clean(worktree), agentID: agentID, toolName: tool, configDigest: ValidationConfigDigest(state.Config), record: models.ValidationReadiness{TaskID: task.ID, Generation: agent.Generation, Commit: head, IntegrationSHA: integration, Digest: models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites), Fingerprint: sessionvalidation.Fingerprint(session.Environment)}}
	return validationFailureCoolingDown(p.SessionScope()+"\x00"+session.Execution, time.Now())
}

// PrepareValidationPreflightContext is PrepareValidationPreflight for a
// supervisor about to launch a provider: its initial state read waits
// patiently for the state lock and aborts when ctx is canceled. Claim paths,
// including the CLI, keep the ordinary wait of PrepareValidationPreflight.
func PrepareValidationPreflightContext(ctx context.Context, projectRoot, taskID, agentID, worktree string, session *ValidationSession) (*ValidationPreflight, error) {
	return prepareValidationPreflight(projectRoot, taskID, agentID, worktree, session, func(bb *db.Blackboard) (*models.State, *models.Task, error) {
		return readTaskStatePatient(ctx, bb, taskID)
	})
}

// PrepareValidationPreflight always reruns successful checks. Only an unchanged
// failure in this producing process has a bounded retry delay.
func PrepareValidationPreflight(projectRoot, taskID, agentID, worktree string, session *ValidationSession) (*ValidationPreflight, error) {
	return prepareValidationPreflight(projectRoot, taskID, agentID, worktree, session, func(bb *db.Blackboard) (*models.State, *models.Task, error) {
		return readTaskState(bb, taskID)
	})
}

func prepareValidationPreflight(projectRoot, taskID, agentID, worktree string, session *ValidationSession, readTask func(*db.Blackboard) (*models.State, *models.Task, error)) (*ValidationPreflight, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	state, task, err := readTask(bb)
	if err != nil {
		return nil, err
	}
	// Runtime inputs come from the ledger, not the session, so their
	// readiness needs neither prerequisites nor an execution policy.
	if err := checkAgentRuntimeInputReadiness(projectRoot, bb, state, task, agentID); err != nil {
		return nil, err
	}
	if len(task.ValidationPrerequisites) == 0 {
		if session != nil {
			return nil, session.PreparationError
		}
		return nil, nil
	}
	p, agent, err := snapshotValidationPreflight(projectRoot, taskID, agentID, worktree, session, state, task)
	if err != nil {
		return nil, err
	}
	// Without a supervisor session the check runs inside a provider tool call.
	inSession := session == nil
	session, checkErr := p.observeSession(agent, session)
	key := p.SessionScope() + "\x00" + session.Execution
	if checkErr == nil && !session.ForceCheck && validationFailureCoolingDown(key, time.Now()) {
		return nil, validationError(validationCodeRetryPending)
	}
	if checkErr == nil {
		checkErr = p.probePrerequisites(state, task, session, inSession)
	}
	p.recordObservation(checkErr)
	newFailure, err := p.recordReadiness(bb)
	if err != nil {
		return nil, err
	}
	if newFailure {
		writeValidationPreflightAlert(projectRoot, agentID, p.record)
	}
	rememberValidationOutcome(key, checkErr)
	if checkErr != nil {
		return nil, checkErr
	}
	return p, nil
}

// snapshotValidationPreflight binds the task, registration, worktree and
// integration identities the preflight observes, and returns the registered
// agent. It runs Git reads but takes no lock.
func snapshotValidationPreflight(projectRoot, taskID, agentID, worktree string, session *ValidationSession, state *models.State, task *models.Task) (*ValidationPreflight, models.Agent, error) {
	agent, ok := state.Agents[agentID]
	if !ok || agent.Generation == "" {
		return nil, agent, validationError(validationCodeRegistrationRequired)
	}
	if worktree == "" {
		if task.Worktree == nil || *task.Worktree == "" {
			return nil, agent, validationError(validationCodeWorktreeUnavailable)
		}
		worktree = *task.Worktree
	}
	if !filepath.IsAbs(worktree) {
		worktree = filepath.Join(projectRoot, worktree)
	}
	worktree = filepath.Clean(worktree)
	p := &ValidationPreflight{projectRoot: projectRoot, worktree: worktree, agentID: agentID, reviewCommit: optionalValue(task.ReviewCommit), integrationBranch: state.Config.IntegrationBranch, policy: state.Config.AgentTools[agent.Provider].ValidationExecution}
	p.taskStatus = task.Status
	p.handoffPending = task.HandoffPending
	p.assignedTo = optionalValue(task.AssignedTo)
	p.reviewingBy = optionalValue(task.ReviewingBy)
	p.baseCommit = optionalValue(task.BaseCommit)
	p.taskWorktree = optionalValue(task.Worktree)
	p.toolName = agent.Provider
	if profile, ok := state.Config.AgentProfiles[p.toolName]; ok {
		p.toolName = profile.CLI
		p.policy = state.Config.AgentTools[p.toolName].ValidationExecution
	}
	var err error
	p.resolver, err = LoadResolverForModels(projectRoot)
	if err != nil {
		return nil, agent, validationError(validationCodePipelineUnavailable)
	}
	p.configDigest = ValidationConfigDigest(state.Config)
	if session != nil && session.ToolName != "" {
		p.toolName = session.ToolName
		p.policy = state.Config.AgentTools[p.toolName].ValidationExecution
	}
	p.record = models.ValidationReadiness{Generation: agent.Generation, TaskID: taskID, Digest: models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites), Method: string(validationMethodDirect), CheckedAt: time.Now().UTC()}
	p.record.Commit, err = gitpkg.New(worktree).GetCommitSHA("HEAD")
	p.record.ReviewCommit = p.reviewCommit
	if err != nil {
		return nil, agent, validationError(validationCodeWorktreeUnavailable)
	}
	p.record.IntegrationSHA, err = gitpkg.New(projectRoot).GetCommitSHA(p.integrationBranch)
	if err != nil {
		return nil, agent, validationError(validationCodeIntegrationUnavailable)
	}
	return p, agent, nil
}

// observeSession resolves the session whose environment is checked and records
// its fingerprint. A nil session means the check runs inside a provider tool
// call, so the caller must prove it is the registered agent's current session;
// the environment it then checks is the process's own. The returned error is a
// refusal to probe, which the readiness record still captures.
func (p *ValidationPreflight) observeSession(agent models.Agent, session *ValidationSession) (*ValidationSession, error) {
	var checkErr error
	if session != nil && session.ConfigDigest != "" && session.ConfigDigest != p.configDigest {
		checkErr = validationError(validationCodeConfigurationChanged)
	}
	if session == nil {
		id := brand.LookupEnv(os.Getenv, agentIDEnvSuffix).Value
		generation := brand.LookupEnv(os.Getenv, agentGenerationEnvSuffix).Value
		if id != p.agentID || generation == "" || generation != agent.Generation {
			checkErr = validationError(validationCodeCurrentSessionRequired)
		}
		session = &ValidationSession{Environment: os.Environ(), Execution: p.policy, ForceCheck: true}
	}
	p.record.Fingerprint = sessionvalidation.Fingerprint(session.Environment)
	if session.PreparationError != nil {
		checkErr = session.PreparationError
	}
	return session, checkErr
}

// probePrerequisites applies the execution policy and runs the task's declared
// prerequisite probes against the session environment, outside any lock.
func (p *ValidationPreflight) probePrerequisites(state *models.State, task *models.Task, session *ValidationSession, inSession bool) error {
	if session.Execution != validationExecutionLocal {
		if session.Execution == validationExecutionArtifactOnly {
			return validationError(validationCodeArtifactUnsupported)
		}
		return validationError(validationCodeExecutionPolicyRequired)
	}
	if session.Execution != p.policy {
		return validationError(validationCodeExecutionPolicyRequired)
	}
	probeTimeout, totalTimeout := models.EffectiveValidationPreflightLimits(state.Config, inSession)
	checkErr := sessionvalidation.Check(context.Background(), task.Validation, task.ValidationPrerequisites, p.worktree, append([]string(nil), session.Environment...), probeTimeout, totalTimeout)
	var missing *sessionvalidation.Error
	if errors.As(checkErr, &missing) && missing.Code == sessionvalidation.CodeEnvironmentMissing && models.RuntimeInputDenyNames(state)[missing.Variable] {
		// A legacy prerequisite demands a name reserved for runtime inputs,
		// which every launch strips (ADR-0169): name the cause.
		missing.Code = sessionvalidation.CodeRuntimeInputScrubbed
	}
	return checkErr
}

// rememberValidationOutcome starts the bounded retry delay of an unchanged
// failure in this process, or clears it after a pass.
func rememberValidationOutcome(key string, checkErr error) {
	validationFailures.Lock()
	defer validationFailures.Unlock()
	if checkErr != nil {
		validationFailures.until[key] = time.Now().Add(models.ValidationRetryInterval)
		return
	}
	delete(validationFailures.until, key)
}

func validationSession(sessions []*ValidationSession) *ValidationSession {
	if len(sessions) == 0 {
		return nil
	}
	return sessions[0]
}

// prepareResumedValidation runs setup before session probes and before renewed
// executable ownership. Legacy setup remains in the supervisor for compatibility.
func prepareResumedValidation(projectRoot, taskID, agentID, worktree string, session *ValidationSession, authority *models.AgentAuthority) (*ValidationPreflight, error) {
	state, task, err := readTaskState(db.For(paths.New(projectRoot).StatePath()), taskID)
	if err != nil {
		return nil, err
	}
	if len(task.ValidationPrerequisites) == 0 {
		if session != nil {
			return nil, session.PreparationError
		}
		return nil, nil
	}
	if task.Worktree == nil || *task.Worktree == "" || worktree == "" {
		return nil, validationError(validationCodeWorktreeUnavailable)
	}
	if !filepath.IsAbs(worktree) {
		worktree = filepath.Join(projectRoot, worktree)
	}
	if state.Config.PostWorktreeCmd != nil {
		if err := RunPostWorktreeCmd(projectRoot, *state.Config.PostWorktreeCmd, worktree, models.EffectivePostWorktreeCmdTimeout(state.Config), authority); err != nil {
			if degraded := degradeOnPostWorktreeSetupFailure(projectRoot, taskID, agentID, authority, err); degraded != nil {
				return nil, degraded
			}
			return nil, err
		}
	}
	return PrepareValidationPreflight(projectRoot, taskID, agentID, worktree, session)
}

// ReleaseValidationOwnership preserves artifacts and releases only this agent's
// executable claim. A paired reviewer awaiting resubmission stays reserved.
func ReleaseValidationOwnership(projectRoot, taskID, agentID string, authority *models.AgentAuthority) error {
	bb := db.For(paths.New(projectRoot).StatePath())
	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return err
	}
	return lifecycleMutation(bb, authority)(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return nil
		}
		releasedOwnership := false
		if task.AssignedTo != nil && *task.AssignedTo == agentID {
			executing, e := pb.resolver.ExecutingStatus(task.RolePair)
			if e != nil {
				return e
			}
			if task.Status == executing {
				initial, e := pb.resolver.InitialStatus(task.RolePair)
				if e != nil {
					return e
				}
				task.Status = initial
			}
			task.AssignedTo = nil
			task.LeaseExpires = nil
			task.HandoffPending = false
			releasedOwnership = true
		}
		if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			active, released, e := ResolveReviewerReleaseStatus(task, pb.pr)
			if e != nil {
				return e
			}
			if task.Status == active && released != "" {
				task.Status = released
			}
			task.ReviewingBy = nil
			task.ReviewLeaseExpires = nil
			releasedOwnership = true
		}
		if releasedOwnership {
			models.AdvanceLifecycle(task)
		}
		if agent, ok := state.Agents[agentID]; ok && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			state.ReleaseAgent(agentID)
		}
		return nil
	})
}

// runtimeInputPreflightError is a failed runtime-input readiness check. It
// classifies as a preflight failure and names the operator action.
type runtimeInputPreflightError struct{ refusal *AcceptanceEvidenceError }

func (e *runtimeInputPreflightError) Error() string {
	return "validation preflight: " + string(runtimeInputCodeUnavailable) + ": " + e.refusal.Error()
}

func (e *runtimeInputPreflightError) Unwrap() error { return sessionvalidation.ErrPreflight }

// checkAgentRuntimeInputReadiness applies role-aware runtime-input readiness
// before an executable claim or launch: the task's reviewer role needs only
// reusable inputs, any other agent every input. A failure records the
// deduplicated operator anomaly.
func checkAgentRuntimeInputReadiness(projectRoot string, bb *db.Blackboard, state *models.State, task *models.Task, agentID string) error {
	if len(task.RuntimeInputs) == 0 {
		return nil
	}
	refusals := runtimeInputReadinessRefusals(projectRoot, state, task, runtimeInputReviewerAgent(projectRoot, task, agentID))
	if len(refusals) == 0 {
		return nil
	}
	_ = bb.Modify(func(current *models.State) error {
		now := time.Now().UTC()
		for _, refusal := range refusals {
			appendRuntimeInputAnomaly(current, task.ID, refusal.declaration.ID, refusal.code, runtimeInputPreflightOperation, refusal.bound, agentID, now)
		}
		return nil
	})
	return &runtimeInputPreflightError{refusal: runtimeInputRefusalError(task.ID, refusals, nil)}
}

// runtimeInputReviewerAgent reports whether agentID acts as the task's
// reviewer: it holds the review, or its role is the pair's reviewer role.
func runtimeInputReviewerAgent(projectRoot string, task *models.Task, agentID string) bool {
	if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
		return true
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil || task.RolePair == "" {
		return false
	}
	pair, err := resolver.RolePair(task.RolePair)
	role, roleErr := identity.ExtractRole(agentID)
	return err == nil && roleErr == nil && role == pair.Reviewer
}
