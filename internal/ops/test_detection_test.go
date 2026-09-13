package ops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestIsTestFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		want bool
	}{
		// Go
		{"Go test file", "foo_test.go", true},
		{"Go test file in subdir", "pkg/bar_test.go", true},
		{"Go non-test file", "foo.go", false},

		// Python
		{"Python test_ prefix", "test_foo.py", true},
		{"Python _test suffix", "foo_test.py", true},
		{"Python non-test", "foo.py", false},

		// JavaScript
		{"JS .test.js", "foo.test.js", true},
		{"JS .spec.js", "foo.spec.js", true},
		{"JS non-test", "foo.js", false},

		// TypeScript
		{"TS .test.ts", "foo.test.ts", true},
		{"TS .spec.ts", "foo.spec.ts", true},
		{"TS non-test", "foo.ts", false},

		// JSX/TSX
		{"JSX .test.jsx", "Button.test.jsx", true},
		{"TSX .spec.tsx", "Button.spec.tsx", true},
		{"TSX .test.tsx", "Button.test.tsx", true},
		{"JSX .spec.jsx", "Button.spec.jsx", true},
		{"JSX non-test", "Button.jsx", false},

		// JS/TS __tests__/ directory
		{"JS __tests__ dir", "__tests__/foo.js", true},
		{"TS __tests__ nested", "src/__tests__/bar.ts", true},
		{"TSX __tests__", "components/__tests__/Button.tsx", true},
		{"JS not in __tests__", "src/foo.js", false},

		// Shell
		{"Shell test_ prefix", "test_integration.sh", true},
		{"Shell _test suffix", "integration_test.sh", true},
		{"Shell non-test", "deploy.sh", false},

		// Ruby
		{"Ruby _test.rb", "foo_test.rb", true},
		{"Ruby _spec.rb", "foo_spec.rb", true},
		{"Ruby non-test", "foo.rb", false},

		// Java
		{"Java Test suffix", "FooTest.java", true},
		{"Java Tests suffix", "FooTests.java", true},
		{"Java Test prefix", "TestFoo.java", true},
		{"Java non-test", "Foo.java", false},

		// Kotlin
		{"Kotlin Test suffix", "FooTest.kt", true},
		{"Kotlin Tests suffix", "FooTests.kt", true},
		{"Kotlin Test prefix", "TestFoo.kt", true},
		{"Kotlin non-test", "Foo.kt", false},

		// C#
		{"CSharp Test suffix", "SessionTest.cs", true},
		{"CSharp nested Tests suffix", "tests/access/authority/SessionTests.cs", true},
		{"CSharp configuration tests", "tests/access/authority/ConfigurationTests.cs", true},
		{"CSharp diagnostics tests", "tests/access/authority/DiagnosticsTests.cs", true},
		{"CSharp production file", "Session.cs", false},
		{"CSharp fixture in tests directory", "tests/AuthorityFixture.cs", false},
		{"CSharp helper prefix", "TestHelpers.cs", false},
		{"CSharp test project", "Access.Authority.Tests.csproj", false},
		{"CSharp backup file", "SessionTests.cs.bak", false},
		{"CSharp casing mismatch", "Sessiontests.cs", false},

		// Rust
		{"Rust _test.rs", "foo_test.rs", true},
		{"Rust tests/ dir", "tests/integration.rs", true},
		{"Rust nested tests/ dir", "crate/tests/foo.rs", true},
		{"Rust non-test", "foo.rs", false},

		// Edge cases
		{"Empty string", "", false},
		{"No extension", "test_file", false},
		{"Partial match", "test.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.file)
			if got != tt.want {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}

func TestHasTestFiles_WithTestFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	g := git.New(tmpDir)
	taskID := "task-1"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	wtPath := g.GetWorktreePath(taskID)

	// Add a test file
	testFile := filepath.Join(wtPath, "hello_test.go")
	if err := os.WriteFile(testFile, []byte("package hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "hello_test.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add test file")

	hasTests, err := HasTestFiles(g, taskID, baseCommit)
	if err != nil {
		t.Fatalf("HasTestFiles failed: %v", err)
	}
	if !hasTests {
		t.Error("Expected HasTestFiles to return true when test file is present")
	}
}

func TestHasTestFiles_WithShellTestFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	g := git.New(tmpDir)
	taskID := "task-1"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	wtPath := g.GetWorktreePath(taskID)

	// Add a shell test file (the pattern that triggered the original bug)
	testFile := filepath.Join(wtPath, "test_integration.sh")
	if err := os.WriteFile(testFile, []byte("#!/bin/bash\necho test\n"), 0755); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "test_integration.sh")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add shell test file")

	hasTests, err := HasTestFiles(g, taskID, baseCommit)
	if err != nil {
		t.Fatalf("HasTestFiles failed: %v", err)
	}
	if !hasTests {
		t.Error("Expected HasTestFiles to return true when shell test file is present")
	}
}

func TestHasTestFiles_WithNestedPythonTestFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	g := git.New(tmpDir)
	taskID := "task-nested-python-test"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	wtPath := g.GetWorktreePath(taskID)
	testDir := filepath.Join(wtPath, "tests", "backend")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(testDir, "test_source_resolution.py")
	if err := os.WriteFile(testFile, []byte("def test_source_resolution():\n    assert True\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "tests/backend/test_source_resolution.py")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Modify nested Python test")

	diagnostics, err := AnalyzeTestFiles(g, taskID, baseCommit, "HEAD")
	if err != nil {
		t.Fatalf("AnalyzeTestFiles failed: %v", err)
	}
	if len(diagnostics.TestFilesMatched) != 1 || diagnostics.TestFilesMatched[0] != "tests/backend/test_source_resolution.py" {
		t.Fatalf("TestFilesMatched = %v, want [tests/backend/test_source_resolution.py]", diagnostics.TestFilesMatched)
	}

	hasTests, err := HasTestFiles(g, taskID, baseCommit)
	if err != nil {
		t.Fatalf("HasTestFiles failed: %v", err)
	}
	if !hasTests {
		t.Error("Expected HasTestFiles to return true for nested Python test file")
	}
}

func TestHasTestFiles_WithoutTestFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	g := git.New(tmpDir)
	taskID := "task-1"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	wtPath := g.GetWorktreePath(taskID)

	// Add a non-test file only
	implFile := filepath.Join(wtPath, "hello.go")
	if err := os.WriteFile(implFile, []byte("package hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, wtPath, "add", "hello.go")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add implementation without tests")

	hasTests, err := HasTestFiles(g, taskID, baseCommit)
	if err != nil {
		t.Fatalf("HasTestFiles failed: %v", err)
	}
	if hasTests {
		t.Error("Expected HasTestFiles to return false when no test file is present")
	}
}

func TestHasTestFiles_NoChanges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	g := git.New(tmpDir)
	taskID := "task-1"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatalf("Failed to create worktree: %v", err)
	}

	// No commits since worktree creation
	hasTests, err := HasTestFiles(g, taskID, baseCommit)
	if err != nil {
		t.Fatalf("HasTestFiles failed: %v", err)
	}
	if hasTests {
		t.Error("Expected HasTestFiles to return false when no changes exist")
	}
}
