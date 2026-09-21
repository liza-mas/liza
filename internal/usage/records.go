// Package usage stores durable provider-usage records outside state.yaml and
// outside every state lock. Records are telemetry: an append failure is
// reported to the caller for logging and never changes a run's result.
// Outcomes are not stored here; the report derives them from task history.
package usage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
)

// SchemaVersion is the record layout version; bumped only with a documented migration.
const SchemaVersion = 1

const (
	usageDirName         = "usage"
	recordFilePrefix     = "records-"
	recordFileSuffix     = ".jsonl"
	recordDayLayout      = "2006-01-02"
	defaultFileByteCap   = 8 * 1024 * 1024
	recordLineByteBudget = 64 * 1024
)

// recordFileByteCap is the size above which a day file rolls to the next
// numbered part. A variable so tests can lower it; production uses the constant.
var recordFileByteCap = defaultFileByteCap

var recordFilePattern = regexp.MustCompile(`^records-(\d{4}-\d{2}-\d{2})(?:\.(\d+))?\.jsonl$`)

// Provenance is the write-time quality of a record's token counts.
type Provenance string

const (
	// ProvenanceTerminalAuthoritative: the provider reported a usage object with a non-zero input or output count.
	ProvenanceTerminalAuthoritative Provenance = "terminal_authoritative"
	// ProvenancePartial: the provider reported some fields and left required ones absent.
	ProvenancePartial Provenance = "partial"
	// ProvenanceUnknown: no usage arrived, or the provider reports none at all.
	ProvenanceUnknown Provenance = "unknown"
	// ProvenanceConflicting: two records share a record_id with different counts; assigned by Load.
	ProvenanceConflicting Provenance = "conflicting"
)

