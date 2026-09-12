package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const quarantinedVerdictTestCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestNonDefaultBrandQuarantinedVerdictHelp(t *testing.T) {
	bin := buildNonDefaultBrandBinary(t)
	submitHelp := runBrandSmokeCommand(t, bin, "submit-verdict", "--help")
	assertContains(t, submitHelp, "--review-commit")
	assertContains(t, submitHelp, "full immutable commit SHA actually reviewed")
	assertContains(t, submitHelp, "acme-agent submit-verdict")
	assertNoDefaultBrandLeaks(t, "submit-verdict help", submitHelp)
	reconcileHelp := runBrandSmokeCommand(t, bin, "reconcile-verdict", "--help")
	assertContains(t, reconcileHelp, "ACME_AGENT_AGENT_GENERATION")
	assertContains(t, reconcileHelp, "acme-agent reconcile-verdict")
	assertContains(t, reconcileHelp, "accepted|refuted|superseded|escalated")
	assertNoDefaultBrandLeaks(t, "reconcile-verdict help", reconcileHelp)
}

func setupQuarantinedVerdictCLI(t *testing.T) (string, string, string) {
	t.Helper()
	root, statePath := setupMutationTestProject(t, func(state *models.State) {
		task := testhelpers.BuildTaskByStatus("task-quarantined-cli", models.TaskStatusReviewing, time.Now().UTC())
		task.ReviewCommit = testhelpers.StringPtr(quarantinedVerdictTestCommit)
		state.Tasks = []models.Task{task}
		state.Agents["code-reviewer-1"] = mutationTestAgent("code-reviewer")
		state.Agents["coder-1"] = mutationTestAgent("coder")
	})
	_, err := ops.SubmitVerdictWithAuthority(root, "task-quarantined-cli", "REJECTED", "The shared version check is absent",
		models.AgentAuthority{ID: "code-reviewer-1", Generation: "quarantine-cli-old-fixture"}, "", quarantinedVerdictTestCommit)
	if !ops.IsAgentAuthorityError(err) {
		t.Fatalf("capture fenced finding: %v", err)
	}
	state := readState(t, statePath)
	if len(state.QuarantinedVerdicts) != 1 {
		t.Fatalf("captured findings = %d, want 1", len(state.QuarantinedVerdicts))
	}
	return root, statePath, state.QuarantinedVerdicts[0].ID
}

func TestReconcileVerdictCLIRequiresCurrentOrchestrator(t *testing.T) {
	for _, tc := range []struct {
		name, agentID, generation, want string
		swap                            bool
	}{
		{"missing generation", "orchestrator-1", "", "generation required", false},
		{"wrong role", "coder-1", testhelpers.TestAgentGeneration, "requires role type", false},
		{"stale generation", "orchestrator-1", "quarantine-cli-stale-fixture", "registration changed; stop", false},
		{"replacement after admission", "orchestrator-1", testhelpers.TestAgentGeneration, "registration changed; stop", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath, findingID := setupQuarantinedVerdictCLI(t)
			before := readState(t, statePath)
			previous := afterRBACAdmissionTestHook
			t.Cleanup(func() { afterRBACAdmissionTestHook = previous })
			replaced := false
			if tc.swap {
				afterRBACAdmissionTestHook = func(string) {
					if replaced {
						return
					}
					replaced = true
					if err := db.For(statePath).Modify(func(state *models.State) error {
						agent := state.Agents[tc.agentID]
						agent.Generation = "quarantine-cli-replacement-fixture"
						state.Agents[tc.agentID] = agent
						return nil
					}); err != nil {
						t.Fatal(err)
					}
					before = readState(t, statePath)
				}
			}
			stdout, err := executeGenerationRootCommandCapture(t, root, tc.generation,
				"reconcile-verdict", "task-quarantined-cli", findingID, "refuted",
				"--reason", "The existing check covers the boundary", "--agent-id", tc.agentID)
			if err == nil || !strings.Contains(stdout, tc.want) {
				t.Fatalf("unauthorized reconciliation = %v, output %s; want %q", err, stdout, tc.want)
			}
			if tc.name == "stale generation" || tc.swap {
				assertLifecycleFailurePolicy(t, parseEnvelope(t, stdout), models.LifecycleStaleCaller, "stop")
				for _, generation := range []string{tc.generation, testhelpers.TestAgentGeneration, "quarantine-cli-replacement-fixture"} {
					if strings.Contains(stdout, generation) || strings.Contains(stdout, fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))) {
						t.Fatal("reconciliation diagnostic exposes generation or fingerprint")
					}
				}
			}
			if tc.swap && !replaced {
				t.Fatal("did not exercise replacement after admission")
			}
			if after := readState(t, statePath); !reflect.DeepEqual(before, after) {
				t.Fatal("unauthorized reconciliation changed state")
			}
		})
	}
}

