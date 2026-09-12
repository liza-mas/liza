package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestLifecycleAuthorityDoesNotDiscloseGenerations(t *testing.T) {
	const losing = "test-losing-generation-155"
	const current = "test-current-generation-155"
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1": {Generation: current},
	}}
	err := RequireAgentAuthority(state, models.AgentAuthority{ID: "coder-1", Generation: losing})
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) {
		t.Fatalf("expected authority rejection, got %v", err)
	}
	details, marshalErr := json.Marshal(authorityErr.SafeDetails())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	wrapped := fmt.Errorf("submission rejected: %w", err)
	var logs bytes.Buffer
	log.New(&logs, "", 0).Print(wrapped)
	for name, text := range map[string]string{
		"error": err.Error(), "wrapped": wrapped.Error(), "details": string(details), "log": logs.String(),
	} {
		for _, generation := range []string{losing, current} {
			if strings.Contains(text, generation) {
				t.Errorf("%s exposes a registration generation", name)
			}
		}
	}
	if !strings.Contains(err.Error(), "coder-1") {
		t.Error("redacted diagnostic must retain agent identity")
	}
	if action := authorityErr.SafeDetails()["safe_action"]; action != "stop" {
		t.Errorf("safe_action = %v, want stop", action)
	}
}
