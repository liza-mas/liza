package procscan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// enumerateCandidatePIDs lists the processes worth asking for a command line
// on a host with no procfs. It is a variable so a test can stand in for the
// host's process table.
var enumerateCandidatePIDs = enumerateAgentImagePIDs

// ZombieScanOptions controls live-process zombie detection.
type ZombieScanOptions struct {
	ProjectRoot    string
	GoalID         string
	RegisteredPIDs map[int]bool
	ProcRoot       string
}

// FindZombieAgents enumerates the host's processes and returns live agent
// supervisors for the current project/goal that are missing from the registered
// PID set. With no project root, the scan is an exact goal filter rather than a
// scan-all mode; candidates without matching goal metadata are omitted.
//
// Procfs is the source wherever there is one. A host without it falls back to
// naming its own processes, and only the real proc root does so: an injected
// one means the caller is describing a host, and the machine underneath is
// not it.
func FindZombieAgents(opts ZombieScanOptions) (ZombieScanResult, error) {
	procRoot := opts.ProcRoot
	if procRoot == "" {
		procRoot = defaultProcRoot
	}

	entries, err := os.ReadDir(procRoot)
	if os.IsNotExist(err) {
		if procRoot != defaultProcRoot {
			return ZombieScanResult{}, ErrProcessScanUnavailable
		}
		return findZombieAgentsNatively(opts)
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

		zombie := newZombieProcess(pid, argv)
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

// findZombieAgentsNatively scans the process table of a host that exposes no
// procfs.
//
// The cwd that decides scope elsewhere is not merely unreadable here, it is
// absent by construction: Windows does not report another process's working
// directory without the access rights the command-line probe deliberately
// avoids needing. Classifying every candidate as unknown scope on that basis
// would report the whole agent pool as unresolved on every scan, which is the
// noise this path exists to remove.
//
// The goal ID each candidate carries in its own argv stands in, and keeps the
// three outcomes meaningful: a match is the current run, a different goal
// belongs to another one, and a candidate with no goal recorded is what nothing
// can be said about. The procfs path is untouched — its cwd evidence is
// stronger, and weakening it to match would trade accuracy everywhere else for
// reach here.
func findZombieAgentsNatively(opts ZombieScanOptions) (ZombieScanResult, error) {
	if opts.GoalID == "" {
		return ZombieScanResult{}, nil
	}
	pids, err := enumerateCandidatePIDs()
	if err != nil {
		return ZombieScanResult{}, err
	}

	var result ZombieScanResult
	for _, pid := range pids {
		if opts.RegisteredPIDs[pid] {
			continue
		}
		argv, err := nativeCommandLine(pid)
		if err != nil || !IsLizaAgentArgv(argv) {
			continue
		}
		candidate := newZombieProcess(pid, argv)
		switch {
		case candidate.GoalID == opts.GoalID:
			result.Zombies = append(result.Zombies, candidate)
		case candidate.GoalID == "":
			candidate.Reason = ScopeReasonCWDUnreadable
			result.UnknownScope = append(result.UnknownScope, candidate)
		}
	}
	return result, nil
}

func newZombieProcess(pid int, argv []string) ZombieProcess {
	return ZombieProcess{
		PID:     pid,
		Role:    roleFromArgv(argv),
		CLI:     flagValue(argv, "--cli"),
		GoalID:  flagValue(argv, "--goal-id"),
		Cmdline: argv,
		Reason:  "not_registered_in_state",
	}
}

// IsLizaAgentArgv reports whether argv identifies an agent supervisor.
//
// The executable suffix is dropped before comparing: Go appends .exe for
// GOOS=windows, so the image name gains an .exe suffix while the configured
// binary name has none. Trimming it keeps one comparison for both platforms.
func IsLizaAgentArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	return isAgentImageName(filepath.Base(argv[0])) && argv[1] == "agent"
}

// isAgentImageName reports whether an image name is the one agents run under.
//
// It says nothing on its own about a process being an agent — argv decides
// that. It exists so the process-table pre-filter and the identity check agree
// on what the image is called.
func isAgentImageName(base string) bool {
	bin := trimExecutableSuffix(base)
	return bin == brand.RuntimeValues().BinaryName
}

// trimExecutableSuffix drops a trailing .exe from an image name.
//
// The suffix case is only insignificant on Windows, where PATHEXT resolution
// decides it: a shell can report the executable with an uppercase suffix, which
// case-sensitive trim would leave intact and no comparison would then match.
// On POSIX, names ending in .EXE and .exe are distinct, so the case stands.
func trimExecutableSuffix(base string) string {
	const suffix = ".exe"
	if runtime.GOOS != "windows" {
		return strings.TrimSuffix(base, suffix)
	}
	if len(base) > len(suffix) && strings.EqualFold(base[len(base)-len(suffix):], suffix) {
		return base[:len(base)-len(suffix)]
	}
	return base
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
