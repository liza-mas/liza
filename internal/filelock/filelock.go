package filelock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const (
	// DefaultLockTimeout is the default maximum time to wait for a file lock.
	DefaultLockTimeout = 10 * time.Second
	// LockCheckInterval is how often to retry lock acquisition.
	LockCheckInterval = 100 * time.Millisecond
)

// FileLock provides file-based mutual exclusion with diagnostic owner metadata.
// For shared locks, metadata records only the most recent acquirer, not the full
// set of current holders.
//
// It wraps flock(2) with a polling acquisition loop, best-effort owner metadata,
// classified error types, and optional metrics collection.
type FileLock struct {
	lockPath     string
	ownerPath    string
	lockTimeout  time.Duration
	retryContext context.Context

	// Metrics collection (optional)
	metricsRecorder *MetricsRecorder
	enableMetrics   bool
}

// New creates a FileLock that protects the given file path.
// Lock file: protectedPath + ".lock", owner metadata: protectedPath + ".lock.owner.json".
func New(protectedPath string) *FileLock {
	return &FileLock{
		lockPath:    protectedPath + ".lock",
		ownerPath:   protectedPath + ".lock.owner.json",
		lockTimeout: DefaultLockTimeout,
	}
}

// WithTimeout returns a new FileLock with the given timeout.
// Metrics state is not shared with the original.
func (fl *FileLock) WithTimeout(timeout time.Duration) *FileLock {
	return &FileLock{
		lockPath:     fl.lockPath,
		ownerPath:    fl.ownerPath,
		lockTimeout:  timeout,
		retryContext: fl.retryContext,
	}
}

// WithRetryContext returns an independent lock that retries acquisition timeouts
// with capped backoff until ctx ends. The protected callback runs at most once;
// errors from it, including nested lock timeouts, are never retried.
// Ordinary locks keep their bounded timeout. Metrics are not shared.
func (fl *FileLock) WithRetryContext(ctx context.Context) *FileLock {
	return &FileLock{
		lockPath: fl.lockPath, ownerPath: fl.ownerPath,
		lockTimeout: fl.lockTimeout, retryContext: ctx,
	}
}

// EnableMetrics enables lock metrics collection.
func (fl *FileLock) EnableMetrics() {
	if fl.metricsRecorder == nil {
		fl.metricsRecorder = NewMetricsRecorder()
	}
	fl.enableMetrics = true
}

// DisableMetrics disables lock metrics collection.
func (fl *FileLock) DisableMetrics() {
	fl.enableMetrics = false
}

// GetMetricsRecorder returns the metrics recorder, or nil if not enabled.
func (fl *FileLock) GetMetricsRecorder() *MetricsRecorder {
	return fl.metricsRecorder
}

type ownerMetadata struct {
	Version    int    `json:"version"`
	PID        int    `json:"pid"`
	Hostname   string `json:"hostname,omitempty"`
	Operation  string `json:"operation"`
	Caller     string `json:"caller,omitempty"`
	AcquiredAt string `json:"acquired_at"`
}

func (fl *FileLock) acquireLock(operation string, shared bool) (*flock.Flock, error) {
	lock := flock.New(fl.lockPath)
	var acquired bool
	var err error
	if shared {
		acquired, err = lock.TryRLock()
	} else {
		acquired, err = lock.TryLock()
	}
	if err != nil {
		return nil, ClassifyLockError(err)
	}
	if !acquired {
		return nil, fmt.Errorf("lock not acquired")
	}

	fl.writeOwnerMetadata(operation)

	return lock, nil
}

