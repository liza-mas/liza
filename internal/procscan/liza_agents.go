package procscan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// ErrProcessScanUnavailable reports that the host does not expose a procfs tree
// usable by the scanner. Callers may treat this as a warning for live validation.
var ErrProcessScanUnavailable = errors.New("process scan unavailable: procfs not found")

const ScopeReasonCWDUnreadable = "cwd_unreadable"

// AgentProcess describes a confirmed live agent supervisor process.
type AgentProcess struct {
	PID     int      `json:"pid" yaml:"pid"`
	Role    string   `json:"role,omitempty" yaml:"role,omitempty"`
	CLI     string   `json:"cli,omitempty" yaml:"cli,omitempty"`
	GoalID  string   `json:"goal_id,omitempty" yaml:"goal_id,omitempty"`
	CWD     string   `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	Cmdline []string `json:"cmdline" yaml:"cmdline"`
	Reason  string   `json:"reason" yaml:"reason"`
}

// ZombieProcess is a process verified to belong to the requested scope but
// missing from state.yaml.
type ZombieProcess = AgentProcess

// ZombieScanResult separates verified zombies from processes whose project
// scope could not be established.
type ZombieScanResult struct {
	Zombies      []ZombieProcess
	UnknownScope []AgentProcess
}

// ZombieScanOptions controls live-process zombie detection.
type ZombieScanOptions struct {
	ProjectRoot    string
	GoalID         string
	RegisteredPIDs map[int]bool
	ProcRoot       string
}

// FindZombieAgents enumerates /proc and returns live agent supervisors for
// the current project/goal that are missing from the registered PID set.
// With no project root, the scan is an exact goal filter rather than a
// scan-all mode; candidates without matching goal metadata are omitted.
func FindZombieAgents(opts ZombieScanOptions) (ZombieScanResult, error) {
	procRoot := opts.ProcRoot
	if procRoot == "" {
		procRoot = "/proc"
	}

	entries, err := os.ReadDir(procRoot)
	if os.IsNotExist(err) {
		return ZombieScanResult{}, ErrProcessScanUnavailable
	}
	if err != nil {
		return ZombieScanResult{}, fmt.Errorf("scan procfs: %w", err)
	}

	projectRoot := canonicalPath(opts.ProjectRoot)
	var result ZombieScanResult
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if opts.RegisteredPIDs[pid] {
			continue
		}

		procDir := filepath.Join(procRoot, entry.Name())
		argv, err := readCmdline(filepath.Join(procDir, "cmdline"))
		if err != nil || !IsLizaAgentArgv(argv) {
			continue
		}

		zombie := ZombieProcess{
			PID:     pid,
			Role:    roleFromArgv(argv),
			CLI:     flagValue(argv, "--cli"),
			GoalID:  flagValue(argv, "--goal-id"),
			Cmdline: argv,
			Reason:  "not_registered_in_state",
		}
		cwd, err := os.Readlink(filepath.Join(procDir, "cwd"))
		if projectRoot != "" && os.IsNotExist(err) {
			continue
		}
		if err == nil {
			zombie.CWD = cwd
		}

		switch classifyScope(zombie, projectRoot, opts.GoalID) {
		case scopeCurrent:
			result.Zombies = append(result.Zombies, zombie)
		case scopeUnknown:
			zombie.Reason = ScopeReasonCWDUnreadable
			result.UnknownScope = append(result.UnknownScope, zombie)
		}
	}

	return result, nil
}

// IsLizaAgentArgv reports whether argv identifies an agent supervisor.
//
// The executable suffix is dropped before comparing: Go appends .exe for
// GOOS=windows, so the image an agent runs under is liza.exe there while the
// configured binary name has no suffix. Trimming it unconditionally keeps one
// comparison for both platforms, and a bare "liza" is what a Unix argv[0]
// already is.
func IsLizaAgentArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	bin := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	return (bin == brand.BinaryName || bin == "liza") && argv[1] == "agent"
}

// MatchesLizaAgentIdentity reports whether argv identifies the expected
// agent supervisor. Empty role or agentID inputs are treated as wildcards.
// Omitted --agent-id is valid because the agent command auto-assigns an ID when the
// flag is not provided; explicit --agent-id values must still match.
func MatchesLizaAgentIdentity(argv []string, role, agentID string) bool {
	if !IsLizaAgentArgv(argv) {
		return false
	}
	if role != "" && roleFromArgv(argv) != role {
		return false
	}
	if agentID == "" {
		return true
	}
	argvAgentID := flagValue(argv, "--agent-id")
	if argvAgentID != "" && argvAgentID != agentID {
		return false
	}
	return true
}

func readCmdline(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCmdlineBytes(data), nil
}

// ParseCmdlineBytes parses Linux procfs' null-separated cmdline format.
func ParseCmdlineBytes(data []byte) []string {
	raw := strings.TrimRight(string(data), "\x00")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\x00")
}

func roleFromArgv(argv []string) string {
	if len(argv) < 3 {
		return ""
	}
	return argv[2]
}

func flagValue(argv []string, name string) string {
	for i, arg := range argv {
		if arg == name && i+1 < len(argv) {
			return argv[i+1]
		}
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
	}
	return ""
}

type scopeClassification uint8

const (
	scopeForeign scopeClassification = iota
	scopeCurrent
	scopeUnknown
)

func classifyScope(process AgentProcess, projectRoot, goalID string) scopeClassification {
	if projectRoot != "" {
		if process.CWD == "" {
			return scopeUnknown
		}
		if canonicalPath(process.CWD) == projectRoot {
			return scopeCurrent
		}
		return scopeForeign
	}
	if goalID != "" && process.GoalID == goalID {
		return scopeCurrent
	}
	return scopeForeign
}

func canonicalPath(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Clean(path)
}
