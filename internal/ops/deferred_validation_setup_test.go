package ops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// dvSetupSecret is a synthetic credential a setup prints; the runner masks
// it by its variable's name.
const dvSetupSecret = "SYNTHETIC-D223-SETUP-SECRET-0123456789"

// runLogged runs one invocation and returns its diagnostics log.
func (f *dvFixture) runLogged() (*RunDeferredValidationResult, string) {
	f.t.Helper()
	var log bytes.Buffer
	result, err := runDeferredValidation(context.Background(), f.root, RunDeferredValidationOptions{}, deferredValidationHooks{
		reap: reapProcessGroup, platformSupported: deferredValidationPlatformSupported(), log: &log,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return result, log.String()
}

func (f *dvFixture) setup(command string) {
	f.t.Helper()
	f.modify(func(state *models.State) { state.Config.PostWorktreeCmd = &command })
}

// Lanes prepare one at a time, so setup commands sharing tool state never
// overlap (D-223). A retry must not hide an overlap: the setup records one.
func TestDeferredValidationSerializesLaneSetup(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	shared := t.TempDir()
	busy, overlap := filepath.Join(shared, "busy"), filepath.Join(shared, "overlap")
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60), dvCommand(dvCheckValue+" # b", 60), dvCommand(dvCheckValue+" # c", 60))
	f.setup(fmt.Sprintf(`mkdir %q 2>/dev/null || { touch %q; exit 1; }; sleep 0.3; rmdir %q`, busy, overlap, busy))

	batch := onlyBatch(t, f.run(false))

	if _, err := os.Stat(overlap); err == nil {
		t.Fatal("two lane setups ran at the same time")
	}
	if batch.Status != models.DeferredValidationBatchPassed {
		t.Fatalf("batch = %#v, want passed", batch)
	}
}

// A setup that fails once is retried from a tree restored to the revision:
// a tracked file the first attempt edited is back to its committed content.
func TestDeferredValidationSetupRetryRestoresTrackedContent(t *testing.T) {
	t.Parallel()
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	tried := filepath.Join(t.TempDir(), "tried")
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.setup(fmt.Sprintf(`if [ ! -f %q ]; then touch %q; echo dirty >> value.txt; exit 1; fi; %s`, tried, tried, dvCheckValue))

	batch := onlyBatch(t, f.run(false))

	if batch.Status != models.DeferredValidationBatchPassed || batch.Reason != "" {
		t.Fatalf("batch = %#v, want passed after the retried setup", batch)
	}
}

// A failed first attempt is logged masked; the retried success leaves no
// batch reason.
func TestDeferredValidationLogsARetriedSetupFailure(t *testing.T) {
	t.Setenv("DV_D223_SECRET_TOKEN", dvSetupSecret)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	tried := filepath.Join(t.TempDir(), "tried")
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.setup(fmt.Sprintf(`if [ ! -f %q ]; then touch %q; echo SETUP-D223-MARKER "$DV_D223_SECRET_TOKEN"; exit 3; fi`, tried, tried))

	result, log := f.runLogged()

	if batch := onlyBatch(t, result); batch.Status != models.DeferredValidationBatchPassed || batch.Reason != "" {
		t.Fatalf("batch = %#v, want passed with no reason", batch)
	}
	if !strings.Contains(log, "attempt 1/2") || !strings.Contains(log, "SETUP-D223-MARKER") || strings.Contains(log, "attempt 2/2") {
		t.Fatalf("log = %q, want the first attempt's failure and output only", log)
	}
	if strings.Contains(log, dvSetupSecret) {
		t.Fatal("log holds the unmasked secret")
	}
}

// A setup that keeps failing makes the batch infra_failed with its masked
// output in the reason, and both attempts are in the log.
func TestDeferredValidationSetupFailureKeepsMaskedOutput(t *testing.T) {
	t.Setenv("DV_D223_SECRET_TOKEN", dvSetupSecret)
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	f.setup(`echo SETUP-D223-MARKER "$DV_D223_SECRET_TOKEN"; exit 3`)

	result, log := f.runLogged()

	batch := onlyBatch(t, result)
	if batch.Status != models.DeferredValidationBatchInfraFailed || !strings.HasPrefix(batch.Reason, "setup: ") || !strings.Contains(batch.Reason, "SETUP-D223-MARKER") {
		t.Fatalf("batch = %#v, want infra_failed with the setup's output", batch)
	}
	if !strings.Contains(log, "attempt 1/2") || !strings.Contains(log, "attempt 2/2") || strings.Count(log, "SETUP-D223-MARKER") != 2 {
		t.Fatalf("log = %q, want both attempts with their output", log)
	}
	if strings.Contains(batch.Reason, dvSetupSecret) || strings.Contains(log, dvSetupSecret) {
		t.Fatal("reason or log holds the unmasked secret")
	}
}

// A setup whose capture overflows keeps none of it, as a failed command's
// does: masking cannot recognize a secret whose end was never captured.
func TestDeferredValidationSetupOutputOverflowIsOmitted(t *testing.T) {
	prefix := "SYNTHETIC-D223-SECRET-PREFIX-" + strings.Repeat("p", 64)
	t.Setenv("DV_D223_SECRET_TOKEN", prefix+"\n"+strings.Repeat("s", 4096))
	f := newDVFixture(t, map[string]string{"value.txt": "ok\n"})
	f.merge("task-1", map[string]string{"a.txt": "a\n"}, dvCommand(dvCheckValue, 60))
	filler := acceptanceOutputLimit - len(prefix) - 64
	f.setup(fmt.Sprintf(`head -c %d /dev/zero | tr '\0' x; printf '\n%%s\n' "$DV_D223_SECRET_TOKEN"; exit 1`, filler))

	result, log := f.runLogged()

	batch := onlyBatch(t, result)
	if batch.Status != models.DeferredValidationBatchInfraFailed || !strings.Contains(batch.Reason, "output omitted") {
		t.Fatalf("batch reason = %q, want infra_failed with the output omitted", batch.Reason)
	}
	for name, text := range map[string]string{"reason": batch.Reason, "log": log} {
		if strings.Contains(text, "xxxx") || strings.Contains(text, prefix) {
			t.Fatalf("%s keeps an overflowed capture", name)
		}
	}
}
