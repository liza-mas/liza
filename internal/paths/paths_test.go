package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestLizaPaths creates a LizaPaths instance for testing purposes.
func newTestLizaPaths(projectRoot string) LizaPaths {
	return LizaPaths{projectRoot: projectRoot}
}

func TestConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		want     string
	}{
		{
			name:     "LizaDirName",
			constant: LizaDirName,
			want:     ".liza",
		},
		{
			name:     "StateFileName",
			constant: StateFileName,
			want:     "state.yaml",
		},
		{
			name:     "LogFileName",
			constant: LogFileName,
			want:     "log.yaml",
		},
		{
			name:     "LockSuffix",
			constant: LockSuffix,
			want:     ".lock",
		},
		{
			name:     "AlertsLogFileName",
			constant: AlertsLogFileName,
			want:     "alerts.log",
		},
		{
			name:     "SprintSummaryFileName",
			constant: SprintSummaryFileName,
			want:     "sprint_summary.md",
		},
		{
			name:     "CircuitBreakerReportFileName",
			constant: CircuitBreakerReportFileName,
			want:     "circuit_breaker_report.md",
		},
		{
			name:     "ArchiveDirName",
			constant: ArchiveDirName,
			want:     "archive",
		},
		{
			name:     "TaskBranchPrefix",
			constant: TaskBranchPrefix,
			want:     "task/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.want {
				t.Errorf("%s = %v, want %v", tt.name, tt.constant, tt.want)
			}
		})
	}
}

func TestGetProjectRoot(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) string // returns the directory to run test from
		want    string                    // relative to temp dir or actual path
		wantErr bool
	}{
		{
			name: "regular git repository",
			setup: func(t *testing.T) string {
				// Create a temporary git repo
				tmpDir := t.TempDir()
				gitDir := filepath.Join(tmpDir, ".git")
				if err := os.Mkdir(gitDir, 0755); err != nil {
					t.Fatal(err)
				}
				// Initialize basic git structure
				if err := os.Mkdir(filepath.Join(gitDir, "objects"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(gitDir, "refs"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\trepositoryformatversion = 0\n"), 0644); err != nil {
					t.Fatal(err)
				}
				return tmpDir
			},
			want:    "", // will be set to tmpDir
			wantErr: false,
		},
		{
			name: "not a git repository",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				return tmpDir
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDir := tt.setup(t)
			originalDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			defer os.Chdir(originalDir)

			if err := os.Chdir(testDir); err != nil {
				t.Fatal(err)
			}

			got, err := GetProjectRoot()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProjectRoot() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// For regular repo test, expect the temp dir
				// Need to resolve symlinks on both sides for comparison (macOS /var -> /private/var)
				if tt.name == "regular git repository" {
					wantResolved, err := filepath.EvalSymlinks(testDir)
					if err != nil {
						t.Fatal(err)
					}
					if got != wantResolved {
						t.Errorf("GetProjectRoot() = %v, want %v", got, wantResolved)
					}
				}
			}
		})
	}
}

func TestLizaPathsFromGit(t *testing.T) {
	// Create a temporary git repo
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.Mkdir(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Initialize basic git structure
	if err := os.Mkdir(filepath.Join(gitDir, "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(gitDir, "refs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n\trepositoryformatversion = 0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(originalDir)

	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	paths, err := LizaPathsFromGit()
	if err != nil {
		t.Fatalf("LizaPathsFromGit() error = %v", err)
	}

	// Resolve symlinks for comparison (macOS /var -> /private/var)
	tmpDirResolved, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify ProjectRoot is correct
	if paths.ProjectRoot() != tmpDirResolved {
		t.Errorf("ProjectRoot() = %v, want %v", paths.ProjectRoot(), tmpDirResolved)
	}

	// Verify all path methods return correct values
	expectedLizaDir := filepath.Join(tmpDirResolved, ".liza")
	if paths.LizaDir() != expectedLizaDir {
		t.Errorf("LizaDir() = %v, want %v", paths.LizaDir(), expectedLizaDir)
	}

	expectedState := filepath.Join(expectedLizaDir, "state.yaml")
	if paths.StatePath() != expectedState {
		t.Errorf("StatePath() = %v, want %v", paths.StatePath(), expectedState)
	}

	expectedLog := filepath.Join(expectedLizaDir, "log.yaml")
	if paths.LogPath() != expectedLog {
		t.Errorf("LogPath() = %v, want %v", paths.LogPath(), expectedLog)
	}

	expectedLock := expectedState + ".lock"
	if paths.LockPath() != expectedLock {
		t.Errorf("LockPath() = %v, want %v", paths.LockPath(), expectedLock)
	}
}

func TestISOTimestamp(t *testing.T) {
	ts := ISOTimestamp()

	// Verify format: YYYY-MM-DDTHH:MM:SSZ
	if len(ts) != 20 {
		t.Errorf("ISOTimestamp() length = %d, want 20", len(ts))
	}

	if !strings.HasSuffix(ts, "Z") {
		t.Errorf("ISOTimestamp() = %v, should end with 'Z'", ts)
	}

	// Parse to verify it's valid ISO8601
	_, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Errorf("ISOTimestamp() = %v, not valid RFC3339: %v", ts, err)
	}

	// Verify it's recent (within 1 second)
	parsed, _ := time.Parse(time.RFC3339, ts)
	now := time.Now().UTC()
	diff := now.Sub(parsed)
	if diff < 0 || diff > time.Second {
		t.Errorf("ISOTimestamp() = %v, time difference from now = %v, want < 1s", ts, diff)
	}
}

func TestISOTimestampOffset(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		wantDiff time.Duration // acceptable difference
	}{
		{
			name:     "60 seconds in future",
			duration: 60 * time.Second,
			wantDiff: time.Second,
		},
		{
			name:     "5 minutes in future",
			duration: 5 * time.Minute,
			wantDiff: time.Second,
		},
		{
			name:     "1 hour in past",
			duration: -1 * time.Hour,
			wantDiff: time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := ISOTimestampOffset(tt.duration)

			// Parse the timestamp
			parsed, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				t.Fatalf("ISOTimestampOffset() = %v, not valid RFC3339: %v", ts, err)
			}

			// Calculate expected time
			expected := time.Now().UTC().Add(tt.duration)
			diff := parsed.Sub(expected)
			if diff < 0 {
				diff = -diff
			}

			if diff > tt.wantDiff {
				t.Errorf("ISOTimestampOffset(%v) = %v, diff from expected = %v, want < %v",
					tt.duration, ts, diff, tt.wantDiff)
			}
		})
	}
}

