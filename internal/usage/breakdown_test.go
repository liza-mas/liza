package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func costOf(v float64) *float64 { return &v }

func breakdownTestRecords() []Record {
	multiModel := usageTestRecord(usageTestStart)
	multiModel.FreshInputTokens, multiModel.CacheWriteTokens, multiModel.CacheWrite1hTokens = 52, 1000, 600
	multiModel.CacheReadTokens, multiModel.OutputTokens = 4000, 20
	multiModel.CostUSD = costOf(0.25)
	multiModel.Models = []ModelUsage{
		{Model: "claude-opus-5-5", FreshInputTokens: 2, CacheWriteTokens: 900, CacheWrite1hTokens: 600, CacheReadTokens: 3000, OutputTokens: 13, CostUSD: costOf(0.2)},
		{Model: "claude-haiku-5-5", FreshInputTokens: 50, CacheWriteTokens: 100, CacheReadTokens: 1000, OutputTokens: 7, CostUSD: costOf(0.05)},
	}

	// A Codex turn: no per-model breakdown and no price.
	codex := usageTestRecord(usageTestStart.Add(time.Minute))
	codex.AgentID, codex.Role, codex.Provider, codex.RequestedModel = "reviewer-1", "reviewer", "codex", "gpt-5.5"
	codex.FreshInputTokens, codex.CacheWriteTokens, codex.CacheReadTokens, codex.OutputTokens = 300, 0, 700, 50

	// A record written before models existed: it lands in the unattributed group.
	legacy := usageTestRecord(usageTestStart.Add(2 * time.Minute))
	return []Record{multiModel, codex, legacy}
}

func TestBuildBreakdownByModel(t *testing.T) {
	b := buildBreakdown(breakdownTestRecords(), BreakdownByModel)

	byKey := map[string]BreakdownRow{}
	sum := BreakdownRow{}
	for _, row := range b.Rows {
		byKey[row.Key] = row
		sum.TokensProcessed += row.TokensProcessed
		sum.CostUSD += row.CostUSD
	}
	if len(b.Rows) != 4 {
		t.Fatalf("rows = %+v, want opus, haiku, gpt-5.5 and the unattributed group", b.Rows)
	}
	// Model shares sum to the totals: a subagent model is neither lost nor double counted.
	if sum.TokensProcessed != b.Totals.TokensProcessed {
		t.Fatalf("model rows sum to %d tokens, totals say %d", sum.TokensProcessed, b.Totals.TokensProcessed)
	}
	if sum.CostUSD != b.Totals.CostUSD {
		t.Fatalf("model rows sum to $%v, totals say $%v", sum.CostUSD, b.Totals.CostUSD)
	}
	if b.Totals.Records != 3 || b.Totals.UnpricedRecords != 2 || b.Totals.CostUSD != 0.25 {
		t.Fatalf("totals = %+v, want 3 records, 2 unpriced, $0.25", b.Totals)
	}
	opus := byKey["claude-opus-5-5"]
	if opus.TokensProcessed != 2+900+3000+13 || opus.CacheWrite1hTokens != 600 || opus.CostUSD != 0.2 || opus.UnpricedRecords != 0 {
		t.Fatalf("opus row = %+v", opus)
	}
	if hit := *opus.InputCacheHitPercent; hit < 76.8 || hit > 76.9 {
		t.Fatalf("opus cache hit = %.2f, want cache read over all input (3000/3902)", hit)
	}
	if byKey["gpt-5.5"].UnpricedRecords != 1 || byKey[unattributedKey].Records != 1 {
		t.Fatalf("rows = %+v, want the unpriced codex row and the legacy record unattributed", b.Rows)
	}
	if b.Rows[0].Key != "claude-opus-5-5" {
		t.Fatalf("first row = %q, want the largest consumer first", b.Rows[0].Key)
	}
}

func TestBuildBreakdownByRole(t *testing.T) {
	b := buildBreakdown(breakdownTestRecords(), BreakdownByRole)
	if len(b.Rows) != 2 {
		t.Fatalf("rows = %+v, want coder and reviewer", b.Rows)
	}
	for _, row := range b.Rows {
		if row.Key == "coder" && row.Records != 2 {
			t.Fatalf("coder row = %+v, want both coder records", row)
		}
	}
	empty := buildBreakdown(nil, BreakdownByRole)
	if len(empty.Rows) != 0 || empty.Totals.InputCacheHitPercent != nil {
		t.Fatalf("empty breakdown = %+v, want no rows and no hit rate", empty)
	}
}

func TestValidateBreakdownBy(t *testing.T) {
	for _, by := range BreakdownDimensions {
		if err := ValidateBreakdownBy(by); err != nil {
			t.Fatalf("ValidateBreakdownBy(%q) = %v", by, err)
		}
	}
	if err := ValidateBreakdownBy("sprint"); err == nil || !strings.Contains(err.Error(), "model, role") {
		t.Fatalf("ValidateBreakdownBy(sprint) = %v, want the accepted dimensions listed", err)
	}
}

// TestUsageStoreLoadsRecordsAcrossAdditiveFields pins the schema promise: a
// line written before the additive fields and one carrying them both load
// under schema version 1, and the new fields survive the round trip.
func TestUsageStoreLoadsRecordsAcrossAdditiveFields(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root)
	if err := os.MkdirAll(dir, recordDirMode); err != nil {
		t.Fatal(err)
	}
	legacyLine := `{"schema_version":1,"record_id":"legacy-1","task_id":"task-1","role":"coder","agent_id":"coder-1",` +
		`"supervisor_run_id":"r","session_id":"task-1","provider":"claude","started_at":"2026-09-19T10:00:00Z",` +
		`"ended_at":"2026-09-19T10:01:00Z","fresh_input_tokens":1,"cache_read_tokens":2,"cache_write_tokens":3,` +
		`"output_tokens":4,"provenance":"terminal_authoritative","exit_code":0,"warm_session":false}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "records-2026-09-19.jsonl"), []byte(legacyLine), recordFileMode); err != nil {
		t.Fatal(err)
	}
	current := breakdownTestRecords()[0]
	current.StartedAt = usageTestStart.Add(time.Hour)
	if err := Append(root, current); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records, stats, err := Load(root, time.Time{}, time.Time{})
	if err != nil || stats.MalformedLines != 0 || len(records) != 2 {
		t.Fatalf("Load = (%d records, %+v, %v), want both lines", len(records), stats, err)
	}
	if records[0].Models != nil || records[0].CostUSD != nil {
		t.Fatalf("legacy record = %+v, want no models and no cost", records[0])
	}
	got := records[1]
	if len(got.Models) != 2 || got.CostUSD == nil || *got.CostUSD != 0.25 || got.CacheWrite1hTokens != 600 {
		t.Fatalf("current record = %+v, want its models, cost and 1h split", got)
	}
}
