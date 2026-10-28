package persist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOracleAlertLog(t *testing.T) {
	t.Parallel()
	var inputs []alertInput
	decodeFixture(t, "alerts.json", &inputs)
	root := t.TempDir()
	path := filepath.Join(root, "alerts.log")
	for _, input := range inputs {
		if err := appendAlert(path, input); err != nil {
			t.Fatalf("append alert: %v", err)
		}
	}
	rawStateCheck(t, "alerts-log.txt", nil, readBytes(t, path))
	rawStateCheck(t, "alerts-once.txt", nil, readBytes(t, path+".once"))
	records := []alertRecord{}
	for _, line := range readLines(fixture(t, "alerts-readback.log")) {
		records = append(records, parseAlert(line))
	}
	rawStateCheck(t, "alerts-readback.txt", fixture(t, "alerts-readback.log"), jsonBytes(t, records))

	for _, keyed := range []bool{false, true} {
		name := "plain"
		if keyed {
			name = "keyed"
		}
		t.Run("refusal-"+name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "alerts.log")
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			before := snapshot(t, root, "alerts.log.once.lock")
			input := inputs[0]
			if keyed {
				input = inputs[1]
			}
			err := appendAlert(path, input)
			requireRefusal(t, "alert directory "+name, err)
			after := snapshot(t, root, "alerts.log.once.lock")
			requireSame(t, "refused alert directory and absent ledger", before, after)
			if _, statErr := os.Stat(path + ".once"); !os.IsNotExist(statErr) {
				t.Fatal("refused alert recorded a ledger")
			}
			rawStateCheck(t, "alerts-refusal-"+name+".txt", before, jsonBytes(t, struct {
				Operation string
				Refused   bool
				Inventory string
			}{"alert directory " + name, err != nil, string(after)}))
		})
	}
}
