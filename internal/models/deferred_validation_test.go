package models

import (
	"strings"
	"testing"
)

func deferredCommand(sha string, timeout int) DeferredValidationCommand {
	return DeferredValidationCommand{Command: "cmd-" + sha, CommandSHA256: strings.Repeat(sha, 64), TimeoutSeconds: timeout, Authority: AcceptanceDeferralContract}
}

func commandSHAs(commands []DeferredValidationCommand) []string {
	shas := make([]string, len(commands))
	for i, command := range commands {
		shas[i] = command.CommandSHA256[:1]
	}
	return shas
}

func TestDeferredValidationEffectiveCommands(t *testing.T) {
	ledger := &DeferredValidationLedger{Deferrals: []DeferredValidationDeferral{
		{TaskID: "t1", Commands: []DeferredValidationCommand{deferredCommand("a", 60), deferredCommand("b", 60)}},
		{TaskID: "t2", Commands: []DeferredValidationCommand{deferredCommand("a", 300)}},
	}}
	got := ledger.EffectiveCommands()
	if strings.Join(commandSHAs(got), ",") != "a,b" || got[0].TimeoutSeconds != 300 {
		t.Fatalf("effective = %+v, want a,b in first-deferral order with the widest timeout", got)
	}

	// A retirement covers the deferrals recorded before it.
	ledger.Retirements = []DeferredValidationRetirement{{CommandSHA256: strings.Repeat("a", 64), CoversDeferrals: 2}}
	if got := commandSHAs(ledger.EffectiveCommands()); strings.Join(got, ",") != "b" {
		t.Fatalf("effective after retirement = %v, want b", got)
	}
	if got := ledger.EffectiveDeferrals(strings.Repeat("a", 64)); len(got) != 0 {
		t.Fatalf("a retired command still has effective deferrals: %+v", got)
	}

	// A later deferral of a retired command puts it back.
	ledger.Deferrals = append(ledger.Deferrals, DeferredValidationDeferral{TaskID: "t3", Commands: []DeferredValidationCommand{deferredCommand("a", 60)}})
	if got := commandSHAs(ledger.EffectiveCommands()); strings.Join(got, ",") != "b,a" {
		t.Fatalf("effective after re-deferral = %v, want b,a", got)
	}
	if got := ledger.EffectiveDeferrals(strings.Repeat("a", 64)); len(got) != 1 || got[0].TaskID != "t3" {
		t.Fatalf("effective deferrals = %+v, want only t3", got)
	}
}

func TestDeferredValidationBatchCovers(t *testing.T) {
	commands := []DeferredValidationCommand{deferredCommand("a", 60), deferredCommand("b", 60)}
	batch := DeferredValidationBatch{Status: DeferredValidationBatchPassed, Results: []DeferredValidationResult{
		{CommandSHA256: strings.Repeat("a", 64), Status: DeferredValidationResultPassed},
		{CommandSHA256: strings.Repeat("b", 64), Status: DeferredValidationResultPassed},
	}}
	if !batch.Covers(commands) {
		t.Fatal("a passed batch running every command does not cover them")
	}
	if batch.Covers(append(commands, deferredCommand("c", 60))) {
		t.Fatal("a batch covers a command it never ran")
	}
	batch.Status = DeferredValidationBatchFailed
	if batch.Covers(commands) {
		t.Fatal("a failed batch covers commands")
	}
	batch.Status = DeferredValidationBatchPassed
	batch.Results[1].Status = DeferredValidationResultCarriedForward
	if !batch.Covers(commands) {
		t.Fatal("a carried-forward result does not cover its command")
	}
	batch.Results[1].Status = DeferredValidationResultNotRun
	if batch.Covers(commands) {
		t.Fatal("a not-run result covers its command")
	}
}

func TestDeferredValidationLatestExecutionSkipsCarriedAndNotRun(t *testing.T) {
	sha := strings.Repeat("a", 64)
	result := func(status DeferredValidationResultStatus) []DeferredValidationResult {
		return []DeferredValidationResult{{CommandSHA256: sha, Status: status}}
	}
	ledger := &DeferredValidationLedger{Batches: []DeferredValidationBatch{
		{ID: 1, Results: result(DeferredValidationResultPassed)},
		{ID: 2, Results: result(DeferredValidationResultFailed)},
		{ID: 3, Results: result(DeferredValidationResultCarriedForward)},
		{ID: 4, Results: result(DeferredValidationResultNotRun)},
	}}
	batch, execution := ledger.LatestExecutionFor(sha)
	if batch == nil || batch.ID != 2 || execution.Status != DeferredValidationResultFailed {
		t.Fatalf("latest execution = %+v %+v, want batch 2's failure", batch, execution)
	}
	if batch, _ := ledger.LatestExecutionFor(strings.Repeat("b", 64)); batch != nil {
		t.Fatalf("a command never executed has latest execution %+v", batch)
	}
}

