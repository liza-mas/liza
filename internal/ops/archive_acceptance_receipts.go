package ops

import (
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ArchiveLimit bounds one archival transaction.
type ArchiveLimit struct {
	MaxTasks int
	// MaxBytes is soft: the first selected object is always archived, even
	// when it alone exceeds the budget, so every call makes progress.
	MaxBytes int
}

// DefaultArchiveLimit bounds the work done under one state lock.
var DefaultArchiveLimit = ArchiveLimit{MaxTasks: 8, MaxBytes: 4 << 20}

// ArchiveResult reports one archival transaction.
type ArchiveResult struct {
	Archived []string
	Bytes    int
	// Remaining counts eligible tasks left after this transaction.
	Remaining int
}

// archiveAfterObjectsTestHook runs inside the state transaction after the
// archive objects are durable and before the state is published. A returned
// error aborts the transaction.
var archiveAfterObjectsTestHook func() error

// postMergeArchiveTestHook runs after a new merge is committed and before the
// post-merge archive maintenance.
var postMergeArchiveTestHook func()

// archivableReceipt reports whether a task's receipt may leave live state.
// Terminal statuses have no outgoing transitions, and every reader of the
// receipt outside inspection serves an active task.
func archivableReceipt(task *models.Task) bool {
	return task.Status.IsTerminal() && task.AcceptanceReceipt != nil
}

// ArchiveTerminalAcceptanceReceipts moves terminal tasks' acceptance receipts
// into immutable archive objects, in one bounded state transaction. It holds
// the project lifecycle lock so it cannot race project reset.
func ArchiveTerminalAcceptanceReceipts(projectRoot string, authority *models.AgentAuthority, limit ArchiveLimit) (ArchiveResult, error) {
	var result ArchiveResult
	err := WithProjectLifecycleSharedLock(projectRoot, "archive-acceptance-receipts", func() error {
		var err error
		result, err = archiveTerminalAcceptanceReceipts(projectRoot, authority, limit)
		return err
	})
	return result, err
}

// archiveTerminalAcceptanceReceipts is the transaction itself, for callers
// already holding the project lifecycle lock. It must never run inside
// another operation's state transaction.
func archiveTerminalAcceptanceReceipts(projectRoot string, authority *models.AgentAuthority, limit ArchiveLimit) (ArchiveResult, error) {
	bb := db.For(paths.New(projectRoot).StatePath())
	// A stale snapshot is safe: eligibility is re-evaluated under the lock.
	// With nothing eligible the state lock is never taken.
	snapshot, err := bb.ReadSnapshot()
	if err != nil {
		return ArchiveResult{}, err
	}
	if !hasArchivableReceipt(snapshot) {
		return ArchiveResult{}, nil
	}

	var result ArchiveResult
	err = modifyLifecycleState(bb, authority, func(state *models.State) error {
		result = ArchiveResult{}
		now := time.Now().UTC()
		// Batches follow state order: selection stops at the first task that
		// does not fit, so later tasks are neither encoded nor skipped ahead.
		full := false
		for i := range state.Tasks {
			task := &state.Tasks[i]
			if !archivableReceipt(task) {
				continue
			}
			if full || len(result.Archived) >= limit.MaxTasks {
				result.Remaining++
				continue
			}
			data, err := encodeArchiveObject(task.ID, task.AcceptanceReceipt)
			if err != nil {
				return err
			}
			if len(result.Archived) > 0 && result.Bytes+len(data) > limit.MaxBytes {
				full = true
				result.Remaining++
				continue
			}
			sha, err := writeArchiveObject(projectRoot, data)
			if err != nil {
				return err
			}
			task.AcceptanceReceipt = nil
			task.Archived = append(task.Archived, models.ArchivedFieldRef{
				Field: models.ArchivedFieldAcceptanceReceipt, SHA256: sha, ArchivedAt: now,
			})
			result.Archived = append(result.Archived, task.ID)
			result.Bytes += len(data)
		}
		if archiveAfterObjectsTestHook != nil {
			return archiveAfterObjectsTestHook()
		}
		return nil
	})
	if err != nil {
		return ArchiveResult{}, err
	}
	return result, nil
}

func hasArchivableReceipt(state *models.State) bool {
	for i := range state.Tasks {
		if archivableReceipt(&state.Tasks[i]) {
			return true
		}
	}
	return false
}
