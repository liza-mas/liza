package persist

import "testing"

func TestOracleUsageRecordAppend(t *testing.T) {
	t.Parallel()
	var inputs []usageInput
	decodeFixture(t, "usage.json", &inputs)
	project := t.TempDir()
	for _, input := range inputs {
		if err := appendUsage(project, input); err != nil {
			t.Fatalf("append usage: %v", err)
		}
	}
	locks := []string{"records-2026-09-30.lock", "records-2026-10-02.lock"}
	dir := usageDirectory(project)
	success := snapshot(t, dir, locks...)
	rawStateCheck(t, "usage-days.txt", nil, success)
	var oversized usageInput
	decodeFixture(t, "usage-oversize.json", &oversized)
	if len(oversized.Session) <= 64*1024 {
		t.Fatal("oversize fixture must exceed line budget via session")
	}
	err := appendUsage(project, oversized)
	requireRefusal(t, "usage line budget", err)
	after := snapshot(t, dir, locks...)
	requireSame(t, "refused usage day filenames and bytes", success, after)
	rawStateCheck(t, "usage-refusal.txt", success, jsonBytes(t, struct {
		Operation string
		Refused   bool
		Days      string
	}{"usage line budget", err != nil, string(after)}))
}
