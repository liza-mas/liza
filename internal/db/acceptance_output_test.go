package db

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestAcceptanceCommandTextRoundTrips(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"mixed indentation", "  Determining projects to restore...\nTest run for assembly\nPassed!\n"},
		{"uniform indentation", "  first\n  second\n"},
		{"leading blank", "\n  first\nsecond\n"},
		{"internal blank", "  first\n\nsecond\n"},
		{"trailing blanks", "  first\nsecond\n\n\n"},
		{"no trailing newline", "  first\nsecond"},
		{"escaped characters", "\tquoted: \"value\"\\path\r\nUnicode: é\n"},
		{"non UTF-8 bytes", "  diagnostic\xff\xfe\nnext line\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
			result := models.AcceptanceCommandResult{
				Command: tc.text, CommandSHA256: "original-command-digest", ExitCode: 0,
				StartedAt: started, FinishedAt: started.Add(time.Second), Output: tc.text,
			}
			bb := New(filepath.Join(t.TempDir(), "state.yaml"))
			state := &models.State{Tasks: []models.Task{{ID: "task-1", AcceptanceReceipt: &models.AcceptanceReceipt{
				Commands: []models.AcceptanceCommandResult{result},
			}}}}
			if err := bb.Write(state); err != nil {
				t.Fatalf("receipt write failed: %v", err)
			}
			if err := bb.Modify(func(s *models.State) error {
				s.Tasks[0].Description = "subsequent state update"
				return nil
			}); err != nil {
				t.Fatalf("subsequent update failed: %v", err)
			}
			got, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Tasks[0].AcceptanceReceipt.Commands, []models.AcceptanceCommandResult{result}) {
				t.Fatal("receipt changed after write, modify and read")
			}
		})
	}
}
