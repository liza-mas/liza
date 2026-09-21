package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/paths"
)

var usageTestStart = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

func usageTestRecord(startedAt time.Time) Record {
	return Record{
		TaskID:           "task-1",
		Role:             "coder",
		AgentID:          "coder-1",
		SupervisorRunID:  "0123456789abcdef0123456789abcdef",
		SessionID:        "task-1",
		Provider:         "claude",
		StartedAt:        startedAt,
		EndedAt:          startedAt.Add(time.Minute),
		FreshInputTokens: 100,
		CacheReadTokens:  1000,
		CacheWriteTokens: 10,
		OutputTokens:     50,
		Provenance:       ProvenanceTerminalAuthoritative,
		ExitCode:         0,
		WarmSession:      true,
	}
}

func recordID(r Record) string {
	return NewRecordID(r.AgentID, r.SupervisorRunID, r.SessionID, r.Provider, r.StartedAt)
}

func TestUsageRecordIdentity(t *testing.T) {
	t.Parallel()
	base := usageTestRecord(usageTestStart)

	// GIVEN the same identity THEN the id is stable and hex-encoded sha256.
	stable := recordID(base)
	if again := recordID(usageTestRecord(usageTestStart)); again != stable {
		t.Fatalf("record id must be deterministic: %s vs %s", stable, again)
	}
	if got := len(stable); got != 64 {
		t.Fatalf("record id length = %d, want 64 hex characters", got)
	}

	// WHEN any identity dimension changes THEN the id changes.
	variants := map[string]Record{
		"agent":          func() Record { r := base; r.AgentID = "coder-2"; return r }(),
		"supervisor_run": func() Record { r := base; r.SupervisorRunID = "fedcba9876543210fedcba9876543210"; return r }(),
		"session":        func() Record { r := base; r.SessionID = "other-session"; return r }(),
		"provider":       func() Record { r := base; r.Provider = "codex"; return r }(),
		"start":          func() Record { r := base; r.StartedAt = base.StartedAt.Add(time.Nanosecond); return r }(),
	}
	seen := map[string]string{recordID(base): "base"}
	for name, r := range variants {
		id := recordID(r)
		if prev, dup := seen[id]; dup {
			t.Errorf("changing %s produced the same id as %s", name, prev)
		}
		seen[id] = name
	}

	// WHEN only counts or provenance change THEN the id is unchanged.
	counts := base
	counts.FreshInputTokens, counts.CacheReadTokens, counts.OutputTokens = 1, 2, 3
	counts.Provenance = ProvenancePartial
	if recordID(counts) != recordID(base) {
		t.Fatal("record id must not depend on token counts or provenance")
	}

	// THEN JSON emits exactly the documented keys and no generation field.
	base.SchemaVersion = SchemaVersion
	base.RecordID = recordID(base)
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(keys))
	for k := range keys {
		got = append(got, k)
	}
	slices.Sort(got)
	want := []string{
		"agent_id", "cache_read_tokens", "cache_write_tokens", "ended_at", "exit_code",
		"fresh_input_tokens", "output_tokens", "provenance", "provider", "record_id", "role",
		"schema_version", "session_id", "started_at", "supervisor_run_id", "task_id", "warm_session",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json keys = %v, want %v", got, want)
	}
	for k := range keys {
		if strings.Contains(k, "generation") {
			t.Fatalf("registration generation must not be serialized, found key %q", k)
		}
	}
	if keys["schema_version"] != float64(1) || keys["supervisor_run_id"] != base.SupervisorRunID {
		t.Fatalf("unexpected schema_version/supervisor_run_id in %s", data)
	}
}

