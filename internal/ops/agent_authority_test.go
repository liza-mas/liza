package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestAgentAuthorityMutationFence(t *testing.T) {
	const (
		agentID     = "coder-1"
		generationA = "generation-a"
		generationB = "generation-b"
	)

	statePath := filepath.Join(t.TempDir(), "state.yaml")
	bb := db.New(statePath)
	state := &models.State{Agents: map[string]models.Agent{
		agentID: {
			Role:       "coder",
			Status:     models.AgentStatusIdle,
			Generation: generationB,
		},
	}}
	if err := bb.Write(state); err != nil {
		t.Fatalf("write state: %v", err)
	}

	for _, authority := range []models.AgentAuthority{
		{ID: agentID},
		{ID: agentID, Generation: generationA},
	} {
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state before stale mutation: %v", err)
		}

		err = ModifyWithAgentAuthority(bb, authority, func(current *models.State) error {
			agent := current.Agents[agentID]
			agent.Status = models.AgentStatusWorking
			current.Agents[agentID] = agent
			return nil
		})
		var authorityErr *AgentAuthorityError
		if !errors.As(err, &authorityErr) {
			t.Fatalf("error = %T %v, want *AgentAuthorityError", err, err)
		}
		for _, want := range []string{agentID, generationFingerprint(generationB)} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want %q", err, want)
			}
		}
		if authority.Generation == "" {
			if !strings.Contains(err.Error(), "missing") {
				t.Errorf("missing-authority error = %q, want missing diagnostic", err)
			}
		} else if !strings.Contains(err.Error(), generationFingerprint(generationA)) {
			t.Errorf("stale-authority error lacks losing-generation fingerprint: %v", err)
		}

		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("read state after stale mutation: %v", err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("stale mutation changed current-generation state\nbefore:\n%s\nafter:\n%s", before, after)
		}
	}

	if err := ModifyWithAgentAuthority(bb, models.AgentAuthority{ID: agentID, Generation: generationB}, func(current *models.State) error {
		agent := current.Agents[agentID]
		agent.Status = models.AgentStatusWorking
		current.Agents[agentID] = agent
		return nil
	}); err != nil {
		t.Fatalf("current-generation mutation: %v", err)
	}
	updated, err := bb.Read()
	if err != nil {
		t.Fatalf("read updated state: %v", err)
	}
	if updated.Agents[agentID].Status != models.AgentStatusWorking {
		t.Fatalf("current-generation status = %s, want WORKING", updated.Agents[agentID].Status)
	}
}
