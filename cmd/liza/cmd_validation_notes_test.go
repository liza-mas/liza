package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/paths"
)

func TestPlanCheckValidationNotesInvalidInputIsAtomic(t *testing.T) {
	for _, tc := range []struct {
		name, payload, action string
	}{
		{"unknown field", `[{"output_index":0,"message":"probe","scope":"changed"}]`, "--pass"},
		{"missing index", `[{"message":"probe"}]`, "--pass"},
		{"null index", `[{"output_index":null,"message":"probe"}]`, "--pass"},
		{"blank", `[{"output_index":0,"message":" "}]`, "--pass"},
		{"invalid utf8", "[{\"output_index\":0,\"message\":\"\xff\"}]", "--pass"},
		{"absent slot", `[{"output_index":1,"message":"probe"}]`, "--pass"},
		{"negative slot", `[{"output_index":-1,"message":"probe"}]`, "--pass"},
		{"duplicate", `[{"output_index":0,"message":"one"},{"output_index":0,"message":"two"}]`, "--pass"},
		{"trailing", `[{"output_index":0,"message":"probe"}] []`, "--pass"},
		{"empty", `[]`, "--pass"},
		{"null array", `null`, "--pass"},
		{"message limit", `[{"output_index":0,"message":"` + strings.Repeat("x", 4097) + `"}]`, "--pass"},
		{"file limit", strings.Repeat(" ", 16385), "--pass"},
		{"clear action", `[{"output_index":0,"message":"probe"}]`, "--clear"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupPlanCheckCLI(t)
			statePath := paths.New(root).StatePath()
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "notes.json")
			if err := os.WriteFile(path, []byte(tc.payload), 0600); err != nil {
				t.Fatal(err)
			}
			stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", tc.action, "--notes-file", path, "--agent-id", "orchestrator-1", "--json")
			if err == nil || parseEnvelope(t, stdout)["ok"] != false {
				t.Fatalf("invalid input accepted: %s, %v", stdout, err)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("invalid notes changed persisted state")
			}
		})
	}
}

func TestPlanCheckValidationNotesReplayPreservesPayload(t *testing.T) {
	root := setupPlanCheckCLI(t)
	path := filepath.Join(t.TempDir(), "notes.json")
	if err := os.WriteFile(path, []byte(`[{"output_index":0,"message":"Check intended hook evidence."}]`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"plan-check", "plan-1", "--pass", "--notes-file", path, "--agent-id", "orchestrator-1", "--json"}
	stdout, err := executeRootCommandCapture(t, root, args...)
	if err != nil || parseEnvelope(t, stdout)["result"].(map[string]any)["changed"] != true {
		t.Fatalf("pass: %s %v", stdout, err)
	}
	statePath := paths.New(root).StatePath()
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err = executeRootCommandCapture(t, root, args...)
	if err != nil || parseEnvelope(t, stdout)["result"].(map[string]any)["changed"] != false {
		t.Fatalf("replay: %s %v", stdout, err)
	}
	if err := os.WriteFile(path, []byte(`[{"output_index":0,"message":"Different guidance."}]`), 0600); err != nil {
		t.Fatal(err)
	}
	stdout, err = executeRootCommandCapture(t, root, args...)
	if err == nil || !strings.Contains(stdout, "different validation notes") {
		t.Fatalf("changed replay: %s %v", stdout, err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("replay changed state")
	}
}
