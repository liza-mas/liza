package ops

import (
	"errors"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

var errNoTerminalArchiveWork = errors.New("no terminal archive work")

// ArchiveTerminalTasks performs one bounded maintenance transaction, only when
// the operator has enabled terminal_task_archival. It never enables it itself.
func ArchiveTerminalTasks(projectRoot string, authority *models.AgentAuthority, limit ArchiveLimit) (ArchiveResult, error) {
	var result ArchiveResult
	err := WithProjectLifecycleSharedLock(projectRoot, "archive-terminal-tasks", func() error {
		var err error
		result, err = archiveTerminalTasks(projectRoot, authority, limit)
		return err
	})
	return result, err
}

// archiveTerminalTasks is for merge callers already holding the lifecycle lock,
// outside their own state transaction.
func archiveTerminalTasks(projectRoot string, authority *models.AgentAuthority, limit ArchiveLimit, requests ...LifecycleRequestOptions) (ArchiveResult, error) {
	return archiveTerminalTasksBatch(projectRoot, authority, limit, false, requests...)
}

// EnableAndArchiveTerminalTasks is operator maintenance: explicit enablement
// and the first bounded batch commit together, even when there is no backlog.
func EnableAndArchiveTerminalTasks(projectRoot string, limit ArchiveLimit) (ArchiveResult, error) {
	var result ArchiveResult
	err := WithProjectLifecycleSharedLock(projectRoot, "archive-terminal-tasks", func() error {
		var err error
		result, err = archiveTerminalTasksBatch(projectRoot, nil, limit, true)
		return err
	})
	return result, err
}

func archiveTerminalTasksBatch(projectRoot string, authority *models.AgentAuthority, limit ArchiveLimit, enable bool, requests ...LifecycleRequestOptions) (ArchiveResult, error) {
	if limit.MaxTasks < 1 || limit.MaxBytes < 1 {
		return ArchiveResult{}, fmt.Errorf("archive task and byte limits must be positive")
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	if len(requests) > 0 {
		bb = RequestBlackboard(paths.New(projectRoot).StatePath(), authority, requests[0])
	}
	snapshot, err := bb.ReadSnapshot()
	if err != nil {
		return ArchiveResult{}, err
	}
	if !enable && !snapshot.Config.TerminalTaskArchival {
		return ArchiveResult{}, nil
	}
	if snapshot.Config.TerminalTaskArchival && !hasArchivableTerminalTask(snapshot) {
		return ArchiveResult{}, nil
	}
	var result ArchiveResult
	err = modifyLifecycleState(bb, authority, func(state *models.State) error {
		result = ArchiveResult{}
		changed := false
		if enable && !state.Config.TerminalTaskArchival {
			state.Config.TerminalTaskArchival, changed = true, true
		}
		if !state.Config.TerminalTaskArchival {
			return errNoTerminalArchiveWork
		}
		now := time.Now().UTC()
		full := false
		for i := range state.Tasks {
			task := &state.Tasks[i]
			if !task.Status.IsTerminal() || task.TerminalArchive != nil {
				continue
			}
			if full || len(result.Archived) >= limit.MaxTasks {
				result.Remaining++
				continue
			}
			data, err := db.EncodeTerminalTask(task)
			if err != nil {
				return err
			}
			if len(result.Archived) > 0 && result.Bytes+len(data) > limit.MaxBytes {
				full = true
				result.Remaining++
				continue
			}
			size, err := bb.ArchiveTerminalTask(task, now)
			if err != nil {
				return err
			}
			result.Archived = append(result.Archived, task.ID)
			result.Bytes += size
			changed = true
		}
		if !changed {
			return errNoTerminalArchiveWork
		}
		return nil
	})
	if errors.Is(err, errNoTerminalArchiveWork) {
		return ArchiveResult{}, nil
	}
	if err != nil {
		return ArchiveResult{}, err
	}
	return result, nil
}

func hasArchivableTerminalTask(state *models.State) bool {
	for i := range state.Tasks {
		if state.Tasks[i].Status.IsTerminal() && state.Tasks[i].TerminalArchive == nil {
			return true
		}
	}
	return false
}

// RestoreTerminalTasksInline atomically disables archival and publishes full
// logical terminal tasks inline for an older-binary rollback. Stop every run
// process before calling; immutable objects remain available to old snapshots.
func RestoreTerminalTasksInline(projectRoot string) ([]string, error) {
	var restored []string
	err := WithProjectLifecycleSharedLock(projectRoot, "restore-terminal-tasks", func() error {
		bb := db.For(paths.New(projectRoot).StatePath())
		return bb.Modify(func(state *models.State) error {
			for i := range state.Tasks {
				if state.Tasks[i].TerminalArchive != nil {
					restored = append(restored, state.Tasks[i].ID)
				}
			}
			if !state.Config.TerminalTaskArchival && len(restored) == 0 {
				return errNoTerminalArchiveWork
			}
			state.Config.TerminalTaskArchival = false
			return nil
		})
	})
	if errors.Is(err, errNoTerminalArchiveWork) {
		return nil, nil
	}
	return restored, err
}
