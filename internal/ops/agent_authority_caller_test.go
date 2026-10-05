package ops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestAuthorityWrappedMutationCaller(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(statePath)
	authority := models.AgentAuthority{ID: "coder-1", Generation: "test-generation"}
	if err := bb.Write(&models.State{Agents: map[string]models.Agent{
		authority.ID: {Generation: authority.Generation},
	}}); err != nil {
		t.Fatal(err)
	}
	err := lifecycleMutation(bb, &authority)(func(state *models.State) error {
		data, err := os.ReadFile(statePath + ".lock.owner.json")
		if err != nil {
			return err
		}
		var metadata struct {
			Operation string `json:"operation"`
			Caller    string `json:"caller"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			return err
		}
		if metadata.Operation != "modify" {
			t.Errorf("operation = %q, want modify", metadata.Operation)
		}
		for _, name := range []string{"TestAuthorityWrappedMutationCaller", "ModifyWithAgentAuthority"} {
			if !strings.Contains(metadata.Caller, name) {
				t.Errorf("caller %q does not identify %s through lifecycle/authority wrappers", metadata.Caller, name)
			}
		}
		if strings.Contains(string(data), authority.Generation) {
			t.Error("owner diagnostics exposed registration authority")
		}
		state.Version++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
