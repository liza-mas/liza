package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// LivenessRecord is one registration generation's latest heartbeat, kept
// beside state.yaml so a heartbeat never takes the state lock or rewrites the
// state (ADR-0177). Reads overlay it onto the generation's agent row while its
// Seq is above the row's folded liveness_seq; every Modify persists that
// overlay, which folds the record into state.yaml.
type LivenessRecord struct {
	AgentID      string    `json:"agent_id"`
	Generation   string    `json:"generation"`
	Seq          int64     `json:"seq"`
	Heartbeat    time.Time `json:"heartbeat"`
	LeaseExpires time.Time `json:"lease_expires"`
	// The latest review-lease renewal, made by the beat with seq ReviewSeq
	// during the active provider session of ReviewTask (ADR-0174). Later beats
	// carry it, since each replaces the record, and it applies only until a
	// mutation folds a seq at or above ReviewSeq, as a locked beat applied once.
	ReviewTask         string    `json:"review_task,omitempty"`
	ReviewLeaseExpires time.Time `json:"review_lease_expires,omitzero"`
	ReviewSeq          int64     `json:"review_seq,omitempty"`
}

const livenessRecordInfix = ".liveness-"

// livenessRecordHashLen is the hex length of the name suffix; the registration
// sweep removes only names of exactly this shape, never temporary files.
const livenessRecordHashLen = 16

// LivenessRecordPath returns the record path of one registration generation.
// Each generation has its own file, so a delayed beat of a replaced
// generation can never displace the current generation's record.
func LivenessRecordPath(statePath, agentID, generation string) string {
	sum := sha256.Sum256([]byte(agentID + "\x00" + generation))
	return statePath + livenessRecordInfix + hex.EncodeToString(sum[:livenessRecordHashLen/2])
}

// WriteLivenessRecord publishes record by atomic rename without the state
// lock. A generation has one writer, its supervisor heartbeat, and the temp
// file is unique per call, so a retried Windows publish still installs this
// call's bytes. There is no fsync: a lost or torn record costs one beat, and
// readers ignore records they cannot decode.
func WriteLivenessRecord(statePath string, record LivenessRecord) error {
	if record.AgentID == "" || record.Generation == "" {
		return fmt.Errorf("liveness record requires agent ID and generation")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal liveness record: %w", err)
	}
	path := LivenessRecordPath(statePath, record.AgentID, record.Generation)
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create liveness record: %w", err)
	}
	tmpPath := f.Name()
	if err := f.Chmod(0644); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("set liveness record permissions: %w", err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("write liveness record: %w", firstError(writeErr, closeErr))
	}
	if err := publishState(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("publish liveness record: %w", err)
	}
	return nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func readLivenessRecord(path string) (LivenessRecord, bool) {
	var record LivenessRecord
	data, err := readStateFile(path)
	if err != nil {
		return record, false
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, false
	}
	return record, true
}

// applyLiveness overlays each agent's current-generation record whose Seq is
// above the row's folded liveness_seq, with the effects the locked heartbeat
// transaction had: agent heartbeat and lease, the lease of the task the agent
// is assigned, and the review lease renewed during an active provider session.
// Leases a mutation cleared stay cleared. Seq is the linearization point:
// inside Modify the record is read under the state lock, so a record published
// earlier is folded before the callback runs (which then wins), and one
// published later carries a higher Seq and applies on top of the commit.
func applyLiveness(state *models.State, statePath string) {
	for id, agent := range state.Agents {
		if agent.Generation == "" {
			continue
		}
		record, ok := readLivenessRecord(LivenessRecordPath(statePath, id, agent.Generation))
		folded := agent.LivenessSeq
		if !ok || record.AgentID != id || record.Generation != agent.Generation || record.Seq <= folded {
			continue
		}
		agent.Heartbeat = record.Heartbeat
		agent.LeaseExpires = timePtr(record.LeaseExpires)
		agent.LivenessSeq = record.Seq
		state.Agents[id] = agent

		if agent.CurrentTask == nil {
			continue
		}
		task := state.FindTask(*agent.CurrentTask)
		if task == nil {
			continue
		}
		if task.AssignedTo != nil && *task.AssignedTo == id && task.LeaseExpires != nil {
			task.LeaseExpires = timePtr(record.LeaseExpires)
		}
		if task.ReviewingBy != nil && *task.ReviewingBy == id && task.ReviewLeaseExpires != nil &&
			record.ReviewTask == task.ID && record.ReviewSeq > folded {
			task.ReviewLeaseExpires = timePtr(record.ReviewLeaseExpires)
		}
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// SweepLivenessRecords removes published records that match no registration
// generation of state. Registration calls it under the state lock with its
// candidate rows. Only exact record names are removed: in-flight temporary
// files keep their writers' publishes intact. Ceilings: a replaced generation
// that publishes after the sweep leaves one orphan, never read and removed by
// the next sweep; if the registering transaction then fails, only the
// registering agent's previous record can be lost, costing that agent the
// beats since its last fold until its next beat.
func SweepLivenessRecords(statePath string, state *models.State) error {
	keep := make(map[string]bool, len(state.Agents))
	for id, agent := range state.Agents {
		keep[filepath.Base(LivenessRecordPath(statePath, id, agent.Generation))] = true
	}
	prefix := filepath.Base(statePath) + livenessRecordInfix
	entries, err := os.ReadDir(filepath.Dir(statePath))
	if err != nil {
		return fmt.Errorf("list liveness records: %w", err)
	}
	var firstErr error
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] || !isLivenessRecordName(name, prefix) {
			continue
		}
		if err := os.Remove(filepath.Join(filepath.Dir(statePath), name)); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = fmt.Errorf("remove liveness record %s: %w", name, err)
		}
	}
	return firstErr
}

func isLivenessRecordName(name, prefix string) bool {
	suffix, ok := strings.CutPrefix(name, prefix)
	if !ok || len(suffix) != livenessRecordHashLen {
		return false
	}
	_, err := hex.DecodeString(suffix)
	return err == nil
}
