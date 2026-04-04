package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/gitenv"
)

// Standard path components used throughout Liza
const (
	LizaDirName   = ".liza"      // Name of the Liza directory
	StateFileName = "state.yaml" // Name of the state file
	LogFileName   = "log.yaml"   // Name of the log file
	LockSuffix    = ".lock"      // Suffix for lock files

	// TaskBranchPrefix is the prefix used for task-specific git branches.
	// Task branches are named: task/<taskID>
	TaskBranchPrefix = "task/"

	// Logs and reports
	AlertsLogFileName            = "alerts.log"                // Alerts log file
	SprintSummaryFileName        = "sprint_summary.md"         // Sprint summary report
	CircuitBreakerReportFileName = "circuit_breaker_report.md" // Circuit breaker report

	// Directories
	AgentPromptsDirName = "agent-prompts" // Directory for agent prompt files
	AgentOutputsDirName = "agent-outputs" // Directory for agent output files
	ArchiveDirName      = "archive"       // Directory for archived files
	ContractsDirName    = "contracts"     // Directory for contract files
	SkillsDirName       = "skills"        // Directory for skill files
	SpecsDirName        = "specs"         // Directory for specification files

	// Claude-specific
	ClaudeDirName      = ".claude"       // Claude directory name (in project root)
	ClaudeSettingsFile = "settings.json" // Claude settings file name

	// Git directories
	GitDirName       = ".git"       // Git directory name
	WorktreesDirName = ".worktrees" // Worktrees directory name
)

// LizaPaths provides path construction methods for the Liza project.
// Create instances using SetupLizaPaths().
type LizaPaths struct {
	projectRoot string // private: main project root (handles worktrees correctly)
}

// get is a private helper that constructs a path within the .liza directory.
func (p LizaPaths) get(name string) string {
	return filepath.Join(p.projectRoot, LizaDirName, name)
}

// ProjectRoot returns the project root directory.
// Use this when you need the project root itself (e.g., for worktree paths).
func (p LizaPaths) ProjectRoot() string {
	return p.projectRoot
}

// LizaDir returns the path to the .liza directory.
func (p LizaPaths) LizaDir() string {
	return filepath.Join(p.projectRoot, LizaDirName)
}

// Core file methods

// StatePath returns the path to the state file.
func (p LizaPaths) StatePath() string {
	return p.get(StateFileName)
}

// LogPath returns the path to the log file.
func (p LizaPaths) LogPath() string {
	return p.get(LogFileName)
}

// LockPath returns the path to the lock file.
func (p LizaPaths) LockPath() string {
	return p.get(StateFileName) + LockSuffix
}

// Log and report file methods

// AlertsLogPath returns the path to the alerts log file.
func (p LizaPaths) AlertsLogPath() string {
	return p.get(AlertsLogFileName)
}

// SprintSummaryPath returns the path to the sprint summary report file.
func (p LizaPaths) SprintSummaryPath() string {
	return p.get(SprintSummaryFileName)
}

// CircuitBreakerReportPath returns the path to the circuit breaker report file.
func (p LizaPaths) CircuitBreakerReportPath() string {
	return p.get(CircuitBreakerReportFileName)
}

// Directory methods

// ArchiveDir returns the path to the archive directory.
func (p LizaPaths) ArchiveDir() string {
	return p.get(ArchiveDirName)
}

// SprintArchivePath returns the path for archiving a specific sprint.
// Format: .liza/archive/sprint-N.yaml
// Takes the numeric sprint number to avoid path traversal via tampered IDs.
func (p LizaPaths) SprintArchivePath(sprintNumber int) string {
	return filepath.Join(p.ArchiveDir(), fmt.Sprintf("sprint-%d.yaml", sprintNumber))
}

// AgentPromptsDir returns the path to the agent prompts directory.
func (p LizaPaths) AgentPromptsDir() string {
	return p.get(AgentPromptsDirName)
}

// AgentOutputsDir returns the path to the agent outputs directory.
func (p LizaPaths) AgentOutputsDir() string {
	return p.get(AgentOutputsDirName)
}

// ContractsDir returns the path to the contracts directory.
func (p LizaPaths) ContractsDir() string {
	return p.get(ContractsDirName)
}

// SkillsDir returns the path to the skills directory.
func (p LizaPaths) SkillsDir() string {
	return p.get(SkillsDirName)
}

