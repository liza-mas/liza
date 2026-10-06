package semble

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

// RepositoryContentMode keeps every query on Semble's single per-root cache slot.
// Switching content selections replaces that slot rather than warming another.
const RepositoryContentMode = "all"

// RepositoryPreparationTimeout allows a cold corpus to build outside agent turns.
const RepositoryPreparationTimeout = 10 * time.Minute

// PrepareRepository builds the actual repository corpus offline. The operator's
// model-download prewarm remains a separate, controlled fixture operation.
func PrepareRepository(opts ValidationOptions) ValidationResult {
	if opts.Timeout == 0 {
		opts.Timeout = RepositoryPreparationTimeout
	}
	return queryRepository(opts, true)
}

// CheckRepositoryReadiness establishes point-in-time readiness with a bounded
// actual-corpus query. It does not cache success across source or cache changes.
func CheckRepositoryReadiness(opts ValidationOptions) ValidationResult {
	if opts.Timeout == 0 {
		opts.Timeout = SembleValidationTimeout
	}
	return queryRepository(opts, false)
}

func queryRepository(opts ValidationOptions, waitForLock bool) (result ValidationResult) {
	if !RuntimeEnabled() {
		return ValidationResult{}
	}
	result.Enabled = true
	safety := ValidateTargetSafety(TargetSafetyOptions{Kind: TargetKindProjectRoot, TargetRoot: opts.TargetRoot})
	if !safety.Safe {
		result.Diagnostic = safety.Diagnostic
		return result
	}
	plans := PlanCommands(CommandPlanOptions{FixtureDir: safety.TargetRoot, LookPath: opts.LookPath, Timeout: opts.Timeout})
	if len(plans.Diagnostics) > 0 {
		result.Diagnostic = plans.Diagnostics[0]
		return result
	}
	lockPath, err := repositoryLockPath(safety.TargetRoot)
	if err != nil {
		result.Diagnostic = repositoryDiagnostic("resolve corpus lock")
		return result
	}
	lock := filelock.New(lockPath)
	deadline := time.Now().Add(opts.Timeout)
	held, acquired, err := lock.TryHold("semble repository query")
	// A readiness probe may time out after discovering a changed corpus. Keep
	// the coordinator's preparation request alive until that probe releases.
	// Lock waiting and the eventual query share one preparation budget.
	for waitForLock && err == nil && !acquired {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			result.Diagnostic = repositoryDiagnostic("corpus lock timed out")
			return result
		}
		time.Sleep(min(filelock.LockCheckInterval, remaining))
		held, acquired, err = lock.TryHold("semble repository preparation")
		opts.Timeout = time.Until(deadline)
	}
	if err != nil || !acquired {
		result.Diagnostic = repositoryDiagnostic("corpus preparation unavailable or already running")
		return result
	}
	defer func() {
		if err := held.Release(); err != nil {
			result.Ready = false
			result.Diagnostic = repositoryDiagnostic("release corpus lock")
		}
	}()

	plan := plans.OfflineValidation
	if opts.Timeout <= 0 {
		result.Diagnostic = repositoryDiagnostic("corpus lock timed out")
		return result
	}
	plan.Timeout = opts.Timeout
	plan.Args = []string{"search", prewarmQuery, safety.TargetRoot, "--top-k", "1", "--content", RepositoryContentMode}
	plan.Fixture = FixtureIdentity{Query: prewarmQuery, TopK: 1, ContentMode: RepositoryContentMode}
	var commandResult CommandResult
	if opts.Runner != nil {
		commandResult, err = opts.Runner(plan)
	} else {
		var files []*os.File
		if file := held.File(); file != nil {
			files = append(files, file)
		}
		commandResult, err = runCommandPlanWithFiles(plan, files)
	}
	if err == nil && commandResult.ExitCode == 0 {
		result.Ready = true
		return result
	}
	result.Diagnostic = repositoryDiagnostic("query failed")
	if errors.Is(err, context.DeadlineExceeded) {
		result.Diagnostic = repositoryDiagnostic("query timed out")
	}
	return result
}

// Keep the lock outside the corpus and independent of process/cache lifetime.
// Resolve symlinks to match Semble's root identity. Never remove the lock file.
func repositoryLockPath(root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(resolved))
	return filepath.Join(os.TempDir(), fmt.Sprintf("semble-repository-%x", digest)), nil
}

// Corpus stdout/stderr can contain source chunks; only generic facts are safe.
func repositoryDiagnostic(reason string) Diagnostic {
	return Diagnostic{Kind: DiagnosticExecutionFailure, Message: "semble repository: " + reason}
}