func (fl *FileLock) writeOwnerMetadata(operation string) {
	hostname, _ := os.Hostname()
	metadata := ownerMetadata{
		Version:    1,
		PID:        os.Getpid(),
		Hostname:   hostname,
		Operation:  operation,
		Caller:     lockCaller(),
		AcquiredAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return
	}
	data = append(data, '\n')

	dir := filepath.Dir(fl.ownerPath)
	base := filepath.Base(fl.ownerPath)
	tmp, err := os.CreateTemp(dir, base+".tmp-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	_ = os.Rename(tmpPath, fl.ownerPath)
}

// lockCaller records a bounded function chain through db/authority wrappers.
// Only compiled function names are exposed: no paths, arguments or state data.
// Operation remains the stable metrics category; this field is diagnostic only.
func lockCaller() string {
	var pcs [32]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	var names []string
	for len(names) < 8 {
		frame, more := frames.Next()
		if frame.Function != "" && !strings.Contains(frame.Function, "/internal/filelock.(*FileLock).") &&
			!strings.HasPrefix(frame.Function, "runtime.") && !strings.HasPrefix(frame.Function, "testing.") {
			names = append(names, frame.Function)
		}
		if !more {
			break
		}
	}
	return strings.Join(names, " <- ")
}

// WithLock executes fn while holding an exclusive file lock.
// Equivalent to WithLockOperation with operation "unknown".
func (fl *FileLock) WithLock(fn func() error) error {
	return fl.WithLockOperation("unknown", fn)
}

// WithLockOperation executes fn while holding an exclusive file lock.
// The operation name is recorded in metrics if enabled.
func (fl *FileLock) WithLockOperation(operation string, fn func() error) error {
	return fl.withLockOperation(operation, false, fn)
}

// WithLockOperationContext executes fn while holding an exclusive file lock,
// aborting lock acquisition when ctx is canceled.
func (fl *FileLock) WithLockOperationContext(ctx context.Context, operation string, fn func() error) error {
	return fl.withLockOperationContext(ctx, operation, false, fn)
}

// WithSharedLockOperation executes fn while holding a shared file lock.
// Multiple shared holders may run concurrently, while an exclusive holder
// acquired through WithLockOperation waits for all shared holders to finish.
func (fl *FileLock) WithSharedLockOperation(operation string, fn func() error) error {
	return fl.withLockOperation(operation, true, fn)
}

// WithSharedLockOperationContext executes fn while holding a shared file lock,
// aborting lock acquisition when ctx is canceled.
func (fl *FileLock) WithSharedLockOperationContext(ctx context.Context, operation string, fn func() error) error {
	return fl.withLockOperationContext(ctx, operation, true, fn)
}

func (fl *FileLock) withLockOperation(operation string, shared bool, fn func() error) error {
	return fl.withLockOperationContext(context.Background(), operation, shared, fn)
}

func (fl *FileLock) withLockOperationContext(ctx context.Context, operation string, shared bool, fn func() error) error {
	if fl.retryContext == nil {
		return fl.withLockAttempt(ctx, operation, shared, fn)
	}
	retryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(fl.retryContext, cancel)
	defer stop()
	if fl.retryContext.Err() != nil {
		cancel()
	}
	backoff := LockCheckInterval
	for {
		entered := false
		err := fl.withLockAttempt(retryCtx, operation, shared, func() error {
			entered = true
			return fn()
		})
		if entered || !IsLockErrorType(err, LockErrorTimeout) {
			return err
		}
		timer := time.NewTimer(backoff)
		select {
		case <-retryCtx.Done():
			timer.Stop()
			return retryCtx.Err()
		case <-timer.C:
		}
		backoff = min(2*backoff, 5*time.Second)
	}
}

func (fl *FileLock) withLockAttempt(ctx context.Context, operation string, shared bool, fn func() error) error {
	var lock *flock.Flock
	var err error

	acquireStart := time.Now()
	deadline := acquireStart.Add(fl.lockTimeout)
	locked := false

	for time.Now().Before(deadline) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		lock, err = fl.acquireLock(operation, shared)
		if err == nil {
			locked = true
			break
		}
		// If it's a non-retryable error (permission, disk full, etc.), fail immediately
		var lockErr *LockError
		if errors.As(err, &lockErr) {
			switch lockErr.Type {
			case LockErrorPermission, LockErrorDiskFull, LockErrorFilesystem:
				return lockErr
			}
		}
		timer := time.NewTimer(LockCheckInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}

	if !locked {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return NewLockTimeout(fmt.Errorf("lock unavailable after %v", fl.lockTimeout))
	}

	acquisitionTime := time.Since(acquireStart)
	holdStart := time.Now()

	// We intentionally do NOT remove the lock file or owner metadata here.
	// Removing the lock file after unlock creates a race: another process can
	// create a new file (different inode) and acquire flock on it, then this
	// process deletes that file, allowing a third process to create yet another
	// file — resulting in two processes holding flock on different inodes
	// simultaneously. Leaving the file in place ensures all processes flock
	// the same inode.
	defer func() {
		lock.Unlock()

		if fl.enableMetrics && fl.metricsRecorder != nil {
			holdTime := time.Since(holdStart)
			fl.metricsRecorder.Record(&Metrics{
				Operation:       operation,
				AcquisitionTime: acquisitionTime,
				HoldTime:        holdTime,
			})
		}
	}()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	return fn()
}
