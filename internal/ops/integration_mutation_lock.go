package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/paths"
)

// The protected CAS and index-sync window is normally sub-second. Thirty
// seconds absorbs a queue of concurrent merges while bounding cold-cache or
// unusually large checkouts; a timeout remains a retryable merge error.
const integrationMutationLockTimeout = 30 * time.Second

func withIntegrationMutationLock(projectRoot, operation string, fn func() error) error {
	return withIntegrationMutationLockTimeout(projectRoot, operation, integrationMutationLockTimeout, fn)
}

func withIntegrationMutationLockTimeout(projectRoot, operation string, timeout time.Duration, fn func() error) error {
	lock, err := projectFileLock(projectRoot, "integration-mutation")
	if err != nil {
		return err
	}

	// Lock ordering is integration mutation lock -> blackboard read lock.
	// Callers must release this lock before any blackboard state write.
	err = lock.WithTimeout(timeout).WithLockOperation(operation, fn)
	if filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		return fmt.Errorf("integration mutation lock operation %q could not acquire the lock within %s; another merge is updating the integration ref or main index; retry the merge: %w", operation, timeout, err)
	}
	return err
}

type cleanIntegrationSourceVerification struct {
	SourceCommit    string
	IntegrationHEAD string
	Effective       bool
}

// verifyCleanIntegrationSource compares a prospective clean analysis source
// with the live integration ref under the same lock order used by ref mutations.
// Callers persist any resulting projection only after this function returns.
func verifyCleanIntegrationSource(projectRoot, sourceCommit string) (cleanIntegrationSourceVerification, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	gitWrapper := git.New(projectRoot)
	verification := cleanIntegrationSourceVerification{SourceCommit: sourceCommit}
	err := withIntegrationMutationLock(projectRoot, "verify clean integration source", func() error {
		state, readErr := bb.Read()
		if readErr != nil {
			return fmt.Errorf("failed to read state for clean integration verification: %w", readErr)
		}
		integrationBranch := state.Config.IntegrationBranch
		if integrationBranch == "" {
			integrationBranch = "main"
		}
		integrationHEAD, headErr := gitWrapper.GetCommitSHA("refs/heads/" + integrationBranch)
		if headErr != nil {
			return fmt.Errorf("failed to read live integration HEAD: %w", headErr)
		}
		verification.IntegrationHEAD = integrationHEAD
		verification.Effective = sourceCommit != "" && sourceCommit == integrationHEAD
		return nil
	})
	return verification, err
}
