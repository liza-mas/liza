package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/pipeline"
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
	if !strings.Contains(err.Error(), "cannot authorize operation") {
		t.Errorf("error %q missing expected substring %q", err.Error(), "cannot authorize operation")
	}
}

// --- loadResolverFromDir tests ---

func TestLoadResolverFromDir_Success(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	lizaDir := filepath.Join(tmpDir, ".liza")
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
	if !strings.Contains(err.Error(), "cannot authorize operation") {
		t.Errorf("error %q missing expected substring %q", err.Error(), "cannot authorize operation")
	}
}
