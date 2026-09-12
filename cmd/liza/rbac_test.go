package main

import (
	"bytes"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// setupResolver creates a temp dir with embedded pipeline config and returns a resolver.
func setupResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	cfg, err := pipeline.LoadFrozen(tmpDir)
	if err != nil {
		t.Fatalf("failed to load pipeline config: %v", err)
	}
	return pipeline.NewResolver(cfg)
}

// --- validateAllowedOperation tests ---

func TestValidateAllowedOperation_HappyPath(t *testing.T) {
	resolver := setupResolver(t)
	err := validateAllowedOperation(resolver, "coder-1", "submit-for-review")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestValidateAllowedOperation_Rejection(t *testing.T) {
	resolver := setupResolver(t)
	err := validateAllowedOperation(resolver, "coder-1", "submit-verdict")
	if err == nil {
		t.Fatal("expected rejection error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{`operation "submit-verdict" not allowed for role "coder"`, "agent coder-1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

func TestValidateAllowedOperationEffectiveRoleCapabilities(t *testing.T) {
	resolver := setupResolver(t)

	allowed := []struct {
		agentID   string
		operation string
	}{
		{agentID: "integration-reviewer-1", operation: "submit-verdict"},
		{agentID: "coder-1", operation: "submit-for-review"},
		{agentID: "coder-1", operation: "mark-blocked"},
	}
	for _, test := range allowed {
		if err := validateAllowedOperation(resolver, test.agentID, test.operation); err != nil {
			t.Errorf("validateAllowedOperation(%q, %q): %v", test.agentID, test.operation, err)
		}
	}

	denied := []struct {
		agentID   string
		role      string
		operation string
	}{
		{agentID: "integration-reviewer-1", role: "integration-reviewer", operation: "mark-blocked"},
		{agentID: "coder-1", role: "coder", operation: "submit-verdict"},
	}
	for _, test := range denied {
		err := validateAllowedOperation(resolver, test.agentID, test.operation)
		if err == nil {
			t.Errorf("validateAllowedOperation(%q, %q): expected permission error", test.agentID, test.operation)
			continue
		}
		var permissionErr *lizaerrors.PermissionError
		if !stderrors.As(err, &permissionErr) {
			t.Errorf("validateAllowedOperation(%q, %q) error type = %T, want *errors.PermissionError", test.agentID, test.operation, err)
			continue
		}
		if permissionErr.AgentID != test.agentID || permissionErr.Role != test.role || permissionErr.Operation != test.operation {
			t.Errorf("PermissionError = %#v, want agent=%q role=%q operation=%q", permissionErr, test.agentID, test.role, test.operation)
		}
		wantReason := fmt.Sprintf("operation %q not allowed for role %q", test.operation, test.role)
		if !strings.Contains(permissionErr.Error(), wantReason) {
			t.Errorf("PermissionError %q missing %q", permissionErr.Error(), wantReason)
		}
	}
}

func TestIntegrationReviewerRenderedCLIFailureRecovery(t *testing.T) {
	const (
		roleName   = "integration-reviewer"
		reviewerID = roleName + "-1"
		taskID     = "task-integration-review-recovery"
		rolePair   = "integration-pair"
	)

	setupProject := func(t *testing.T) (string, string, *pipeline.Resolver, models.TaskStatus) {
		t.Helper()

		projectRoot, statePath := setupMutationTestProject(t, nil)
		cfg, err := pipeline.LoadFrozen(projectRoot)
		if err != nil {
			t.Fatalf("LoadFrozen: %v", err)
		}
		resolver := pipeline.NewResolver(cfg)
		reviewingStatus, err := resolver.ReviewingStatus(rolePair)
		if err != nil {
			t.Fatalf("ReviewingStatus(%q): %v", rolePair, err)
		}
		rejectedStatus, err := resolver.RejectedStatus(rolePair)
		if err != nil {
			t.Fatalf("RejectedStatus(%q): %v", rolePair, err)
		}

		state := readState(t, statePath)
		task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, time.Now().UTC())
		task.Type = models.TaskTypeIntegration
		task.RolePair = rolePair
		task.Status = reviewingStatus
		task.ReviewingBy = testhelpers.StringPtr(reviewerID)
		task.ReviewCommit = testhelpers.StringPtr(testhelpers.MustGit(t, projectRoot, "rev-parse", "HEAD"))
		state.Tasks = []models.Task{task}
		state.Agents[reviewerID] = mutationTestAgent(roleName)
		testhelpers.WriteInitialState(t, statePath, state)

		return projectRoot, statePath, resolver, rejectedStatus
	}

	renderedRecoveryOperation := func(t *testing.T, resolver *pipeline.Resolver, reviewCommit string) string {
		t.Helper()

		capabilities, err := resolver.EffectiveRoleCapabilities(roleName)
		if err != nil {
			t.Fatalf("EffectiveRoleCapabilities(%q): %v", roleName, err)
		}
		sections, err := resolver.ContextSections(roleName)
		if err != nil {
			t.Fatalf("ContextSections(%q): %v", roleName, err)
		}
		rendered, err := prompts.BuildRoleContext(roleName, sections, &prompts.RoleContextData{
			Role:         roleName,
			RoleType:     capabilities.RoleType,
			AgentID:      reviewerID,
			TaskID:       taskID,
			ReviewCommit: reviewCommit,
		})
		if err != nil {
			t.Fatalf("BuildRoleContext(%q): %v", roleName, err)
		}
		if !strings.Contains(rendered, "--review-commit "+reviewCommit) {
			t.Fatal("rendered recovery omitted the immutable reviewed boundary")
		}

		const executableLabel = "EXECUTABLE RECOVERY COMMAND:"
		if count := strings.Count(rendered, executableLabel); count != 1 {
			t.Fatalf("rendered recovery contains %d executable command labels, want 1:\n%s", count, rendered)
		}
		recovery := rendered[strings.Index(rendered, executableLabel)+len(executableLabel):]
		commandStart := strings.Index(recovery, "`")
		if commandStart < 0 {
			t.Fatalf("rendered recovery has no executable command:\n%s", recovery)
		}
		commandTail := recovery[commandStart+1:]
		commandEnd := strings.Index(commandTail, "`")
		if commandEnd < 0 {
			t.Fatalf("rendered recovery has an unterminated executable command:\n%s", recovery)
		}
		commandFields := strings.Fields(commandTail[:commandEnd])
		if len(commandFields) < 2 {
			t.Fatalf("rendered executable command has fewer than two fields: %q", commandTail[:commandEnd])
		}
		return commandFields[1]
	}

	const failingCommand = "liza await-resubmission task-integration-review-recovery --agent-id integration-reviewer-1 --json"
	const observedError = "rpc transport closed after 3 attempts"
	rejectionReason := fmt.Sprintf(
		"Repeated CLI failure. Exact command: %s. Observed error: %s.",
		failingCommand,
		observedError,
	)

	t.Run("executes authorized recovery and records exact evidence", func(t *testing.T) {
		projectRoot, statePath, resolver, rejectedStatus := setupProject(t)
		reviewCommit := *mustFindTask(t, readState(t, statePath), taskID).ReviewCommit
		operation := renderedRecoveryOperation(t, resolver, reviewCommit)
		if operation != "submit-verdict" {
			t.Fatalf("rendered recovery operation = %q, want submit-verdict", operation)
		}

		stdout, err := executeRootCommandCapture(t, projectRoot,
			operation, taskID, "REJECTED",
			"--review-commit", reviewCommit,
			"--reason", rejectionReason,
			"--agent-id", reviewerID,
			"--json",
		)
		if strings.Contains(strings.ToLower(stdout), "permission_denied") {
			t.Fatalf("rendered recovery returned permission_denied: %s", stdout)
		}
		if err != nil {
			t.Fatalf("rendered recovery command failed: %v\nstdout: %s", err, stdout)
		}

		state := readState(t, statePath)
		task := mustFindTask(t, state, taskID)
		if task.Status != rejectedStatus {
			t.Fatalf("task status = %s, want configured rejected status %s", task.Status, rejectedStatus)
		}
		if task.RejectionReason == nil || *task.RejectionReason != rejectionReason {
			t.Fatalf("rejection_reason = %v, want exact CLI failure evidence %q", task.RejectionReason, rejectionReason)
		}
		if len(task.History) == 0 {
			t.Fatal("expected rejected history entry")
		}
		last := task.History[len(task.History)-1]
		if last.Event != models.TaskEventRejected {
			t.Fatalf("history event = %q, want %q", last.Event, models.TaskEventRejected)
		}
		if last.Agent == nil || *last.Agent != reviewerID {
			t.Fatalf("history agent = %v, want %q", last.Agent, reviewerID)
		}
		if last.Reason == nil || *last.Reason != rejectionReason {
			t.Fatalf("history reason = %v, want exact CLI failure evidence %q", last.Reason, rejectionReason)
		}
		if last.Commit == nil || *last.Commit != reviewCommit {
			t.Fatalf("history commit = %v, want immutable reviewed boundary %s", last.Commit, reviewCommit)
		}
	})

	t.Run("keeps mark-blocked denied without changing state", func(t *testing.T) {
		projectRoot, statePath, _, _ := setupProject(t)
		before, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatalf("ReadFile() before mark-blocked: %v", err)
		}

		stdout, err := executeRootCommandCapture(t, projectRoot,
			"mark-blocked", taskID,
			"--reason", rejectionReason,
			"--questions", "Which prerequisite or external state must change?",
			"--agent-id", reviewerID,
			"--json",
		)
		if err == nil {
			t.Fatal("integration reviewer mark-blocked unexpectedly succeeded")
		}
		if !strings.Contains(strings.ToLower(stdout), "permission_denied") {
			t.Fatalf("mark-blocked output missing permission_denied:\n%s\nerror: %v", stdout, err)
		}

		after, readErr := os.ReadFile(statePath)
		if readErr != nil {
			t.Fatalf("ReadFile() after mark-blocked: %v", readErr)
		}
		if !bytes.Equal(after, before) {
			t.Fatal("denied integration reviewer mark-blocked changed task state")
		}
	})
}

func TestValidateAllowedOperation_InvalidAgentID(t *testing.T) {
	resolver := setupResolver(t)
	err := validateAllowedOperation(resolver, "badformat", "submit-for-review")
	if err == nil {
		t.Fatal("expected error for invalid agent ID, got nil")
	}
	msg := err.Error()
	for _, want := range []string{`cannot validate operation "submit-for-review"`, `agent "badformat"`, "invalid agent ID format"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

func TestValidateAllowedOperation_UnknownRole(t *testing.T) {
	resolver := setupResolver(t)
	err := validateAllowedOperation(resolver, "nonexistent-1", "submit-for-review")
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
	msg := err.Error()
	for _, want := range []string{`cannot validate operation "submit-for-review"`, `agent "nonexistent-1"`, "unknown role"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

// --- validateRoleType tests ---

func TestValidateRoleType_HappyPath(t *testing.T) {
	resolver := setupResolver(t)
	err := validateRoleType(resolver, "coder-1", "doer")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestValidateRoleType_Rejection(t *testing.T) {
	resolver := setupResolver(t)
	err := validateRoleType(resolver, "orchestrator-1", "doer")
	if err == nil {
		t.Fatal("expected rejection error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"command requires role type", `has type "orchestrator"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

func TestValidateRoleType_InvalidAgentID(t *testing.T) {
	resolver := setupResolver(t)
	err := validateRoleType(resolver, "badformat", "doer")
	if err == nil {
		t.Fatal("expected error for invalid agent ID, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"cannot validate role type [doer]", `agent "badformat"`, "invalid agent ID format"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

func TestValidateRoleType_UnknownRole(t *testing.T) {
	resolver := setupResolver(t)
	err := validateRoleType(resolver, "nonexistent-1", "doer")
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"cannot validate role type [doer]", `agent "nonexistent-1"`, "unknown role"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing expected substring %q", msg, want)
		}
	}
}

// --- loadResolverForRBAC tests ---

func TestLoadResolverForRBAC_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	resolver, err := loadResolverForRBAC(tmpDir)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestLoadResolverForRBAC_MissingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := loadResolverForRBAC(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
	var cfgErr *lizaerrors.PipelineConfigError
	if !stderrors.As(err, &cfgErr) {
		t.Fatalf("error type = %T, want *PipelineConfigError", err)
	}
	if cfgErr.Operation != "rbac" {
		t.Errorf("operation = %q, want rbac", cfgErr.Operation)
	}
}

// --- loadResolverFromDir tests ---

func TestLoadResolverFromDir_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	lizaDir := filepath.Join(tmpDir, paths.ProjectDirName())
	resolver, err := loadResolverFromDir(lizaDir)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if resolver == nil {
		t.Fatal("expected non-nil resolver")
	}
}

func TestLoadResolverFromDir_MissingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := loadResolverFromDir(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
	var cfgErr *lizaerrors.PipelineConfigError
	if !stderrors.As(err, &cfgErr) {
		t.Fatalf("error type = %T, want *PipelineConfigError", err)
	}
	if cfgErr.Operation != "rbac" {
		t.Errorf("operation = %q, want rbac", cfgErr.Operation)
	}
}