// Record is one provider turn. The registration generation is deliberately
// absent (lifecycle-results.md, Result contract); supervisor_run_id carries the
// session/generation dimension instead.
type Record struct {
	SchemaVersion    int        `json:"schema_version"`
	RecordID         string     `json:"record_id"`
	TaskID           string     `json:"task_id"`
	Role             string     `json:"role"`
	AgentID          string     `json:"agent_id"`
	SupervisorRunID  string     `json:"supervisor_run_id"`
	SessionID        string     `json:"session_id"`
	Provider         string     `json:"provider"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          time.Time  `json:"ended_at"`
	FreshInputTokens int        `json:"fresh_input_tokens"`
	CacheReadTokens  int        `json:"cache_read_tokens"`
	CacheWriteTokens int        `json:"cache_write_tokens"`
	OutputTokens     int        `json:"output_tokens"`
	Provenance       Provenance `json:"provenance"`
	ExitCode         int        `json:"exit_code"`
	WarmSession      bool       `json:"warm_session"`
}

func (r Record) sameCounts(other Record) bool {
	return r.FreshInputTokens == other.FreshInputTokens && r.CacheReadTokens == other.CacheReadTokens &&
		r.CacheWriteTokens == other.CacheWriteTokens && r.OutputTokens == other.OutputTokens
}

// LoadStats describes what Load read. Available=false means the store could
// not be observed; its counts are then meaningless, not zero.
type LoadStats struct {
	Available           bool   `json:"available"`
	Warning             string `json:"warning,omitempty"`
	Files               int    `json:"files"`
	Records             int    `json:"records"`
	MalformedLines      int    `json:"malformed_lines"`
	DuplicatesCollapsed int    `json:"duplicates_collapsed"`
	ConflictingRecords  int    `json:"conflicting_records"`
}

// NewRecordID is the dedup identity: sha256 over the identity dimensions and
// the turn start. Token counts and provenance are deliberately excluded so a
// replayed or corrected record collapses onto the same id.
func NewRecordID(agentID, supervisorRunID, sessionID, provider string, startedAt time.Time) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		agentID, supervisorRunID, sessionID, provider, startedAt.UTC().Format(time.RFC3339Nano),
	}, "|")))
	return hex.EncodeToString(digest[:])
}

// Dir returns the usage store directory inside the branded runtime directory.
func Dir(projectRoot string) string {
	return filepath.Join(paths.New(projectRoot).LizaDir(), usageDirName)
}

// Append writes one record as a complete JSON line to the day file keyed by
// its start time, under a leaf lock. Hold no other lock when calling it.
// Missing SchemaVersion and RecordID are filled from the record itself.
func Append(projectRoot string, r Record) error {
	if r.SchemaVersion == 0 {
		r.SchemaVersion = SchemaVersion
	}
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now().UTC()
	}
	if r.RecordID == "" {
		r.RecordID = NewRecordID(r.AgentID, r.SupervisorRunID, r.SessionID, r.Provider, r.StartedAt)
	}
	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("usage record: %w", err)
	}
	line = append(line, '\n')
	if len(line) > recordLineByteBudget {
		return fmt.Errorf("usage record: %d bytes exceeds the %d line budget", len(line), recordLineByteBudget)
	}

	dir := Dir(projectRoot)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("usage store: %w", err)
	}
	day := r.StartedAt.UTC().Format(recordDayLayout)
	// One lock per day covers every numbered part of that day.
	err = filelock.New(filepath.Join(dir, recordFilePrefix+day)).WithLockOperation("usage-append", func() error {
		filename, err := currentDayFile(dir, day, len(line))
		if err != nil {
			return err
		}
		f, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if _, err := f.Write(line); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	})
	if err != nil {
		return fmt.Errorf("usage store: %w", err)
	}
	return nil
}

// currentDayFile picks the highest-numbered part of the day and rolls to the
// next one when appending lineLen bytes would push it above the byte cap.
func currentDayFile(dir, day string, lineLen int) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	part := 0
	for _, e := range entries {
		d, n, ok := parseRecordFileName(e.Name())
		if ok && d == day && n > part {
			part = n
		}
	}
	filename := recordFileName(dir, day, part)
	info, err := os.Stat(filename)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return filename, nil
	case err != nil:
		return "", err
	case info.Size() > 0 && info.Size()+int64(lineLen) > int64(recordFileByteCap):
		return recordFileName(dir, day, part+1), nil
	}
	return filename, nil
}

func recordFileName(dir, day string, part int) string {
	if part == 0 {
		return filepath.Join(dir, recordFilePrefix+day+recordFileSuffix)
	}
	return filepath.Join(dir, recordFilePrefix+day+"."+strconv.Itoa(part)+recordFileSuffix)
}

func parseRecordFileName(name string) (day string, part int, ok bool) {
	m := recordFilePattern.FindStringSubmatch(name)
	if m == nil {
		return "", 0, false
	}
	if m[2] != "" {
		part, _ = strconv.Atoi(m[2])
	}
	return m[1], part, true
}

type recordFile struct {
	path string
	day  time.Time
	part int
}

// Load reads every day file overlapping [since, until] (a zero bound is open)
// and returns the records whose start lies in the window. A line this version
// cannot interpret — unparsable, without a record_id, or carrying another
// schema_version — is skipped and counted in MalformedLines. Records sharing a
// record_id collapse to the first one, and a divergent duplicate marks that id
// conflicting. A missing directory is reported as unavailable, never as an
// empty store.
func Load(projectRoot string, since, until time.Time) ([]Record, LoadStats, error) {
	var stats LoadStats
	files, err := listRecordFiles(Dir(projectRoot), since, until)
	if errors.Is(err, os.ErrNotExist) {
		stats.Warning = "usage store unavailable: no records directory"
		return nil, stats, nil
	}
	if err != nil {
		return nil, stats, fmt.Errorf("usage store: %w", err)
	}
	stats.Available = true

	var records []Record
	index := map[string]int{}
	for _, file := range files {
		truncatedTail, err := readRecordFile(file.path, func(line []byte) {
			var r Record
			if json.Unmarshal(line, &r) != nil || r.RecordID == "" || r.SchemaVersion != SchemaVersion {
				stats.MalformedLines++
				return
			}
			if !inWindow(r.StartedAt, since, until) {
				return
			}
			if i, seen := index[r.RecordID]; seen {
				switch {
				case records[i].sameCounts(r):
					stats.DuplicatesCollapsed++
				case records[i].Provenance != ProvenanceConflicting:
					records[i].Provenance = ProvenanceConflicting
					stats.ConflictingRecords++
				}
				return
			}
			index[r.RecordID] = len(records)
			records = append(records, r)
		})
		if err != nil {
			return nil, LoadStats{}, fmt.Errorf("usage store: %w", err)
		}
		if truncatedTail {
			stats.MalformedLines++
		}
		stats.Files++
	}
	slices.SortFunc(records, func(a, b Record) int {
		if c := a.StartedAt.Compare(b.StartedAt); c != 0 {
			return c
		}
		return strings.Compare(a.RecordID, b.RecordID)
	})
	stats.Records = len(records)
	return records, stats, nil
}

func listRecordFiles(dir string, since, until time.Time) ([]recordFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []recordFile
	for _, e := range entries {
		dayName, part, ok := parseRecordFileName(e.Name())
		if !ok || e.IsDir() {
			continue
		}
		day, err := time.ParseInLocation(recordDayLayout, dayName, time.UTC)
		if err != nil {
			continue
		}
		// A day overlaps the window unless it ends before since or starts after until.
		if (!since.IsZero() && day.Add(24*time.Hour).Before(since)) || (!until.IsZero() && day.After(until)) {
			continue
		}
		files = append(files, recordFile{path: filepath.Join(dir, e.Name()), day: day, part: part})
	}
	slices.SortFunc(files, func(a, b recordFile) int {
		if c := a.day.Compare(b.day); c != 0 {
			return c
		}
		return a.part - b.part
	})
	return files, nil
}

// readRecordFile feeds each non-empty line to the callback. An oversized line
// stops the scan and is reported as a truncated tail (one malformed line), not
// as a store failure, so the records before it still load.
func readRecordFile(path string, each func(line []byte)) (truncatedTail bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, recordLineByteBudget), recordLineByteBudget)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			each([]byte(line))
		}
	}
	if err := scanner.Err(); errors.Is(err, bufio.ErrTooLong) {
		return true, nil
	} else if err != nil {
		return false, err
	}
	return false, nil
}

func inWindow(t, since, until time.Time) bool {
	if !since.IsZero() && t.Before(since) {
		return false
	}
	if !until.IsZero() && t.After(until) {
		return false
	}
	return true
}
