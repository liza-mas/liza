package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
)

const (
	maxPendingVerdictBytes = 16 * 1024
	// pendingVerdictDirName is the private outbox directory under the runtime
	// directory; pendingVerdictDirMode keeps it owner-only.
	pendingVerdictDirName = "pending-verdicts"
	pendingVerdictDirMode = 0o700
	// A drain retries a contended state lock from the initial delay, doubling it
	// up to the maximum.
	pendingVerdictRetryInitialDelay = 100 * time.Millisecond
	pendingVerdictRetryMaxDelay     = 5 * time.Second
	// An envelope is named by the hex SHA-256 of its bytes; the drain reads back
	// only files with the extension and rejects a name that is not its digest.
	pendingVerdictFileExt        = ".json"
	pendingVerdictFileNameFormat = "%x" + pendingVerdictFileExt
	// pendingVerdictRequestIDFormat names a request the outbox mints for a
	// verdict that carried none.
	pendingVerdictRequestIDFormat = "verdict-%x"
	// The two lock labels order publication and cleanup of the outbox directory.
	publishPendingVerdictLockOperation = "publish-pending-verdict"
	removePendingVerdictLockOperation  = "remove-pending-verdict"
)

// This private outbox includes the original generation so a replacement can
// replay only as the original caller (and be quarantined), never impersonate it.
// Neither the envelope nor its credentials may be included in diagnostics.
type pendingVerdict struct {
	TaskID, Verdict, Reason, Impact, ReviewCommit string
	Authority                                     models.AgentAuthority
	Request                                       models.LifecycleIdentity
	GenerationDigest                              string // Public lifecycle JSON intentionally omits this private fence.
}

func pendingVerdictDir(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), pendingVerdictDirName)
}

func submitDurableVerdict(projectRoot, taskID, verdict, reason string, authority models.AgentAuthority, impact, reviewCommit string, opts LifecycleRequestOptions) (*VerdictResult, error) {
	// Legacy calls without a registration generation retain their old behavior.
	if authority.Generation == "" {
		return submitVerdictLifecycle(projectRoot, taskID, verdict, reason, authority.ID, &authority, impact, reviewCommit, opts)
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if err := validateVerdictPayload(taskID, verdict, reason, authority.ID, impact, reviewCommit, &authority); err != nil {
		return nil, err
	}
	// A published snapshot avoids the contested writer lock while capturing the
	// immutable request boundary. A failed read cannot establish a new boundary.
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("read verdict boundary: %w", err)
	}
	verdict, reviewCommit = strings.ToUpper(verdict), strings.ToLower(reviewCommit)
	request, err := NewLifecycleRequest(payloadschema.SubmitVerdictOperation, state.FindTask(taskID), authority.ID, &authority, opts,
		verdictRequestPayload(verdict, reason, impact, reviewCommit))
	if err != nil {
		return nil, err
	}
	if request.RequestID == "" {
		identity, _ := json.Marshal(request)
		request.RequestID = fmt.Sprintf(pendingVerdictRequestIDFormat, sha256.Sum256(identity))
	}
	entry := pendingVerdict{TaskID: taskID, Verdict: verdict, Reason: sanitizeVerdictReason(reason, state, authority),
		Impact: impact, ReviewCommit: reviewCommit, Authority: authority, Request: request}
	filename, err := savePendingVerdict(projectRoot, entry)
	if err != nil {
		return nil, fmt.Errorf("reached verdict not saved: %w", err)
	}
	result, err := entry.submit(projectRoot, opts.RetryContext)
	if entry.settled(result, err) {
		if removeErr := removePendingVerdictFile(filename); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			// Keep success truthful: the receipt makes a later drain idempotent.
			return result, fmt.Errorf("verdict settled but private outbox cleanup failed: %w", removeErr)
		}
	}
	return result, err
}

