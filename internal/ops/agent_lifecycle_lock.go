package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
)

var agentLifecycleLockTimeoutNanos atomic.Int64

func init() {
	agentLifecycleLockTimeoutNanos.Store(int64(filelock.DefaultLockTimeout))
}

// WithAgentLifecycleLock serializes registration and provider-start authority
// for one stable agent ID across processes. Callers must acquire this lock
// before reading or modifying blackboard state.
func WithAgentLifecycleLock(ctx context.Context, projectRoot, agentID, operation string, fn func() error) error {
	if strings.TrimSpace(agentID) == "" {
		return fmt.Errorf("agent lifecycle lock requires an agent ID")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	lock, err := agentLifecycleLock(projectRoot, agentID)
	if err != nil {
		return err
	}
	timeout := time.Duration(agentLifecycleLockTimeoutNanos.Load())
	// Only this lock's own acquisition timeout gets its label; one returned by
	// fn came from a lock taken inside.
	entered := false
	err = lock.WithTimeout(timeout).WithLockOperationContext(ctx, operation, func() error { entered = true; return fn() })
	if !entered && filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		return fmt.Errorf("agent lifecycle operation %q for %s could not acquire the lock within %s; another registration or provider start is still running: %w", operation, agentID, timeout, err)
	}
	return err
}

// SetAgentLifecycleLockTimeoutForTest temporarily changes lifecycle lock wait
// time. It exists so failure semantics can be exercised without slow tests.
func SetAgentLifecycleLockTimeoutForTest(timeout time.Duration) func() {
	previous := agentLifecycleLockTimeoutNanos.Swap(int64(timeout))
	return func() {
		agentLifecycleLockTimeoutNanos.Store(previous)
	}
}

func agentLifecycleLock(projectRoot, agentID string) (*filelock.FileLock, error) {
	sum := sha256.Sum256([]byte(agentID))
	key := hex.EncodeToString(sum[:8])
	lock, err := projectFileLock(projectRoot, "agent-lifecycle-"+key)
	if err == nil {
		return lock, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Some focused callers operate outside a Git repository. They cannot race
	// project cleanup, but still need cross-process same-ID serialization.
	root, absErr := filepath.Abs(projectRoot)
	if absErr != nil {
		return nil, fmt.Errorf("resolve project root for agent lifecycle lock: %w", absErr)
	}
	lockName := strings.TrimPrefix(paths.ProjectDirName(), ".") + "-agent-lifecycle-" + key
	return filelock.New(filepath.Join(filepath.Clean(root), lockName)), nil
}
