package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestProviderDetectorsDistinguishDiagnosticsFromTranscript(t *testing.T) {
	detectors := []struct {
		name, message string
		detect        func(string) bool
	}{
		{"audit", "ERROR codex_core::session: failed to record rollout items: thread historical not found", func(s string) bool { return DetectProviderAuditDegraded(s, "codex") != nil }},
		{"quota", "You've hit your usage limit", func(s string) bool { return DetectQuotaExhaustion(s, "codex") != nil }},
		{"unavailable", "Codex cannot access session files under .codex/sessions (permission denied)", func(s string) bool { return DetectProviderUnavailable(s, "codex") != nil }},
	}
	for _, detector := range detectors {
		t.Run(detector.name, func(t *testing.T) {
			quoted, err := json.Marshal(detector.message)
			if err != nil {
				t.Fatal(err)
			}
			command := fmt.Sprintf(`{"type":"item.completed","item":{"type":"command_execution","aggregated_output":%s,"exit_code":0}}`, quoted)
			cases := []struct {
				name, output string
				want         bool
			}{
				{"plain diagnostic", detector.message, true},
				{"error event", fmt.Sprintf(`{"type":"error","message":%s}`, quoted), true},
				{"failed turn", fmt.Sprintf(`{"type":"turn.failed","error":{"message":%s}}`, quoted), true},
				{"quoted command output", command, false},
				{"assistant quote", fmt.Sprintf(`{"type":"item.completed","item":{"type":"agent_message","text":%s}}`, quoted), false},
				{"tool result", fmt.Sprintf(`{"type":"user","message":{"content":[{"type":"tool_result","content":%s}]}}`, quoted), false},
				{"successful result quote", fmt.Sprintf(`{"type":"result","is_error":false,"result":%s}`, quoted), false},
				{"failed result", fmt.Sprintf(`{"type":"result","is_error":true,"result":%s}`, quoted), true},
				{"incomplete event", command[:len(command)-1], false},
				{"quote then real diagnostic", command + "\n" + detector.message, true},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					if got := detector.detect(tc.output); got != tc.want {
						t.Fatalf("provider signal = %t, want %t", got, tc.want)
					}
				})
			}
		})
	}
}

func TestProviderAuditDiagnosticDoesNotPersistProviderPayload(t *testing.T) {
	output := `{"type":"error","message":"failed to record rollout items: thread missing not found","payload":"arbitrary transcript data"}`
	result := DetectProviderAuditDegraded(output, "codex")
	if result == nil || result.Message != "failed to record rollout items: thread not found" {
		t.Fatal("audit detection must retain only its canonical bounded diagnostic")
	}
}