// A newer failure outlives retention, so it can never be pruned in favour of
// an older pass that carry-forward would then reuse.
func TestDeferredValidationPruneBatchesKeepsLatestExecutionOfEachCommand(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ledger := &DeferredValidationLedger{Batches: []DeferredValidationBatch{
		{ID: 1, Results: []DeferredValidationResult{{CommandSHA256: sha, Status: DeferredValidationResultPassed}}},
		{ID: 2, Results: []DeferredValidationResult{{CommandSHA256: sha, Status: DeferredValidationResultFailed}}},
	}}
	for i := range DeferredValidationBatchRetention {
		ledger.Batches = append(ledger.Batches, DeferredValidationBatch{ID: 3 + i, Results: []DeferredValidationResult{{CommandSHA256: sha, Status: DeferredValidationResultNotRun}}})
	}
	ledger.PruneBatches()
	if batch, execution := ledger.LatestExecutionFor(sha); batch == nil || batch.ID != 2 || execution.Status != DeferredValidationResultFailed {
		t.Fatalf("latest execution after pruning = %+v, want batch 2's failure", batch)
	}
	if latest := ledger.LatestPassFor(sha); latest == nil || latest.ID != 1 {
		t.Fatalf("latest pass after pruning = %+v, want batch 1", latest)
	}
}

func TestDeferredValidationLedgerViolationsRequireCarriedFrom(t *testing.T) {
	commit := strings.Repeat("c", 40)
	for _, tc := range []struct {
		name    string
		result  DeferredValidationResult
		invalid bool
	}{
		{"carried with commit", DeferredValidationResult{Status: DeferredValidationResultCarriedForward, CarriedFrom: commit}, false},
		{"carried without commit", DeferredValidationResult{Status: DeferredValidationResultCarriedForward}, true},
		{"carried with short commit", DeferredValidationResult{Status: DeferredValidationResultCarriedForward, CarriedFrom: "abc"}, true},
		{"passed with commit", DeferredValidationResult{Status: DeferredValidationResultPassed, CarriedFrom: commit}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.result.CommandSHA256 = strings.Repeat("a", 64)
			ledger := &DeferredValidationLedger{Batches: []DeferredValidationBatch{{ID: 1, Commit: commit, Status: DeferredValidationBatchPassed, Results: []DeferredValidationResult{tc.result}}}}
			if got := len(DeferredValidationLedgerViolations(ledger)) > 0; got != tc.invalid {
				t.Fatalf("violations = %v, want invalid=%v", DeferredValidationLedgerViolations(ledger), tc.invalid)
			}
		})
	}
}

func TestDeferredValidationPruneBatchesKeepsLatestPassOfEachCommand(t *testing.T) {
	ledger := &DeferredValidationLedger{}
	pass := func(sha string) []DeferredValidationResult {
		return []DeferredValidationResult{{CommandSHA256: strings.Repeat(sha, 64), Status: DeferredValidationResultPassed}}
	}
	ledger.Batches = append(ledger.Batches, DeferredValidationBatch{ID: 1, Status: DeferredValidationBatchPassed, Results: pass("a")})
	ledger.Batches = append(ledger.Batches, DeferredValidationBatch{ID: 2, Status: DeferredValidationBatchPassed, Results: pass("b")})
	for id := 3; id < 3+DeferredValidationBatchRetention; id++ {
		ledger.Batches = append(ledger.Batches, DeferredValidationBatch{ID: id, Status: DeferredValidationBatchFailed, Results: pass("b")})
	}
	ledger.PruneBatches()
	if len(ledger.Batches) != DeferredValidationBatchRetention+1 || ledger.Batches[0].ID != 1 || ledger.Batches[1].ID != 3 {
		t.Fatalf("pruned to %d batches starting %d,%d; want the last pass of a plus the newest %d", len(ledger.Batches), ledger.Batches[0].ID, ledger.Batches[1].ID, DeferredValidationBatchRetention)
	}
	if latest := ledger.LatestPassFor(strings.Repeat("a", 64)); latest == nil || latest.ID != 1 {
		t.Fatalf("latest pass of a = %+v, want batch 1", latest)
	}
	if highest := ledger.HighestBatchID(); highest != 2+DeferredValidationBatchRetention {
		t.Fatalf("highest batch id = %d, want %d", highest, 2+DeferredValidationBatchRetention)
	}
}

// Identities are never reused, even after retention pruned every batch.
func TestDeferredValidationAppendBatchKeepsHighWaterIdentity(t *testing.T) {
	ledger := &DeferredValidationLedger{}
	for i := 0; i < DeferredValidationBatchRetention+3; i++ {
		ledger.AppendBatch(DeferredValidationBatch{Status: DeferredValidationBatchAborted})
	}
	if len(ledger.Batches) != DeferredValidationBatchRetention || ledger.LastBatchID != DeferredValidationBatchRetention+3 {
		t.Fatalf("batches = %d, last id = %d", len(ledger.Batches), ledger.LastBatchID)
	}
	ledger.Batches = nil
	if next := ledger.AppendBatch(DeferredValidationBatch{}); next.ID != DeferredValidationBatchRetention+4 {
		t.Fatalf("id after pruning everything = %d, want %d", next.ID, DeferredValidationBatchRetention+4)
	}
}
