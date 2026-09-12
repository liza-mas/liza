package jsonout

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func TestLifecycleErrorEnvelopeHasSafeAction(t *testing.T) {
	const losing = "test-losing-generation-json-155"
	const current = "test-current-generation-json-155"
	state := &models.State{Agents: map[string]models.Agent{
		"coder-1": {Generation: current},
	}}
	authorityErr := ops.RequireAgentAuthority(state, models.AgentAuthority{ID: "coder-1", Generation: losing})
	var output bytes.Buffer
	err := WriteResult(&output, nil, nil, authorityErr)
	if !errors.Is(err, ErrAlreadyWritten) {
		t.Fatalf("authority rejection must retain hard-failure exit signaling: %v", err)
	}
	var envelope struct {
		OK     bool           `json:"ok"`
		Result map[string]any `json:"result"`
		Error  *ErrorDetail   `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error == nil {
		t.Error("authority rejection must remain an error envelope")
	}
	for key, want := range map[string]string{"outcome": "STALE_CALLER", "safe_action": "stop"} {
		if envelope.Result[key] != want {
			t.Errorf("result.%s = %v, want %s", key, envelope.Result[key], want)
		}
	}
	for _, generation := range []string{losing, current} {
		if strings.Contains(output.String(), generation) {
			t.Error("JSON envelope exposes a registration generation")
		}
	}
}
