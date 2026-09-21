package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const rejectionRCACLITask = "task-gated-cli"

// setupRejectionRCACLI writes a project whose only task is held by the
// rejection-RCA gate, with a registered orchestrator and coder, plus one valid
// request file per command. It returns the project root, state path and a map
// from command name to its valid file argument.
func setupRejectionRCACLI(t *testing.T) (string, string, map[string][]string) {
	t.Helper()
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		now := time.Now().UTC().Truncate(time.Second)
		task := testhelpers.BuildTaskByStatus(rejectionRCACLITask, models.TaskStatusBlocked, now)
		task.AssignedTo = nil
		task.SpecRef = state.Goal.SpecRef
		reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
		task.BlockedReason = &reason
		task.ReviewCyclesCurrent = 4
		task.ReviewCyclesTotal = 4
		task.RejectionRCA = &models.RejectionRCARecord{
			SchemaVersion:  models.RejectionRCASchemaVersion,
			Threshold:      4,
			RejectionCount: 4,
			GatedAt:        now.Add(-time.Minute),
			GatingCommit:   "0123456789abcdef0123456789abcdef01234567",
		}
		state.Tasks = []models.Task{task}
		state.Agents["coder-1"] = mutationTestAgent("coder")
	})
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	files := map[string][]string{
		"record-rejection-rca": {"--rca-file", writeRejectionRCAFile(t, "rca.json", map[string]any{
			"schema_version": models.RejectionRCASchemaVersion,
			"summary":        "One product defect and one lifecycle retry.",
			"contributions": []any{
				map[string]any{"rejection_index": 1, "categories": []string{models.RejectionCauseProductDefect}, "evidence": []string{"verdict 1: missing check"}},
				map[string]any{"rejection_index": 2, "categories": []string{models.RejectionCauseLifecycleRetry}, "evidence": []string{"STALE_CALLER after reclaim"}},
			},
		})},
		"resume-rejection-rca": {"--disposition-file", writeRejectionRCAFile(t, "disposition.json", map[string]any{
			"schema_version": models.RejectionRCASchemaVersion,
			"recovery_path":  models.RecoveryLifecycleRepair,
			"rationale":      "Repair ownership; no code change is needed.",
		})},
	}
	return root, statePath, files
}