func TestUsageStoreAppendLoad(t *testing.T) {
	root := t.TempDir()
	if want := filepath.Join(paths.New(root).LizaDir(), "usage"); Dir(root) != want {
		t.Fatalf("Dir = %q, want %q", Dir(root), want)
	}

	// GIVEN one appended record WHEN loaded THEN it round-trips with id and version filled.
	first := usageTestRecord(usageTestStart)
	if err := Append(root, first); err != nil {
		t.Fatal(err)
	}
	records, stats, err := Load(root, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Available || stats.Records != 1 || len(records) != 1 {
		t.Fatalf("stats = %+v, records = %d", stats, len(records))
	}
	want := first
	want.SchemaVersion, want.RecordID = SchemaVersion, recordID(first)
	if !records[0].StartedAt.Equal(want.StartedAt) || !records[0].EndedAt.Equal(want.EndedAt) {
		t.Fatalf("interval mismatch: %+v", records[0])
	}
	records[0].StartedAt, records[0].EndedAt = want.StartedAt, want.EndedAt
	if !reflect.DeepEqual(records[0], want) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", records[0], want)
	}

	// WHEN 16 appends race on the same day file THEN all 16 load intact.
	const workers = 16
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Go(func() {
			r := usageTestRecord(usageTestStart.Add(time.Duration(i+1) * time.Second))
			errs[i] = Append(root, r)
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	records, stats, err = Load(root, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != workers+1 || stats.MalformedLines != 0 || stats.DuplicatesCollapsed != 0 {
		t.Fatalf("concurrent appends: %d records, stats %+v", len(records), stats)
	}

	// GIVEN a record two days later WHEN the window covers only the first day THEN it is excluded.
	later := usageTestRecord(usageTestStart.Add(48 * time.Hour))
	if err := Append(root, later); err != nil {
		t.Fatal(err)
	}
	records, _, err = Load(root, usageTestStart, usageTestStart.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != workers+1 {
		t.Fatalf("window filter kept %d records, want %d", len(records), workers+1)
	}
	for _, r := range records {
		if r.StartedAt.After(usageTestStart.Add(24 * time.Hour)) {
			t.Fatalf("out-of-window record loaded: %v", r.StartedAt)
		}
	}
	records, _, err = Load(root, usageTestStart.Add(36*time.Hour), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RecordID != recordID(later) {
		t.Fatalf("open-ended window loaded %d records", len(records))
	}

	t.Run("roll at byte cap", testUsageStoreAppendRollsAtByteCap)
}

func testUsageStoreAppendRollsAtByteCap(t *testing.T) {
	root := t.TempDir()
	saved := recordFileByteCap
	recordFileByteCap = 700 // roughly one and a half records
	t.Cleanup(func() { recordFileByteCap = saved })

	// GIVEN appends exceeding the cap on one day THEN the day file rolls into numbered parts.
	day := usageTestStart.Add(72 * time.Hour)
	const n = 5
	for i := range n {
		if err := Append(root, usageTestRecord(day.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	dayName := day.Format("2006-01-02")
	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "records-"+dayName) && strings.HasSuffix(e.Name(), ".jsonl") {
			parts = append(parts, e.Name())
			info, err := e.Info()
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() > int64(recordFileByteCap) {
				t.Fatalf("%s is %d bytes, above the %d cap", e.Name(), info.Size(), recordFileByteCap)
			}
		}
	}
	if !slices.Contains(parts, "records-"+dayName+".jsonl") || !slices.Contains(parts, "records-"+dayName+".1.jsonl") {
		t.Fatalf("expected rolled parts, got %v", parts)
	}

	// WHEN loaded THEN every part contributes and no record is lost.
	records, stats, err := Load(root, day, day.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != n || stats.Files != len(parts) {
		t.Fatalf("rolled load: %d records from %d files (parts %v)", len(records), stats.Files, parts)
	}
}

func TestUsageStoreLoadDegraded(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// GIVEN a missing directory THEN the store is unavailable, not zero.
	records, stats, err := Load(root, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Available || records != nil || stats.Warning == "" {
		t.Fatalf("missing directory must be unavailable with a warning: %+v", stats)
	}

	// GIVEN a day file with a truncated line, an exact duplicate and a divergent duplicate.
	good := usageTestRecord(usageTestStart)
	good.SchemaVersion, good.RecordID = SchemaVersion, recordID(good)
	dup := good
	conflictA := usageTestRecord(usageTestStart.Add(time.Hour))
	conflictA.SchemaVersion, conflictA.RecordID = SchemaVersion, recordID(conflictA)
	conflictB := conflictA
	conflictB.FreshInputTokens = 999
	clean := usageTestRecord(usageTestStart.Add(2 * time.Hour))
	clean.SchemaVersion, clean.RecordID = SchemaVersion, recordID(clean)

	var lines []string
	for _, r := range []Record{good, dup, conflictA, conflictB, clean} {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	truncated := lines[4][:len(lines[4])/2]
	content := strings.Join([]string{lines[0], lines[1], lines[2], lines[3], truncated, lines[4], ""}, "\n")
	if err := os.MkdirAll(Dir(root), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(Dir(root), "records-"+usageTestStart.Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// WHEN loaded THEN malformed is skipped and counted, duplicates collapse, conflicts are marked.
	records, stats, err = Load(root, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !stats.Available {
		t.Fatalf("store must be available: %+v", stats)
	}
	if stats.MalformedLines != 1 || stats.DuplicatesCollapsed != 1 || stats.ConflictingRecords != 1 {
		t.Fatalf("stats = %+v, want malformed=1 duplicates=1 conflicting=1", stats)
	}
	if len(records) != 3 || stats.Records != 3 {
		t.Fatalf("loaded %d records (stats %d), want 3", len(records), stats.Records)
	}
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.RecordID] = r
	}
	if got := byID[good.RecordID]; got.Provenance != ProvenanceTerminalAuthoritative || got.FreshInputTokens != good.FreshInputTokens {
		t.Fatalf("collapsed duplicate altered the record: %+v", got)
	}
	if got := byID[conflictA.RecordID]; got.Provenance != ProvenanceConflicting {
		t.Fatalf("divergent duplicate must be marked conflicting, got %q", got.Provenance)
	}
	if got := byID[clean.RecordID]; got.Provenance != ProvenanceTerminalAuthoritative {
		t.Fatalf("clean record after a malformed line must still load: %+v", got)
	}
	var authoritative int
	for _, r := range records {
		if r.Provenance == ProvenanceTerminalAuthoritative {
			authoritative += r.FreshInputTokens
		}
	}
	if authoritative != good.FreshInputTokens+clean.FreshInputTokens {
		t.Fatalf("authoritative total %d includes a conflicting record", authoritative)
	}
}
