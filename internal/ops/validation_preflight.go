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
	}{config.AgentTools, config.AgentProfiles, config.CodexPackageVersion})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type validationFailure struct{ code string }

func (e *validationFailure) Error() string {
	if e.code == "artifact_unsupported" {
		return "validation preflight: artifact_unsupported: validation artifacts are not implemented in this version"
	}
	return "validation preflight: " + e.code
}
func (e *validationFailure) Unwrap() error { return sessionvalidation.ErrPreflight }

func validationError(code string) error { return &validationFailure{code: code} }

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
		return validationError("context_changed")
	}
	if task.Status != p.taskStatus || task.HandoffPending != p.handoffPending || optionalValue(task.AssignedTo) != p.assignedTo || optionalValue(task.ReviewingBy) != p.reviewingBy || optionalValue(task.BaseCommit) != p.baseCommit || optionalValue(task.Worktree) != p.taskWorktree {
		return validationError("ownership_changed")
	}
	if task.Worktree != nil && filepath.Clean(filepath.Join(p.projectRoot, *task.Worktree)) != p.worktree {
		return validationError("context_changed")
	}
	git := gitpkg.New(p.projectRoot)
	head, err := gitpkg.New(p.worktree).GetCommitSHA("HEAD")
	if err != nil || head != p.record.Commit {
		return validationError("commit_changed")
	}
	integration, err := git.GetCommitSHA(p.integrationBranch)
	if err != nil || integration != p.record.IntegrationSHA {
		return validationError("integration_changed")
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
	return validationError("executable_ownership_required")
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

// PrepareValidationPreflight always reruns successful checks. Only an unchanged
// failure in this producing process has a bounded retry delay.
func PrepareValidationPreflight(projectRoot, taskID, agentID, worktree string, session *ValidationSession) (*ValidationPreflight, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if len(task.ValidationPrerequisites) == 0 {
		if session != nil {
			return nil, session.PreparationError
		}
		return nil, nil
	}
	agent, ok := state.Agents[agentID]
	if !ok || agent.Generation == "" {
		return nil, validationError("registration_required")
	}
	if worktree == "" {
		if task.Worktree == nil || *task.Worktree == "" {
			return nil, validationError("worktree_unavailable")
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
	p.resolver, err = LoadResolverForModels(projectRoot)
	if err != nil {
		return nil, validationError("pipeline_unavailable")
	}
	p.configDigest = ValidationConfigDigest(state.Config)
	if session != nil && session.ToolName != "" {
		p.toolName = session.ToolName
		p.policy = state.Config.AgentTools[p.toolName].ValidationExecution
	}
	p.record = models.ValidationReadiness{Generation: agent.Generation, TaskID: taskID, Digest: models.ValidationPrerequisiteDigest(task.Validation, task.ValidationPrerequisites), Method: "direct", CheckedAt: time.Now().UTC()}
	p.record.Commit, err = gitpkg.New(worktree).GetCommitSHA("HEAD")
	p.record.ReviewCommit = p.reviewCommit
	if err != nil {
		return nil, validationError("worktree_unavailable")
	}
	p.record.IntegrationSHA, err = gitpkg.New(projectRoot).GetCommitSHA(p.integrationBranch)
	if err != nil {
		return nil, validationError("integration_unavailable")
	}
	var checkErr error
	if session != nil && session.ConfigDigest != "" && session.ConfigDigest != p.configDigest {
		checkErr = validationError("configuration_changed")
	}
	if session == nil {
		id := brand.LookupEnv(os.Getenv, "AGENT_ID").Value
		generation := brand.LookupEnv(os.Getenv, "AGENT_GENERATION").Value
		if id != agentID || generation == "" || generation != agent.Generation {
			checkErr = validationError("current_session_required")
		}
		session = &ValidationSession{Environment: os.Environ(), Execution: p.policy, ForceCheck: true}
	}
	p.record.Fingerprint = sessionvalidation.Fingerprint(session.Environment)
	if session.PreparationError != nil {
		checkErr = session.PreparationError
	}
	key := p.SessionScope() + "\x00" + session.Execution
	if checkErr == nil && !session.ForceCheck && validationFailureCoolingDown(key, time.Now()) {
		return nil, validationError("retry_pending")
	}
	if checkErr == nil && session.Execution != "local" {
		if session.Execution == "artifact-only" {
			checkErr = validationError("artifact_unsupported")
		} else {
			checkErr = validationError("execution_policy_required")
		}
	}
	if checkErr == nil && session.Execution != p.policy {
		checkErr = validationError("execution_policy_required")
	}
	if checkErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		checkErr = sessionvalidation.Check(ctx, task.Validation, task.ValidationPrerequisites, worktree, append([]string(nil), session.Environment...))
		cancel()
	}
	p.record.Result = "passed"
	if checkErr != nil {
		p.record.Result = "failed"
		p.record.Code = "prerequisite_failed"
		var probe *sessionvalidation.Error
		var local *validationFailure
		if errors.As(checkErr, &probe) {
			p.record.Code = probe.Code
			p.record.CommandIndex = probe.CommandIndex
			p.record.CheckIndex = probe.CheckIndex
			p.record.Variable = probe.Variable
		}
		if errors.As(checkErr, &local) {
			p.record.Code = local.code
		}
	}
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
		current.ValidationReadiness[agentID][taskID] = p.record
		return nil
	})
	if err != nil {
		return nil, err
	}
	if checkErr != nil {
		validationFailures.Lock()
		validationFailures.until[key] = time.Now().Add(models.ValidationRetryInterval)
		validationFailures.Unlock()
		return nil, checkErr
	}
	validationFailures.Lock()
	delete(validationFailures.until, key)
	validationFailures.Unlock()
	return p, nil
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
		return nil, validationError("worktree_unavailable")
	}
	if !filepath.IsAbs(worktree) {
		worktree = filepath.Join(projectRoot, worktree)
	}
	if state.Config.PostWorktreeCmd != nil {
		if err := RunPostWorktreeCmd(*state.Config.PostWorktreeCmd, worktree); err != nil {
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
		}
		if agent, ok := state.Agents[agentID]; ok && agent.CurrentTask != nil && *agent.CurrentTask == taskID {
			state.ReleaseAgent(agentID)
		}
		return nil
	})
}