func (p pendingVerdict) submit(projectRoot string, retryContexts ...context.Context) (*VerdictResult, error) {
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("read pending verdict boundary: %w", err)
	}
	if state.FindTask(p.TaskID) == nil {
		return nil, WrapLifecycleError(payloadschema.SubmitVerdictOperation, nil, &PreconditionError{Reason: "task is no longer in live state"},
			models.LifecycleAlreadyTransitioned, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	opts := LifecycleRequestOptions{RequestID: p.Request.RequestID, ExpectedTransition: p.Request.ExpectedTransition,
		verdictPayloadDigest: p.Request.PayloadDigest}
	if len(retryContexts) > 0 {
		opts.RetryContext = retryContexts[0]
	}
	return submitVerdictLifecycle(projectRoot, p.TaskID, p.Verdict, p.Reason, p.Authority.ID, &p.Authority, p.Impact, p.ReviewCommit, opts)
}

func (p pendingVerdict) settled(result *VerdictResult, err error) bool {
	if err == nil && result != nil {
		return true
	}
	var quarantined *QuarantinedVerdictError
	if errors.As(err, &quarantined) {
		return true
	}
	var lifecycle *LifecycleError
	noEffects := string(LifecycleEffectsNone)
	if errors.As(err, &lifecycle) && lifecycle.Outcome.Effects == noEffects && lifecycle.Outcome.Outcome == models.LifecycleInvalidInput {
		return true // A deterministic refusal requires a corrected request.
	}
	var precondition *PreconditionError
	if errors.As(err, &precondition) && (!errors.As(err, &lifecycle) || lifecycle.Outcome.Effects == noEffects) {
		return true // Invalid input/state cannot improve by replaying this request.
	}
	return errors.As(err, &lifecycle) && (lifecycle.Outcome.Outcome == models.LifecycleAlreadyTransitioned ||
		lifecycle.Outcome.Outcome == models.LifecycleStaleCaller ||
		(lifecycle.Outcome.Outcome == models.LifecycleStateChanged && lifecycle.Outcome.Effects == noEffects &&
			lifecycle.Outcome.TransitionID != "" && lifecycle.Outcome.TransitionID != p.Request.ExpectedTransition))
}

func savePendingVerdict(projectRoot string, entry pendingVerdict) (string, error) {
	entry.GenerationDigest = entry.Request.GenerationDigest
	data, err := json.Marshal(entry)
	if err != nil || len(data) > maxPendingVerdictBytes {
		return "", fmt.Errorf("private verdict envelope exceeds its bounded encoding")
	}
	dir := pendingVerdictDir(projectRoot)
	if err := os.MkdirAll(dir, pendingVerdictDirMode); err != nil {
		return "", err
	}
	filename := filepath.Join(dir, fmt.Sprintf(pendingVerdictFileNameFormat, sha256.Sum256(data)))
	var published string
	err = filelock.New(dir).WithLockOperation(publishPendingVerdictLockOperation, func() error {
		var publishErr error
		published, publishErr = publishPendingVerdictFile(filename, data)
		return publishErr
	})
	return published, err
}

func publishPendingVerdictFile(filename string, data []byte) (string, error) {
	dir := filepath.Dir(filename)
	// Repeat all barriers on retries: a previous publication may have renamed
	// the file but failed before syncing the newly-created parent directory.
	finishPublication := func() (string, error) {
		if err := retrySharingViolation(func() error { return syncArchiveFile(filename) }); err != nil {
			return filename, err
		}
		for _, parent := range []string{dir, filepath.Dir(dir)} {
			if err := syncArchiveDir(parent); err != nil {
				return filename, err
			}
		}
		return filename, nil
	}
	if existing, err := readPendingVerdictFile(filename); err == nil {
		if bytes.Equal(existing, data) {
			return finishPublication()
		}
		return "", fmt.Errorf("private verdict envelope conflict")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".pending-verdict-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data) // CreateTemp creates private 0600 files.
	syncErr, closeErr := f.Sync(), f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	// A concurrent identical submission may hold, replace, or remove the target
	// between our rename and the fallback read; retry until one of them settles.
	err = retryReplaceCollision(func() error {
		renameErr := os.Rename(f.Name(), filename)
		if renameErr != nil {
			if existing, readErr := readPendingVerdictFile(filename); readErr == nil && bytes.Equal(existing, data) {
				return nil
			}
		}
		return renameErr
	})
	if err != nil {
		return "", err
	}
	return finishPublication()
}

// ReplayPendingVerdicts drains this agent's reached verdicts before it releases
// ownership or claims new work. Only acquisition timeouts are retried. A
// cancelled drain leaves the durable envelope available to the next supervisor.
// Old generations always pass through the normal authority/quarantine boundary.
// Unreadable or invalid envelopes are retained for manual recovery, without
// preventing unrelated agents from registering or valid verdicts from draining.
func ReplayPendingVerdicts(ctx context.Context, projectRoot, agentID string) error {
	entries, err := os.ReadDir(pendingVerdictDir(projectRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read private verdict outbox: %w", err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), pendingVerdictFileExt) {
			continue
		}
		filename := filepath.Join(pendingVerdictDir(projectRoot), entry.Name())
		pending, err := readPendingVerdict(filename)
		if errors.Is(err, os.ErrNotExist) {
			continue // The submitting command settled it while we listed.
		}
		if err != nil {
			// Its owner cannot be trusted until validation succeeds. Do not let
			// one corrupt envelope stop the pool, or print private contents.
			log.Printf("WARNING: skipping unreadable or invalid private verdict envelope %q; retained for manual recovery (see troubleshooting)", entry.Name())
			continue
		}
		if pending.Authority.ID != agentID {
			continue
		}
		// A prior save may have failed after rename; replay must first finish
		// the publication barriers, just like a repeated CLI submission.
		if _, err := savePendingVerdict(projectRoot, pending); err != nil {
			return fmt.Errorf("finish pending verdict publication: %w", err)
		}
		delay := pendingVerdictRetryInitialDelay
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			result, submitErr := pending.submit(projectRoot)
			if pending.settled(result, submitErr) {
				if err := removePendingVerdictFile(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("remove settled private verdict: %w", err)
				}
				break
			}
			if !filelock.IsLockErrorType(submitErr, filelock.LockErrorTimeout) {
				return submitErr
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			delay = min(delay*2, pendingVerdictRetryMaxDelay)
		}
	}
	return nil
}

// Identical concurrent submissions share one envelope path; on Windows any
// operation on it can collide with another caller's rename or removal.
func readPendingVerdictFile(filename string) (data []byte, err error) {
	err = retrySharingViolation(func() (readErr error) { data, readErr = os.ReadFile(filename); return readErr })
	return data, err
}

func removePendingVerdictFile(filename string) error {
	// Publication and cleanup share a lock; submission runs outside it.
	return filelock.New(filepath.Dir(filename)).WithLockOperation(removePendingVerdictLockOperation, func() error {
		return retrySharingViolation(func() error { return os.Remove(filename) })
	})
}

func readPendingVerdict(filename string) (pendingVerdict, error) {
	var pending pendingVerdict
	var f *os.File
	if err := retrySharingViolation(func() (err error) { f, err = os.Open(filename); return err }); err != nil {
		return pending, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPendingVerdictBytes+1))
	if err != nil {
		return pending, err
	}
	if len(data) > maxPendingVerdictBytes || json.Unmarshal(data, &pending) != nil {
		return pending, fmt.Errorf("invalid private verdict envelope")
	}
	if filepath.Base(filename) != fmt.Sprintf(pendingVerdictFileNameFormat, sha256.Sum256(data)) {
		return pending, fmt.Errorf("private verdict envelope checksum mismatch")
	}
	pending.Request.GenerationDigest = pending.GenerationDigest
	if err := ValidateLifecycleRequestOptions(LifecycleRequestOptions{RequestID: pending.Request.RequestID, ExpectedTransition: pending.Request.ExpectedTransition}); err != nil {
		return pending, fmt.Errorf("invalid private verdict identity")
	}
	if pending.Authority.Generation == "" || pending.Request.RequestID == "" || pending.Request.Actor != pending.Authority.ID ||
		pending.Request.Operation != payloadschema.SubmitVerdictOperation || !lifecycleDigestValid(pending.Request.PayloadDigest) ||
		pending.Request.GenerationDigest != generationFingerprint(pending.Authority.Generation) {
		return pending, fmt.Errorf("invalid private verdict authority")
	}
	return pending, nil
}
