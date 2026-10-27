package agent

import (
	"testing"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
)

// C2 P2c/P2d: the merge gate compares canonical provider keys for approvals and
// for the reviewer pool. Diversity stays a soft preference: no case refuses a
// merge, and recorded approval providers keep their tool names.

func c2GateKeys(config models.Config) agentlaunch.ProviderKeys {
	return agentlaunch.NewProviderKeys(agentlaunch.BuiltInAgentTools(), config)
}

func c2SharedKeyConfig() models.Config {
	return models.Config{AgentTools: map[string]models.AgentToolConfig{
		"zz-one": {Executable: "one", ProviderKey: "shared"},
		"zz-two": {Executable: "two", ProviderKey: "shared"},
	}}
}

func c2GateInput(keys agentlaunch.ProviderKeys, approvalProviders []string, poolProviders ...string) mergeGateInput {
	task := &models.Task{ID: "task-1", RolePair: "coding-pair"}
	for i, provider := range approvalProviders {
		task.Approvals = append(task.Approvals, models.Approval{Agent: "code-reviewer-" + string(rune('1'+i)), Provider: provider})
	}
	agents := map[string]models.Agent{}
	for i, provider := range poolProviders {
		agents["code-reviewer-"+string(rune('1'+i))] = models.Agent{Role: "code-reviewer", Provider: provider}
	}
	return mergeGateInput{
		task: task, agents: agents, effectiveQuorum: len(approvalProviders),
		providerDiversity: "preferred", reviewerRole: "code-reviewer", keys: keys,
	}
}

func TestC2MergeGateApprovalDiversityComparesCanonicalKeys(t *testing.T) {
	tests := []struct {
		name      string
		keys      agentlaunch.ProviderKeys
		approvals []string
		pool      []string
		wantExtra string
	}{
		{"codex and codex-acp approvals do not achieve diversity", c2GateKeys(models.Config{}), []string{"codex", "codex-acp"}, []string{"codex", "codex-acp"}, "diversity_not_achievable"},
		{"custom tools sharing a key do not achieve diversity", c2GateKeys(c2SharedKeyConfig()), []string{"zz-one", "zz-two"}, []string{"zz-one", "zz-two"}, "diversity_not_achievable"},
		{"a truly different backend still achieves diversity", c2GateKeys(models.Config{}), []string{"codex-acp", "claude"}, []string{"codex-acp", "claude"}, "diversity_achieved"},
		{"a different backend in the pool makes diversity merely unmet", c2GateKeys(models.Config{}), []string{"codex", "codex-acp"}, []string{"codex", "codex-acp", "claude"}, "diversity_not_met"},
		{"zero-value keys compare recorded names, as before C2", agentlaunch.ProviderKeys{}, []string{"codex", "codex-acp"}, []string{"codex", "codex-acp"}, "diversity_achieved"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := evaluateMergeGate(c2GateInput(tt.keys, tt.approvals, tt.pool...))

			if !result.proceed {
				t.Fatalf("a merge gate that prefers diversity must not refuse: %s", result.skipReason)
			}
			if got, ok := result.extra[tt.wantExtra]; !ok || got != true {
				t.Fatalf("extra = %v, want %s=true", result.extra, tt.wantExtra)
			}
			if tt.wantExtra != "diversity_achieved" {
				if _, achieved := result.extra["diversity_achieved"]; achieved {
					t.Fatalf("extra = %v: same-key approvals must never set diversity_achieved", result.extra)
				}
			}
		})
	}
}

func TestC2MergeGateAliasOnlyPoolNamesSmallestRecordedProvider(t *testing.T) {
	result := evaluateMergeGate(c2GateInput(c2GateKeys(models.Config{}), []string{"codex-acp", "codex"}, "codex-acp", "codex", "codex-acp"))

	if !result.proceed {
		t.Fatalf("proceed = false: %s", result.skipReason)
	}
	if got := result.extra["diversity_reason"]; got != "all reviewers use provider codex" {
		t.Fatalf("diversity_reason = %v, want the group named by its smallest recorded provider", got)
	}
}

func TestC2MergeGateQuorumShortfallUnchanged(t *testing.T) {
	input := c2GateInput(c2GateKeys(models.Config{}), []string{"codex"}, "codex")
	input.effectiveQuorum = 2

	result := evaluateMergeGate(input)

	if result.proceed || result.skipReason == "" {
		t.Fatalf("proceed=%v reason=%q, want the quorum defense to skip the merge", result.proceed, result.skipReason)
	}
}

// Existing approvals whose reviewers are no longer registered evaluate under the
// current config: the override splits the alias and flips the outcome, and the
// task's persisted providers are untouched.
func TestC2MergeGatePersistedApprovalsFollowCurrentConfig(t *testing.T) {
	approvals := []string{"codex", "codex-acp"}
	input := c2GateInput(c2GateKeys(models.Config{}), approvals)
	input.agents = map[string]models.Agent{}

	if _, achieved := evaluateMergeGate(input).extra["diversity_achieved"]; achieved {
		t.Fatal("aliases are one provider under the default config")
	}
	input.keys = c2GateKeys(models.Config{AgentTools: map[string]models.AgentToolConfig{"codex-acp": {ProviderKey: "acp-lane"}}})
	if got := evaluateMergeGate(input).extra["diversity_achieved"]; got != true {
		t.Fatal("the override makes codex-acp a different provider: diversity achieved")
	}
	for i, approval := range input.task.Approvals {
		if approval.Provider != approvals[i] {
			t.Fatalf("approval %d provider = %q, want the stored tool name %q", i, approval.Provider, approvals[i])
		}
	}
}

// The merge batch derives keys from the snapshot it evaluates and merges the
// task: aliases never count as diversity, yet the merge proceeds.
func TestC2MergeGateBatchTreatsAliasApprovalsAsOneProvider(t *testing.T) {
	task := runSupervisorFinalPlanningQuorumApprovalAutoMerges(t, "codex-acp")

	if task.Approvals[0].Provider != "codex-acp" {
		t.Fatalf("approval provider = %q, want the stored tool name codex-acp", task.Approvals[0].Provider)
	}
	for _, entry := range task.History {
		if entry.Event != models.TaskEventMerged {
			continue
		}
		if _, achieved := entry.Extra["diversity_achieved"]; achieved {
			t.Fatalf("merged history %+v: codex-acp and codex approvals must not achieve diversity", entry.Extra)
		}
		if got := entry.Extra["diversity_not_achievable"]; got != true {
			t.Fatalf("merged history %+v, want diversity_not_achievable=true for an alias-only pool", entry.Extra)
		}
		return
	}
	t.Fatalf("merged history entry not found: %+v", task.History)
}

// The batch derives keys from the config of the snapshot it reads, so custom
// tools sharing a provider_key collapse there as well.
func TestC2MergeGateBatchUsesSnapshotConfig(t *testing.T) {
	task := runSupervisorFinalPlanningQuorumApprovalAutoMergesWith(t, "zz-one", "zz-two", func(config *models.Config) {
		config.AgentTools = c2SharedKeyConfig().AgentTools
	})

	for _, entry := range task.History {
		if entry.Event != models.TaskEventMerged {
			continue
		}
		if _, achieved := entry.Extra["diversity_achieved"]; achieved {
			t.Fatalf("merged history %+v: tools sharing a provider_key must not achieve diversity", entry.Extra)
		}
		if got := entry.Extra["diversity_not_achievable"]; got != true {
			t.Fatalf("merged history %+v, want diversity_not_achievable=true", entry.Extra)
		}
		return
	}
	t.Fatalf("merged history entry not found: %+v", task.History)
}
