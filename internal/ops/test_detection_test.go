package ops

import (
	"os"
	"path/filepath"
	"slices"
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
		{"Node ESM test", "experiments/offline-lifecycle/tests/harness.test.mjs", true},
		{"Node ESM spec", "foo.spec.mjs", true},
		{"Node CommonJS test", "foo.test.cjs", true},
		{"Node CommonJS spec", "foo.spec.cjs", true},
		{"Node ESM non-test", "server.mjs", false},
		{"Node CommonJS non-test", "server.cjs", false},
		{"Node ESM helper in tests directory", "tests/fixture.mjs", false},
		{"Node CommonJS helper in tests directory", "tests/fixture.cjs", false},
		{"Node ESM backup", "harness.test.mjs.bak", false},
		{"Node CommonJS backup", "harness.spec.cjs.bak", false},

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
		{"Node ESM __tests__", "__tests__/foo.mjs", true},
		{"Node CommonJS nested __tests__", "src/__tests__/foo.cjs", true},

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

		// D68 incident: a pytest module without a recognized name is not admitted.
		{"Python module without test naming", "tests/validation/dev239_decision_load.py", false},
		{"Python incident module renamed", "tests/validation/test_dev239_decision_load.py", true},

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

// TestTestFileMatcherPatterns_SamplesAdmitted pins every displayed pattern to
// a nested sample isTestFile accepts. It catches a displayed pattern the
// matcher does not honor; a matcher branch missing from the list is not
// detectable here.
func TestTestFileMatcherPatterns_SamplesAdmitted(t *testing.T) {
	t.Parallel()

	samples := map[string]string{
		"*_test.go":                           "pkg/foo_test.go",
		"*_test.py":                           "pkg/foo_test.py",
		"test_*.py":                           "tests/validation/test_foo.py",
		"*.test.{js,ts,jsx,tsx,mjs,cjs}":      "src/foo.test.tsx",
		"*.spec.{js,ts,jsx,tsx,mjs,cjs}":      "src/foo.spec.mjs",
		"__tests__/*.{js,ts,jsx,tsx,mjs,cjs}": "src/__tests__/nested/foo.ts",
		"test_*.sh":                           "scripts/test_foo.sh",
		"*_test.sh":                           "scripts/foo_test.sh",
		"*_test.rb":                           "lib/foo_test.rb",
		"*_spec.rb":                           "spec/foo_spec.rb",
		"*Test.java":                          "src/FooTest.java",
		"Test*.java":                          "src/TestFoo.java",
		"*Tests.java":                         "src/FooTests.java",
		"*Test.kt":                            "src/FooTest.kt",
		"Test*.kt":                            "src/TestFoo.kt",
		"*Tests.kt":                           "src/FooTests.kt",
		"*Test.cs":                            "tests/FooTest.cs",
		"*Tests.cs":                           "tests/FooTests.cs",
		"*_test.rs":                           "src/foo_test.rs",
		"tests/*.rs":                          "crate/tests/nested/check.rs",
	}

	patterns := TestFileMatcherPatterns()
	if len(patterns) != len(samples) {
		t.Fatalf("patterns = %v, want one sample per pattern (%d samples)", patterns, len(samples))
	}
	for _, p := range patterns {
		sample, ok := samples[p]
		if !ok {
			t.Errorf("pattern %q has no admitted sample", p)
			continue
		}
		if !isTestFile(sample) {
			t.Errorf("pattern %q: isTestFile(%q) = false, want true", p, sample)
		}
	}

	patterns[0] = "mutated"
	if got := TestFileMatcherPatterns()[0]; got == "mutated" {
		t.Error("TestFileMatcherPatterns returned shared backing storage")
	}
}

func TestAnalyzeTestFiles_NodeModules(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	g := git.New(tmpDir)
	taskID := "node-tests"
	baseCommit, err := g.CreateWorktree(taskID, "main")
	if err != nil {
		t.Fatal(err)
	}
	wtPath := g.GetWorktreePath(taskID)
	testFiles := []string{
		"experiments/offline-lifecycle/tests/harness.spec.cjs",
		"experiments/offline-lifecycle/tests/harness.spec.mjs",
		"experiments/offline-lifecycle/tests/harness.test.cjs",
		"experiments/offline-lifecycle/tests/harness.test.mjs",
	}
	for _, name := range append(slices.Clone(testFiles), "experiments/offline-lifecycle/tests/fixture.mjs", "experiments/offline-lifecycle/server.cjs") {
		path := filepath.Join(wtPath, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("// Test-file recognition fixture.\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	testhelpers.MustGit(t, wtPath, "add", ".")
	testhelpers.MustGit(t, wtPath, "commit", "-m", "Add Node modules")

	diagnostics, err := AnalyzeTestFiles(g, taskID, baseCommit, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(diagnostics.TestFilesMatched, testFiles) {
		t.Errorf("matched test files = %v, want %v", diagnostics.TestFilesMatched, testFiles)
	}
	for _, pattern := range []string{"*.test.{js,ts,jsx,tsx,mjs,cjs}", "*.spec.{js,ts,jsx,tsx,mjs,cjs}", "__tests__/*.{js,ts,jsx,tsx,mjs,cjs}"} {
		if !slices.Contains(diagnostics.MatcherPatterns, pattern) {
			t.Errorf("diagnostic matcher patterns omit %q", pattern)
		}
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