func TestValidatePath(t *testing.T) {
	// Use a platform-appropriate absolute path so the "valid" case holds on
	// Windows (where /tmp/test is not absolute) as well as Unix.
	absPath := filepath.Join(t.TempDir(), "test")
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid absolute path",
			path:    absPath,
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "relative path",
			path:    "relative/path",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLizaPathsMethods(t *testing.T) {
	// Create mock LizaPaths instance (no git repo needed for path construction tests)
	mockPaths := newTestLizaPaths("/mock/project")

	tests := []struct {
		name     string
		method   func() string
		expected string
	}{
		{
			name:     "StatePath returns State field",
			method:   mockPaths.StatePath,
			expected: "/mock/project/.liza/state.yaml",
		},
		{
			name:     "LogPath returns Log field",
			method:   mockPaths.LogPath,
			expected: "/mock/project/.liza/log.yaml",
		},
		{
			name:     "LockPath returns Lock field",
			method:   mockPaths.LockPath,
			expected: "/mock/project/.liza/state.yaml.lock",
		},
		{
			name:     "AlertsLogPath constructs alerts.log path",
			method:   mockPaths.AlertsLogPath,
			expected: "/mock/project/.liza/alerts.log",
		},
		{
			name:     "SprintSummaryPath constructs sprint_summary.md path",
			method:   mockPaths.SprintSummaryPath,
			expected: "/mock/project/.liza/sprint_summary.md",
		},
		{
			name:     "CircuitBreakerReportPath constructs circuit_breaker_report.md path",
			method:   mockPaths.CircuitBreakerReportPath,
			expected: "/mock/project/.liza/circuit_breaker_report.md",
		},
		{
			name:     "ArchiveDir constructs archive directory path",
			method:   mockPaths.ArchiveDir,
			expected: "/mock/project/.liza/archive",
		},
		{
			name:     "AgentPromptsDir constructs agent-prompts directory path",
			method:   mockPaths.AgentPromptsDir,
			expected: "/mock/project/.liza/agent-prompts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.method()
			// Expected paths are written with forward slashes; normalize the
			// result the same way so the assertions hold on Windows where
			// filepath.Join uses backslashes.
			if filepath.ToSlash(got) != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}

// TestLizaPathsGenericGet tests the private get() method indirectly through StatePath
func TestLizaPathsGenericGet(t *testing.T) {
	mockPaths := newTestLizaPaths("/mock/project")

	// Test that get() works correctly by testing public methods that use it
	tests := []struct {
		name     string
		method   func() string
		expected string
	}{
		{
			name:     "StatePath uses get() with StateFileName",
			method:   mockPaths.StatePath,
			expected: "/mock/project/.liza/state.yaml",
		},
		{
			name:     "LogPath uses get() with LogFileName",
			method:   mockPaths.LogPath,
			expected: "/mock/project/.liza/log.yaml",
		},
		{
			name:     "AlertsLogPath uses get() with AlertsLogFileName",
			method:   mockPaths.AlertsLogPath,
			expected: "/mock/project/.liza/alerts.log",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.method()
			// Expected paths are written with forward slashes; normalize the
			// result the same way so the assertions hold on Windows where
			// filepath.Join uses backslashes.
			if filepath.ToSlash(got) != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
