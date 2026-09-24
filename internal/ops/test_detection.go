package ops

import (
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/git"
)

var testFileMatcherPatterns = []string{
	"*_test.go",
	"*_test.py",
	"test_*.py",
	"*.test.{js,ts,jsx,tsx,mjs,cjs}",
	"*.spec.{js,ts,jsx,tsx,mjs,cjs}",
	"__tests__/*.{js,ts,jsx,tsx,mjs,cjs}",
	"test_*.sh",
	"*_test.sh",
	"*_test.rb",
	"*_spec.rb",
	"*Test.java",
	"Test*.java",
	"*Tests.java",
	"*Test.kt",
	"Test*.kt",
	"*Tests.kt",
	"*Test.cs",
	"*Tests.cs",
	"*_test.rs",
	"tests/*.rs",
}

// TestFileMatcherPatterns returns a copy of the display patterns for the TDD
// admission gate, as reported in diagnostics and rendered into prompts. They
// describe isTestFile; they are not root-relative globs.
func TestFileMatcherPatterns() []string {
	return append([]string(nil), testFileMatcherPatterns...)
}

// TestFileDiagnostics records the exact range and matcher results used by the
// TDD gate. It is safe to expose in JSON errors.
type TestFileDiagnostics struct {
	BaseRef                string   `json:"base_ref"`
	HeadRef                string   `json:"head_ref"`
	ChangedFilesConsidered []string `json:"changed_files_considered"`
	TestFilesMatched       []string `json:"test_files_matched"`
	MatcherPatterns        []string `json:"matcher_patterns"`
}

func (d TestFileDiagnostics) Details() map[string]any {
	return map[string]any{
		"base_ref":                 d.BaseRef,
		"head_ref":                 d.HeadRef,
		"changed_files_considered": d.ChangedFilesConsidered,
		"test_files_matched":       d.TestFilesMatched,
		"matcher_patterns":         d.MatcherPatterns,
	}
}

// isTestFile returns true if the filename matches known test file patterns
// across Go, Python, JS/TS, Shell, Ruby, Java, Kotlin, C#, and Rust.
func isTestFile(name string) bool {
	base := filepath.Base(name)

	// Go: *_test.go
	if strings.HasSuffix(base, "_test.go") {
		return true
	}

	// Python: *_test.py, test_*.py
	if strings.HasSuffix(base, "_test.py") || (strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py")) {
		return true
	}

	// JS/TS: .test/.spec files or modules under __tests__, including Node ESM/CommonJS.
	for _, ext := range []string{".js", ".ts", ".jsx", ".tsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(base, ".test"+ext) || strings.HasSuffix(base, ".spec"+ext) {
			return true
		}
		if strings.HasSuffix(base, ext) {
			slashed := filepath.ToSlash(name)
			if strings.Contains(slashed, "/__tests__/") || strings.HasPrefix(slashed, "__tests__/") {
				return true
			}
		}
	}

	// Shell: test_*.sh, *_test.sh
	if strings.HasSuffix(base, ".sh") {
		noExt := strings.TrimSuffix(base, ".sh")
		if strings.HasPrefix(noExt, "test_") || strings.HasSuffix(noExt, "_test") {
			return true
		}
	}

	// Ruby: *_test.rb, *_spec.rb
	if strings.HasSuffix(base, "_test.rb") || strings.HasSuffix(base, "_spec.rb") {
		return true
	}

	// Java: *Test.java, Test*.java, *Tests.java
	if strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java") ||
		(strings.HasPrefix(base, "Test") && strings.HasSuffix(base, ".java")) {
		return true
	}

	// Kotlin: *Test.kt, Test*.kt, *Tests.kt
	if strings.HasSuffix(base, "Test.kt") || strings.HasSuffix(base, "Tests.kt") ||
		(strings.HasPrefix(base, "Test") && strings.HasSuffix(base, ".kt")) {
		return true
	}

	// C#: *Test.cs, *Tests.cs
	if strings.HasSuffix(base, "Test.cs") || strings.HasSuffix(base, "Tests.cs") {
		return true
	}

	// Rust: *_test.rs, or any .rs file under a tests/ directory
	if strings.HasSuffix(base, "_test.rs") {
		return true
	}
	if strings.HasSuffix(base, ".rs") {
		slashed := filepath.ToSlash(name)
		if strings.Contains(slashed, "/tests/") || strings.HasPrefix(slashed, "tests/") {
			return true
		}
	}

	return false
}

// AnalyzeTestFiles returns the full TDD gate analysis for files changed between
// baseCommit and headRef in the task worktree.
func AnalyzeTestFiles(g *git.Git, taskID, baseCommit, headRef string) (*TestFileDiagnostics, error) {
	wtPath := g.GetWorktreePath(taskID)
	files, err := g.DiffFiles(wtPath, baseCommit, headRef)
	if err != nil {
		return nil, err
	}
	matched := make([]string, 0)
	for _, f := range files {
		if isTestFile(f) {
			matched = append(matched, f)
		}
	}
	return &TestFileDiagnostics{
		BaseRef:                baseCommit,
		HeadRef:                headRef,
		ChangedFilesConsidered: files,
		TestFilesMatched:       matched,
		MatcherPatterns:        TestFileMatcherPatterns(),
	}, nil
}

// HasTestFiles checks whether the commits between baseCommit and HEAD in the
// task worktree include any test files (added or modified).
func HasTestFiles(g *git.Git, taskID, baseCommit string) (bool, error) {
	diagnostics, err := AnalyzeTestFiles(g, taskID, baseCommit, "HEAD")
	if err != nil {
		return false, err
	}
	return len(diagnostics.TestFilesMatched) > 0, nil
}
