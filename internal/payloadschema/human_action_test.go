package payloadschema

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestHumanActionSchema(t *testing.T) {
	t.Parallel()
	assessWith := func(humanAction any, clear any) map[string]any {
		payload := AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false)
		if humanAction != nil {
			payload["human_action"] = humanAction
		}
		if clear != nil {
			payload["clear_human_action"] = clear
		}
		return payload
	}
	markWith := func(humanAction string) MarkBlockedPayload {
		return MarkBlockedPayload{TaskID: "task-1", Reason: "r", Questions: []string{"q"}, HumanAction: humanAction}
	}

	for _, tt := range []struct {
		name      string
		operation string
		payload   any
		field     string
		class     string
	}{
		{"assess valid ask", "assess-blocked", assessWith("grant access", nil), "", ""},
		{"assess valid clear", "assess-blocked", assessWith(nil, true), "", ""},
		{"assess ask not a string", "assess-blocked", assessWith(42, nil), "/human_action", models.FieldValueClassWrongType},
		{"assess clear not a boolean", "assess-blocked", assessWith(nil, "yes"), "/clear_human_action", models.FieldValueClassWrongType},
		{"assess blank ask", "assess-blocked", assessWith("  ", nil), "/human_action", models.FieldValueClassMissing},
		{"assess multi-line ask", "assess-blocked", assessWith("a\nb", nil), "/human_action", models.FieldValueClassMalformed},
		{"assess ask with clear", "assess-blocked", assessWith("grant access", true), "/clear_human_action", models.FieldValueClassConflict},
		{"mark valid ask", MarkBlockedOperation, markWith("grant access"), "", ""},
		{"mark no ask", MarkBlockedOperation, markWith(""), "", ""},
		{"mark oversized ask", MarkBlockedOperation, markWith(strings.Repeat("a", 4097)), "/human_action", models.FieldValueClassOversized},
		{"mark multi-line ask", MarkBlockedOperation, markWith("a\r\nb"), "/human_action", models.FieldValueClassMalformed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, diagnostics, err := Validate(tt.operation, tt.payload)
			if err != nil {
				t.Fatal(err)
			}
			if tt.field == "" {
				if len(diagnostics) != 0 {
					t.Fatalf("diagnostics = %+v, want none", diagnostics)
				}
				return
			}
			if len(diagnostics) != 1 || diagnostics[0].Field != tt.field || diagnostics[0].ValueClass != tt.class {
				t.Fatalf("diagnostics = %+v; want one %s at %s", diagnostics, tt.class, tt.field)
			}
		})
	}

	if _, present := AssessBlockedPayload("task-1", "note", "", nil, nil, nil, false)["human_action"]; present {
		t.Fatal("the base payload gained a human_action key")
	}
	if payload := WithAssessBlockedHumanAction(AssessBlockedPayload("task-1", "", "", nil, nil, nil, false), "", false); len(payload) != 5 {
		t.Fatalf("empty human-ask options changed the payload shape: %v", payload)
	}
}