func writeRejectionRCAFile(t *testing.T, name string, payload any) string {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// resetRejectionRCAFileFlags clears the payload-file flags between in-process
// executions. The shared reset list does not know these two commands, and a
// sticky value would hide a missing flag from the next run.
func resetRejectionRCAFileFlags(t *testing.T) {
	t.Helper()
	reset := func() {
		for name, fileFlag := range map[string]string{"record-rejection-rca": "rca-file", "resume-rejection-rca": "disposition-file"} {
			cmd, _, err := rootCmd.Find([]string{name})
			if err != nil {
				t.Fatalf("find %s: %v", name, err)
			}
			if flag := cmd.Flags().Lookup(fileFlag); flag != nil {
				if err := flag.Value.Set(flag.DefValue); err != nil {
					t.Fatalf("reset --%s: %v", fileFlag, err)
				}
				flag.Changed = false
			}
		}
	}
	reset()
	t.Cleanup(reset)
}

func readStateBytes(t *testing.T, statePath string) string {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func envelopeResult(t *testing.T, stdout string) map[string]any {
	t.Helper()
	result, ok := parseEnvelope(t, stdout)["result"].(map[string]any)
	if !ok {
		t.Fatalf("envelope carries no result object: %s", stdout)
	}
	return result
}

func TestRejectionRCACommandWiring(t *testing.T) {
	resetRejectionRCAFileFlags(t)
	commandFlags := map[string]string{
		"record-rejection-rca": "rca-file",
		"resume-rejection-rca": "disposition-file",
	}

	t.Run("registered with own flags", func(t *testing.T) {
		for name, fileFlag := range commandFlags {
			cmd, _, err := rootCmd.Find([]string{name})
			if err != nil || cmd.Name() != name || cmd.Parent() != rootCmd {
				t.Fatalf("%s is not registered on the root command: %v", name, err)
			}
			for _, flag := range []string{"agent-id", "json", fileFlag, "request-id", "expected-transition"} {
				if cmd.LocalFlags().Lookup(flag) == nil {
					t.Errorf("%s lacks its own --%s flag", name, flag)
				}
			}
		}
	})

	t.Run("coder refused by validateAllowedOperation", func(t *testing.T) {
		resetRejectionRCAFileFlags(t)
		for name := range commandFlags {
			root, statePath, files := setupRejectionRCACLI(t)
			before := readStateBytes(t, statePath)
			args := append([]string{name, rejectionRCACLITask, "--agent-id", "coder-1", "--json"}, files[name]...)
			stdout, err := executeRootCommandCapture(t, root, args...)
			if err == nil {
				t.Fatalf("%s by a coder succeeded: %s", name, stdout)
			}
			envelope := parseEnvelope(t, stdout)
			assertLifecycleFailurePolicy(t, envelope, models.LifecycleForbidden, "stop")
			message, _ := envelope["error"].(map[string]any)["message"].(string)
			if want := `operation "` + name + `" not allowed for role "coder"`; !strings.Contains(message, want) {
				t.Fatalf("%s refusal is not the operation capability check %q: %s", name, want, message)
			}
			if readStateBytes(t, statePath) != before {
				t.Fatalf("refused %s changed state", name)
			}
		}
	})

	t.Run("missing payload file is a clean error", func(t *testing.T) {
		resetRejectionRCAFileFlags(t)
		for name, fileFlag := range commandFlags {
			root, statePath, _ := setupRejectionRCACLI(t)
			before := readStateBytes(t, statePath)
			resetRejectionRCAFileFlags(t)
			for _, tc := range []struct {
				label string
				extra []string
				want  string
			}{
				{"flag absent", nil, "--" + fileFlag + " is required"},
				{"file absent", []string{"--" + fileFlag, filepath.Join(t.TempDir(), "absent.json")}, "reading --" + fileFlag},
			} {
				args := append([]string{name, rejectionRCACLITask, "--agent-id", "orchestrator-1", "--json"}, tc.extra...)
				stdout, err := executeRootCommandCapture(t, root, args...)
				if err == nil {
					t.Fatalf("%s with %s succeeded: %s", name, tc.label, stdout)
				}
				envelope := parseEnvelope(t, stdout)
				assertLifecycleFailurePolicy(t, envelope, models.LifecycleInvalidInput, "correct_input")
				if message, _ := envelope["error"].(map[string]any)["message"].(string); !strings.Contains(message, tc.want) {
					t.Fatalf("%s with %s: error message %q lacks %q", name, tc.label, message, tc.want)
				}
			}
			if readStateBytes(t, statePath) != before {
				t.Fatalf("%s without a payload file changed state", name)
			}
		}
	})

	t.Run("json envelope for an invalid payload", func(t *testing.T) {
		resetRejectionRCAFileFlags(t)
		for name, fileFlag := range commandFlags {
			root, statePath, _ := setupRejectionRCACLI(t)
			before := readStateBytes(t, statePath)
			invalid := writeRejectionRCAFile(t, "invalid.json", map[string]any{"schema_version": 0, "threshold": 9})
			stdout, err := executeRootCommandCapture(t, root, name, rejectionRCACLITask,
				"--agent-id", "orchestrator-1", "--json", "--"+fileFlag, invalid)
			if err == nil {
				t.Fatalf("%s accepted an invalid payload: %s", name, stdout)
			}
			result := envelopeResult(t, stdout)
			if result["outcome"] != models.LifecycleInvalidInput || result["safe_action"] != "correct_input" {
				t.Fatalf("%s invalid payload result = %#v, want INVALID_INPUT/correct_input", name, result)
			}
			diagnostics, ok := result["diagnostics"].([]any)
			if !ok || len(diagnostics) == 0 {
				t.Fatalf("%s invalid payload result carries no diagnostics: %s", name, stdout)
			}
			for _, entry := range diagnostics {
				diagnostic, _ := entry.(map[string]any)
				if diagnostic["field"] == "" || diagnostic["constraint"] == "" || diagnostic["schema_version"] == nil {
					t.Fatalf("%s diagnostic lacks field, constraint or schema version: %#v", name, entry)
				}
			}
			if readStateBytes(t, statePath) != before {
				t.Fatalf("%s invalid payload changed state", name)
			}
		}
	})

	t.Run("json envelope on success", func(t *testing.T) {
		resetRejectionRCAFileFlags(t)
		root, statePath, files := setupRejectionRCACLI(t)
		for _, name := range []string{"record-rejection-rca", "resume-rejection-rca"} {
			args := append([]string{name, rejectionRCACLITask, "--agent-id", "orchestrator-1", "--json"}, files[name]...)
			stdout, err := executeRootCommandCapture(t, root, args...)
			if err != nil {
				t.Fatalf("%s: %v; %s", name, err, stdout)
			}
			result := envelopeResult(t, stdout)
			if result["outcome"] != models.LifecycleCompleted || result["safe_action"] != "continue" {
				t.Fatalf("%s result = %#v, want COMPLETED/continue", name, result)
			}
		}
		task := mustFindTask(t, readState(t, statePath), rejectionRCACLITask)
		if task.RejectionRCAGateOpen() || task.RejectionRCA.Disposition.RecoveryPath != models.RecoveryLifecycleRepair || task.Status != models.TaskStatusBlocked {
			t.Fatalf("CLI round trip left gate=%v disposition=%#v status=%s", task.RejectionRCAGateOpen(), task.RejectionRCA.Disposition, task.Status)
		}
	})
}
