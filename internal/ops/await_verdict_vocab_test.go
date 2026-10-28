package ops

import (
	"encoding/json"
	"testing"
)

// TestAwaitVerdictVocabularyKeepsItsValues compares each await outcome and safe
// action with the literal the --json output carried before the K7 types, so a
// renamed or respelled constant fails here rather than in a caller.
func TestAwaitVerdictVocabularyKeepsItsValues(t *testing.T) {
	outcomes := map[AwaitVerdictOutcome]string{
		VerdictApproved:            "APPROVED",
		VerdictRejected:            "REJECTED",
		VerdictNewAttempt:          "NEW_ATTEMPT",
		VerdictAlreadyTransitioned: "ALREADY_TRANSITIONED",
		VerdictTerminal:            "TERMINAL",
		VerdictTimeout:             "TIMEOUT",
		VerdictAborted:             "ABORTED",
		VerdictPoll:                "POLL",
		VerdictPaused:              "PAUSED",
	}
	if len(outcomes) != 9 {
		t.Fatalf("outcome set has %d distinct constants, want 9", len(outcomes))
	}
	for got, want := range outcomes {
		if string(got) != want {
			t.Errorf("outcome constant = %q, want %q", got, want)
		}
	}

	actions := map[AwaitSafeAction]string{
		SafeActionRevise: "revise",
		SafeActionStop:   "stop",
	}
	if len(actions) != 2 {
		t.Fatalf("safe action set has %d distinct constants, want 2", len(actions))
	}
	for got, want := range actions {
		if string(got) != want {
			t.Errorf("safe action constant = %q, want %q", got, want)
		}
	}
}

// TestAwaitVerdictResultKeepsStringFields pins the wire form: the typed constants
// reach the result by conversion, and the fields marshal as plain JSON strings.
func TestAwaitVerdictResultKeepsStringFields(t *testing.T) {
	encoded, err := json.Marshal(AwaitVerdictResult{
		Verdict:    string(VerdictAlreadyTransitioned),
		SafeAction: string(SafeActionRevise),
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded["verdict"].(string); !ok || got != "ALREADY_TRANSITIONED" {
		t.Errorf("verdict = %#v, want the string ALREADY_TRANSITIONED", decoded["verdict"])
	}
	if got, ok := decoded["safe_action"].(string); !ok || got != "revise" {
		t.Errorf("safe_action = %#v, want the string revise", decoded["safe_action"])
	}
}
