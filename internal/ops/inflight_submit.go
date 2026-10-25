package ops

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/atomicfile"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// submitGraceMargin covers the rebase, index refresh and final transaction
// around canonical execution.
const submitGraceMargin = 10 * time.Minute

// inflightSubmitNonceBytes is the random part of a marker's file name.
const inflightSubmitNonceBytes = 8

// MaxInflightSubmitGrace bounds the time any in-flight submit marker can keep
// a session alive past its execution timeout: the largest acceptance batch,
// the submit slot wait floor and the margin.
const MaxInflightSubmitGrace = referencecontract.AcceptanceMaxTimeoutSeconds*time.Second + SubmitSlotWaitFloor + submitGraceMargin

// inflightSubmitMarker tells the submitting agent's supervisor that this
// agent's own submit is running and until when it may legitimately run. Its
// deadline is fixed when written and never extended (D-38).
type inflightSubmitMarker struct {
	TaskID           string    `json:"task_id"`
	PID              int       `json:"pid"`
	GenerationDigest string    `json:"generation_digest"`
	StartedAt        time.Time `json:"started_at"`
	Deadline         time.Time `json:"deadline"`
}

// inflightSubmitPrefix is the file name prefix of every marker of agentID.
// Each invocation writes its own `<agent-id>.<nonce>.json`, so a returning
// submit can only remove its own marker, never a replacement's.
func inflightSubmitPrefix(agentID string) (string, bool) {
	if agentID == "" || filepath.Base(agentID) != agentID || agentID == "." || agentID == ".." {
		return "", false
	}
	return agentID + ".", true
}

// writeInflightSubmitMarker records an agent-authenticated submit whose
// canonical batch may run for batch. A marker that cannot be written only
// forgoes the deferral; the submit proceeds. The returned func removes it.
func writeInflightSubmitMarker(projectRoot string, authority models.AgentAuthority, taskID string, batch time.Duration, now time.Time) (remove func()) {
	noop := func() {}
	prefix, ok := inflightSubmitPrefix(authority.ID)
	if !ok {
		return noop
	}
	nonce := make([]byte, inflightSubmitNonceBytes)
	_, err := rand.Read(nonce)
	path := filepath.Join(paths.New(projectRoot).InflightSubmitDir(), prefix+hex.EncodeToString(nonce)+".json")
	var data []byte
	if err == nil {
		data, err = json.Marshal(inflightSubmitMarker{
			TaskID:           taskID,
			PID:              os.Getpid(),
			GenerationDigest: lifecycleDigest([]byte(authority.Generation)),
			StartedAt:        now,
			Deadline:         now.Add(batch + submitGraceMargin),
		})
	}
	if err == nil {
		err = atomicfile.Write(path, data)
	}
	if err != nil {
		log.Printf("WARNING: in-flight submit marker for task %s not written; the execution timeout will not wait for this submit: %v", taskID, err)
		return noop
	}
	return func() {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("WARNING: in-flight submit marker for task %s not removed: %v", taskID, err)
		}
	}
}

// InflightSubmitDeadline reports the latest fixed deadline among the submits
// the given registration is running for taskID. A marker counts only if it
// names that task and generation, its process is alive, and its deadline is in
// the future and within MaxInflightSubmitGrace of its start. Unreadable
// markers are skipped, so the caller keeps its ordinary timeouts.
func InflightSubmitDeadline(projectRoot string, authority models.AgentAuthority, taskID string, now time.Time) (time.Time, bool) {
	var latest time.Time
	for _, marker := range liveInflightSubmitMarkers(projectRoot, authority, taskID) {
		if marker.Deadline.After(now) && marker.Deadline.After(latest) {
			latest = marker.Deadline
		}
	}
	return latest, !latest.IsZero()
}

// InflightSubmitRunning reports whether a submit of the given registration for
// taskID is still running, whatever its marker's deadline: a marker past its
// deadline whose process is alive still counts (D-174). Unreadable markers do
// not, so their submit is no longer observable.
func InflightSubmitRunning(projectRoot string, authority models.AgentAuthority, taskID string) bool {
	return len(liveInflightSubmitMarkers(projectRoot, authority, taskID)) > 0
}

// liveInflightSubmitMarkers returns the readable markers that name taskID and
// the registration's generation, stay within MaxInflightSubmitGrace of their
// start, and whose process is alive.
func liveInflightSubmitMarkers(projectRoot string, authority models.AgentAuthority, taskID string) []inflightSubmitMarker {
	prefix, ok := inflightSubmitPrefix(authority.ID)
	if !ok {
		return nil
	}
	dir := paths.New(projectRoot).InflightSubmitDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	digest := lifecycleDigest([]byte(authority.Generation))
	var live []inflightSubmitMarker
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var marker inflightSubmitMarker
		if err := json.Unmarshal(data, &marker); err != nil {
			continue
		}
		if marker.TaskID != taskID || marker.GenerationDigest != digest ||
			marker.Deadline.After(marker.StartedAt.Add(MaxInflightSubmitGrace)) ||
			!IsProcessAlive(marker.PID) {
			continue
		}
		live = append(live, marker)
	}
	return live
}

// WriteInflightSubmitMarkerForTest writes a marker for the calling process
// with the given deadline, as a submit of authority for taskID would. The
// returned func removes it.
func WriteInflightSubmitMarkerForTest(projectRoot string, authority models.AgentAuthority, taskID string, deadline time.Time) (remove func()) {
	now := time.Now().UTC()
	return writeInflightSubmitMarker(projectRoot, authority, taskID, deadline.Sub(now)-submitGraceMargin, now)
}