// SpecsDir returns the path to the specs directory.
func (p LizaPaths) SpecsDir() string {
	return p.get(SpecsDirName)
}

// ClaudeDir returns the path to the .claude directory (in project root, not .liza).
func (p LizaPaths) ClaudeDir() string {
	return filepath.Join(p.projectRoot, ClaudeDirName)
}

// ClaudeSettingsPath returns the path to the Claude settings.json file.
func (p LizaPaths) ClaudeSettingsPath() string {
	return filepath.Join(p.ClaudeDir(), ClaudeSettingsFile)
}

// GlobalLizaDir returns the path to the global ~/.liza directory.
func GlobalLizaDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, LizaDirName), nil
}

// GetProjectRoot returns the main project root directory.
// It correctly handles both regular git repositories and git worktrees.
//
// In a regular repo: returns the git toplevel directory
// In a worktree: returns the main repo directory (parent of .git common dir)
func GetProjectRoot() (string, error) {
	// Get the toplevel directory
	toplevelCmd := gitenv.Command("rev-parse", "--show-toplevel")
	toplevelOut, err := toplevelCmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository or git command failed: %w", err)
	}
	toplevel := strings.TrimSpace(string(toplevelOut))

	// Get the common git directory
	commonDirCmd := gitenv.Command("rev-parse", "--git-common-dir")
	commonDirOut, err := commonDirCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git common dir: %w", err)
	}
	gitCommonDir := strings.TrimSpace(string(commonDirOut))

	// Resolve to absolute path
	absCommonDir, err := filepath.Abs(gitCommonDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve common dir: %w", err)
	}
	absCommonDir, err = filepath.EvalSymlinks(absCommonDir)
	if err != nil {
		return "", fmt.Errorf("failed to eval symlinks: %w", err)
	}

	// Check if we're in a worktree
	// Main repo: git-common-dir == toplevel/.git
	// Worktree: git-common-dir points to main repo's .git directory
	expectedGitDir := filepath.Join(toplevel, GitDirName)
	if absCommonDir != expectedGitDir {
		// We're in a worktree - common dir is <main>/.git
		// Return the parent directory (main repo root)
		return filepath.Dir(absCommonDir), nil
	}

	// Regular repo - return toplevel
	return toplevel, nil
}

// New creates a LizaPaths instance from a known project root.
// Use this when you already have the project root path.
func New(projectRoot string) LizaPaths {
	return LizaPaths{projectRoot: projectRoot}
}

// LizaPathsFromGit initializes and returns a LizaPaths instance.
// It automatically detects the project root using git.
// Use this when you need to auto-detect the project root.
func LizaPathsFromGit() (LizaPaths, error) {
	projectRoot, err := GetProjectRoot()
	if err != nil {
		return LizaPaths{}, fmt.Errorf("failed to get project root: %w", err)
	}

	return New(projectRoot), nil
}

// ISOTimestamp returns the current time in ISO8601 format (UTC).
// Format: YYYY-MM-DDTHH:MM:SSZ
func ISOTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// ISOTimestampOffset returns a timestamp offset by the given duration in ISO8601 format (UTC).
// Positive duration returns future time, negative returns past time.
// Format: YYYY-MM-DDTHH:MM:SSZ
func ISOTimestampOffset(offset time.Duration) string {
	return time.Now().UTC().Add(offset).Format(time.RFC3339)
}

// ValidatePath checks if a path is valid (non-empty and absolute).
func ValidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %s", path)
	}
	return nil
}

// safeTaskIDRe enforces a strict charset for task IDs: alphanumeric, hyphens,
// underscores, and dots (no leading dot). This ensures safety for file paths,
// branch names, and interpolation into prompt text (including JSON fragments).
var safeTaskIDRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// ValidateTaskID checks that a task ID is safe for use in file paths, branch names,
// and prompt interpolation. Rejects path traversal, empty IDs, and any character
// outside [a-zA-Z0-9._-].
func ValidateTaskID(taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	// Strict charset check — must start with alphanumeric, then [a-zA-Z0-9._-]
	if !safeTaskIDRe.MatchString(taskID) {
		return fmt.Errorf("task ID contains invalid characters (allowed: a-z A-Z 0-9 . _ -): %q", taskID)
	}

	// Reject path traversal (.. sequences)
	if strings.Contains(taskID, "..") {
		return fmt.Errorf("task ID contains path traversal: %q", taskID)
	}

	return nil
}
