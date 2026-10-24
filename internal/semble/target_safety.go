package semble

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

var defaultIgnorePatternsTail = []string{
	".worktrees/",
	"stacklit.json",
	"*.scip",
	".env",
	".env.*",
	"*.env",
	"credentials.*",
	"secrets.*",
	"*secret*.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.jks",
	"*_rsa",
	"*_dsa",
	"*_ecdsa",
	"*_ed25519",
	"*.keystore",
	"*.truststore",
	"config/secrets/",
	"**/secrets/",
	"serviceAccountKey.json",
	"*-credentials.json",
}

// TargetKind identifies the Semble target-root safety rules to apply.
type TargetKind string

const (
	TargetKindProjectRoot  TargetKind = "project_root"
	TargetKindTaskWorktree TargetKind = "task_worktree"
)

// TargetSafetyOptions identifies the intended Semble target root and caller
// context.
type TargetSafetyOptions struct {
	Kind                 TargetKind
	TargetRoot           string
	ExpectedWorktreeRoot string
}

// TargetSafetyResult reports whether a target root is safe for Semble guidance.
type TargetSafetyResult struct {
	Safe                  bool
	Kind                  TargetKind
	TargetRoot            string
	ExpectedWorktreeRoot  string
	MissingIgnorePatterns []string
	Diagnostic            Diagnostic
}

// DefaultIgnorePatterns returns the ordered Semble ignore source of truth for
// runtime, generated-index, and credential exclusions.
func DefaultIgnorePatterns() []string {
	patterns := make([]string, 0, len(defaultIgnorePatternsTail)+1)
	patterns = append(patterns, brand.RuntimeValues().ProjectDirName+"/")
	patterns = append(patterns, defaultIgnorePatternsTail...)
	return patterns
}

// DefaultIgnorePayload returns the physical .sembleignore content shared by
// project-root and generated task-worktree safety setup.
func DefaultIgnorePayload() string {
	return strings.Join(DefaultIgnorePatterns(), "\n") + "\n"
}

// GeneratedWorktreeIgnorePayload returns the generated task-worktree
// .sembleignore content. Lifecycle callers own writing this payload.
func GeneratedWorktreeIgnorePayload() string {
	return DefaultIgnorePayload()
}

// EnsureProjectRootIgnore creates or verifies the project-root .sembleignore
// required before pairing SessionStart may advertise Semble.
func EnsureProjectRootIgnore(root string) TargetSafetyResult {
	targetRoot, err := normalizeRoot(root)
	if err != nil {
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: root,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble project root invalid", err.Error()),
			},
		}
	}

	ignorePath := filepath.Join(targetRoot, ".sembleignore")
	file, err := os.OpenFile(ignorePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return ValidateTargetSafety(TargetSafetyOptions{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
		})
	}
	if err != nil {
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble create project root .sembleignore", err.Error()),
			},
		}
	}

	_, writeErr := file.WriteString(DefaultIgnorePayload())
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(ignorePath)
		return TargetSafetyResult{
			Kind:       TargetKindProjectRoot,
			TargetRoot: targetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble write project root .sembleignore", writeErr.Error()),
			},
		}
	}

	return ValidateTargetSafety(TargetSafetyOptions{
		Kind:       TargetKindProjectRoot,
		TargetRoot: targetRoot,
	})
}

// ValidateTargetSafety checks whether a target root is safe for Semble prompt
// guidance in the supplied context.
func ValidateTargetSafety(opts TargetSafetyOptions) TargetSafetyResult {
	targetRoot, err := normalizeRoot(opts.TargetRoot)
	if err != nil {
		return TargetSafetyResult{
			Kind:       opts.Kind,
			TargetRoot: opts.TargetRoot,
			Diagnostic: Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble target root invalid", err.Error()),
			},
		}
	}

	result := TargetSafetyResult{
		Kind:       opts.Kind,
		TargetRoot: targetRoot,
	}
	switch opts.Kind {
	case TargetKindProjectRoot:
		missing, diagnostic := missingProjectIgnorePatterns(targetRoot)
		result.MissingIgnorePatterns = missing
		result.Diagnostic = diagnostic
		result.Safe = len(missing) == 0 && diagnostic == (Diagnostic{})
	case TargetKindTaskWorktree:
		expectedRoot, err := normalizeRoot(opts.ExpectedWorktreeRoot)
		if err != nil {
			result.Diagnostic = Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: boundedDiagnosticMessage("semble task worktree root invalid", err.Error()),
			}
			return result
		}
		result.ExpectedWorktreeRoot = expectedRoot
		if targetRoot != expectedRoot {
			result.Diagnostic = Diagnostic{
				Kind:    DiagnosticExecutionFailure,
				Message: "semble target root does not match explicit task worktree root",
			}
			return result
		}
		missing, diagnostic := missingTaskWorktreeIgnorePatterns(targetRoot)
		result.MissingIgnorePatterns = missing
		result.Diagnostic = diagnostic
		result.Safe = len(missing) == 0 && diagnostic == (Diagnostic{})
	default:
		result.Diagnostic = Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: boundedDiagnosticMessage("semble target kind invalid", string(opts.Kind)),
		}
	}
	return result
}

func normalizeRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("empty target root")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func missingProjectIgnorePatterns(root string) ([]string, Diagnostic) {
	return missingRequiredIgnorePatterns(root, "project root")
}

func missingTaskWorktreeIgnorePatterns(root string) ([]string, Diagnostic) {
	return missingRequiredIgnorePatterns(root, "task worktree")
}

func missingRequiredIgnorePatterns(root, targetLabel string) ([]string, Diagnostic) {
	content, err := os.ReadFile(filepath.Join(root, ".sembleignore"))
	if err != nil {
		return DefaultIgnorePatterns(), Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: boundedDiagnosticMessage("semble "+targetLabel+" .sembleignore missing", err.Error()),
		}
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		pattern := strings.TrimSpace(line)
		if pattern == "" || strings.HasPrefix(pattern, "#") {
			continue
		}
		present[pattern] = true
	}
	var missing []string
	for _, pattern := range DefaultIgnorePatterns() {
		if !present[pattern] {
			missing = append(missing, pattern)
		}
	}
	if len(missing) > 0 {
		return missing, Diagnostic{
			Kind:    DiagnosticExecutionFailure,
			Message: "semble " + targetLabel + " .sembleignore missing required patterns",
		}
	}
	return nil, Diagnostic{}
}

func pathWithin(root, path string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." && !filepath.IsAbs(rel))
}
