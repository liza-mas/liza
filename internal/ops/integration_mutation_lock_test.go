package ops

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const (
	integrationMutationLockHelperEnv = "TEST_INTEGRATION_MUTATION_LOCK_HELPER"
	integrationMutationLockRootEnv   = "TEST_INTEGRATION_MUTATION_LOCK_ROOT"
)

func TestIntegrationMutationLockCrossProcess(t *testing.T) {
	if os.Getenv(integrationMutationLockHelperEnv) == "1" {
		runIntegrationMutationLockHelper(t)
		return
	}

	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)

	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- withIntegrationMutationLock(projectRoot, "parent", func() error {
			close(held)
			<-release
			return nil
		})
	}()

	select {
	case <-held:
	case holderErr := <-holderDone:
		t.Fatalf("integration mutation lock holder failed before acquisition: %v", holderErr)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for integration mutation lock holder")
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegrationMutationLockCrossProcess$")
	cmd.Env = append(os.Environ(),
		integrationMutationLockHelperEnv+"=1",
		integrationMutationLockRootEnv+"="+projectRoot,
	)
	output, childErr := cmd.CombinedOutput()
	close(release)
	if holderErr := <-holderDone; holderErr != nil {
		t.Fatalf("integration mutation lock holder failed: %v", holderErr)
	}
	if childErr != nil {
		t.Fatalf("cross-process integration mutation lock check failed: %v\n%s", childErr, output)
	}
}

func TestIntegrationMutationLinearization(t *testing.T) {
	t.Run("public merge appends validated receipt without rewriting evidence", testIntegrationMutationReceiptPersistence)
	t.Run("validator rejection prevents receipt and task persistence", testIntegrationMutationValidatorRejection)
	t.Run("receipt persistence starts after mutation lock release", testIntegrationMutationReceiptAfterLockRelease)
	t.Run("rollback appends the reverse mutation", testIntegrationMutationRollbackReceipt)
	t.Run("already merged no-op appends no receipt", testIntegrationMutationNoOpReceipt)
	t.Run("mutation ordered before verification invalidates old clean evidence", testIntegrationMutationBeforeVerification)
	t.Run("mutation ordered after verification invalidates persisted clean evidence", testIntegrationMutationAfterVerification)
}

func runIntegrationMutationLockHelper(t *testing.T) {
	projectRoot := os.Getenv(integrationMutationLockRootEnv)
	if projectRoot == "" {
		t.Fatalf("%s is not set", integrationMutationLockRootEnv)
	}

	callbackCalled := false
	err := withIntegrationMutationLockTimeout(projectRoot, "child", 150*time.Millisecond, func() error {
		callbackCalled = true
		return nil
	})
	if callbackCalled {
		t.Fatal("integration mutation callback ran without acquiring the cross-process lock")
	}
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("integration mutation contention error = %T %v, want timeout classification", err, err)
	}
	for _, want := range []string{
		`integration mutation lock operation "child"`,
		"another merge is updating the integration ref or main index",
		"retry the merge",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("integration mutation contention error %q missing %q", err, want)
		}
	}
}