func TestReconcileVerdictCLIAppendsAuditedDecisions(t *testing.T) {
	root, statePath, findingID := setupQuarantinedVerdictCLI(t)
	before := readState(t, statePath)
	for i, disposition := range []string{"escalated", "accepted", "superseded", "refuted"} {
		for retry := 0; retry < 2; retry++ {
			stdout, err := executeRootCommandCapture(t, root, "reconcile-verdict", "task-quarantined-cli", findingID,
				disposition, "--reason", "Reviewed the evidence and recorded this decision", "--agent-id", "orchestrator-1", "--json")
			if err != nil || parseEnvelope(t, stdout)["ok"] != true {
				t.Fatalf("reconcile %s: %v; %s", disposition, err, stdout)
			}
			after := readState(t, statePath)
			if !reflect.DeepEqual(before.Tasks, after.Tasks) || !reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("reconciliation changed task or agent authority")
			}
			decisions := after.QuarantinedVerdicts[0].Reconciliations
			if len(decisions) != i+1 {
				t.Fatalf("decision count = %d, want %d after retry %d", len(decisions), i+1, retry)
			}
			decision := decisions[i]
			if decision.Actor != "orchestrator-1" || decision.Disposition != disposition || decision.Timestamp.IsZero() || decision.Reason == "" {
				t.Fatalf("incomplete reconciliation audit: %#v", decision)
			}
		}
	}
}

func TestReconcileVerdictMissingReasonPreservesJSONError(t *testing.T) {
	root, statePath, findingID := setupQuarantinedVerdictCLI(t)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, root, "reconcile-verdict", "task-quarantined-cli", findingID,
		"refuted", "--agent-id", "orchestrator-1", "--json")
	if err == nil {
		t.Fatal("reconciliation without a reason succeeded")
	}
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != false || !strings.Contains(envelope["error"].(map[string]any)["message"].(string), "reason") {
		t.Fatalf("missing reason lacks a structured error: %s", stdout)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("missing reconciliation reason changed state")
	}
}

func TestGetQuarantinedVerdicts(t *testing.T) {
	root, _, findingID := setupQuarantinedVerdictCLI(t)
	for _, format := range [][]string{{"--json"}, {"--format", "json"}, {"--format", "yaml"}} {
		args := append([]string{"get", "quarantined_verdicts"}, format...)
		var stdout string
		var err error
		if format[0] == "--json" {
			stdout, err = executeRootCommandCapture(t, root, args...)
		} else {
			// Formatted output uses Cobra's writer; --json writes to os.Stdout.
			resetRootCmdForTest(t)
			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetArgs(append([]string{"-C", root}, args...))
			err = rootCmd.Execute()
			stdout = output.String()
		}
		if err != nil {
			t.Fatalf("query quarantined evidence: %v; %s", err, stdout)
		}
		for _, want := range []string{findingID, quarantinedVerdictTestCommit, "The shared version check is absent", "generation_fingerprints"} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("query omitted %q: %s", want, stdout)
			}
		}
		for _, generation := range []string{"quarantine-cli-old-fixture", testhelpers.TestAgentGeneration} {
			if strings.Contains(stdout, generation) {
				t.Fatal("evidence query exposed a reusable generation")
			}
		}
	}
}

func TestSubmitVerdictCLIRequiresImmutableBoundary(t *testing.T) {
	for _, commit := range []string{"", "HEAD", "abc1234"} {
		t.Run("commit="+commit, func(t *testing.T) {
			root, statePath, _ := setupQuarantinedVerdictCLI(t)
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"submit-verdict", "task-quarantined-cli", "APPROVED", "--agent-id", "code-reviewer-1", "--json"}
			if commit != "" {
				args = append(args, "--review-commit", commit)
			}
			stdout, err := executeRootCommandCapture(t, root, args...)
			if err == nil || !strings.Contains(stdout, "review-commit") {
				t.Fatalf("invalid boundary accepted or unactionable: %v; %s", err, stdout)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("administrative failure changed state or manufactured evidence")
			}
		})
	}
}
