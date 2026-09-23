// Package process provides shared subprocess management for agent spawning.
// Used by both the TUI and the HTTP API server.
package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/subprocess"
)

var agentSpawnGuard = struct {
	sync.Mutex
	inFlight map[string]bool
}{inFlight: make(map[string]bool)}

const (
	supervisorBootstrapTimeout      = 5 * time.Second
	supervisorBootstrapPollInterval = 10 * time.Millisecond
)

func buildSpawnCommand(projectRoot, role, cli string, extraArgs ...string) (*exec.Cmd, *os.File, string, error) {
	args := []string{"agent", role, "--cli", cli}
	if goalID := readGoalID(projectRoot); goalID != "" && !hasFlag(extraArgs, "--goal-id") {
		args = append(args, "--goal-id", goalID)
	}
	args = append(args, extraArgs...)

	outputsDir := paths.New(projectRoot).AgentOutputsDir()
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		return nil, nil, "", fmt.Errorf("create supervisor output directory: %w", err)
	}
	timestamp := time.Now().UTC().Format("20060102-150405.000000000")
	filenameRole := strings.NewReplacer("/", "_", "\\", "_").Replace(role)
	stdoutPath := filepath.Join(outputsDir, fmt.Sprintf("supervisor-%s-%s.stdout.log", filenameRole, timestamp))
	stderrPath := filepath.Join(outputsDir, fmt.Sprintf("supervisor-%s-%s.stderr.log", filenameRole, timestamp))
	readyFile, err := os.CreateTemp(outputsDir, ".supervisor-"+filenameRole+"-*.ready")
	if err != nil {
		return nil, nil, "", fmt.Errorf("create supervisor readiness file: %w", err)
	}
	readyPath := readyFile.Name()
	if err := readyFile.Close(); err != nil {
		os.Remove(readyPath)
		return nil, nil, "", fmt.Errorf("close supervisor readiness file: %w", err)
	}
	if err := os.Remove(readyPath); err != nil {
		return nil, nil, "", fmt.Errorf("release supervisor readiness path: %w", err)
	}
	args = append(args,
		"--"+agent.SupervisorStdoutLogFlag, stdoutPath,
		"--"+agent.SupervisorStderrLogFlag, stderrPath,
		"--"+agent.SupervisorReadyFileFlag, readyPath,
	)

	cmd := exec.Command(brand.BinaryName, args...)
	cmd.Dir = projectRoot
	subprocess.SetDetachedProcessGroup(cmd)

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		os.Remove(readyPath)
		return nil, nil, "", fmt.Errorf("open devnull: %w", err)
	}
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	return cmd, devNull, readyPath, nil
}

func readGoalID(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return ""
	}
	return state.Goal.ID
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

// SpawnAgent starts a detached agent subprocess with parent-owned stdio bound to
// /dev/null. It returns only after the child confirms that its masked supervisor
// logs are open. The readiness file is then removed, leaving no lifetime channel
// between the child and the spawning process. A background goroutine reaps the
// child to prevent zombie accumulation while the parent remains alive.
//
// Returns the started command and an error. The caller owns lifecycle
// management (the process is already started and will be reaped).
func SpawnAgent(projectRoot, role, cli string, extraArgs ...string) (*exec.Cmd, error) {
	guardKey := projectRoot + "\x00" + role
	agentSpawnGuard.Lock()
	if agentSpawnGuard.inFlight[guardKey] {
		agentSpawnGuard.Unlock()
		return nil, fmt.Errorf("spawn already in progress for role %s", role)
	}
	agentSpawnGuard.inFlight[guardKey] = true
	agentSpawnGuard.Unlock()
	defer func() {
		agentSpawnGuard.Lock()
		delete(agentSpawnGuard.inFlight, guardKey)
		agentSpawnGuard.Unlock()
	}()

	if agent.CheckQuotaSignal(projectRoot, cli) {
		err := fmt.Errorf("provider quota exhausted for %s; refusing to spawn %s", cli, role)
		if alertErr := agent.LogQuotaSpawnBlockedAlert(projectRoot, cli, role); alertErr != nil {
			return nil, errors.Join(err, fmt.Errorf("write quota spawn-blocked alert: %w", alertErr))
		}
		return nil, err
	}
	if agent.CheckProviderUnavailableSignal(projectRoot, cli) {
		err := fmt.Errorf("provider unavailable for %s; refusing to spawn %s", cli, role)
		if alertErr := agent.LogProviderUnavailableSpawnBlockedAlert(projectRoot, cli, role); alertErr != nil {
			return nil, errors.Join(err, fmt.Errorf("write provider-unavailable spawn-blocked alert: %w", alertErr))
		}
		return nil, err
	}
	runtimeConfig := readRuntimeConfig(projectRoot)
	if err := agent.CheckCLIPrerequisitesWithConfig(cli, runtimeConfig); err != nil {
		return nil, fmt.Errorf("spawn %s with %s: %w", role, cli, err)
	}

	cmd, devNull, readyPath, err := buildSpawnCommand(projectRoot, role, cli, extraArgs...)
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, errors.Join(err, devNull.Close(), removeSupervisorReadyFile(readyPath))
	}
	waitCh := make(chan error, 1)
	go func() {
		waitCh <- errors.Join(cmd.Wait(), devNull.Close())
	}()
	if err := awaitSupervisorBootstrap(cmd, readyPath, waitCh); err != nil {
		return nil, err
	}

	return cmd, nil
}

func awaitSupervisorBootstrap(cmd *exec.Cmd, readyPath string, waitCh <-chan error) error {
	timer := time.NewTimer(supervisorBootstrapTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(supervisorBootstrapPollInterval)
	defer ticker.Stop()

	for {
		select {
		case waitErr := <-waitCh:
			status, complete, statusErr := readSupervisorBootstrapStatus(readyPath)
			removeSupervisorReadyFile(readyPath)
			if complete && strings.HasPrefix(status, agent.SupervisorBootstrapErrorPrefix) {
				message := strings.TrimSpace(strings.TrimPrefix(status, agent.SupervisorBootstrapErrorPrefix))
				return errors.Join(fmt.Errorf("supervisor bootstrap failed: %s", message), waitErr, statusErr)
			}
			return errors.Join(fmt.Errorf("supervisor exited before bootstrap completed"), waitErr, statusErr)
		case <-ticker.C:
			status, complete, err := readSupervisorBootstrapStatus(readyPath)
			if err != nil {
				stopErr := stopSpawnedCommand(cmd, waitCh)
				removeSupervisorReadyFile(readyPath)
				return errors.Join(fmt.Errorf("read supervisor bootstrap status: %w", err), stopErr)
			}
			if !complete {
				continue
			}
			removeSupervisorReadyFile(readyPath)
			if status == agent.SupervisorBootstrapReadyStatus {
				select {
				case waitErr := <-waitCh:
					return errors.Join(fmt.Errorf("supervisor exited immediately after bootstrap"), waitErr)
				default:
					return nil
				}
			}
			message := strings.TrimSpace(strings.TrimPrefix(status, agent.SupervisorBootstrapErrorPrefix))
			stopErr := stopSpawnedCommand(cmd, waitCh)
			return errors.Join(fmt.Errorf("supervisor bootstrap failed: %s", message), stopErr)
		case <-timer.C:
			stopErr := stopSpawnedCommand(cmd, waitCh)
			removeSupervisorReadyFile(readyPath)
			return errors.Join(fmt.Errorf("timed out waiting for supervisor logging readiness"), stopErr)
		}
	}
}

func removeSupervisorReadyFile(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func readSupervisorBootstrapStatus(path string) (status string, complete bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	status = string(data)
	if status == agent.SupervisorBootstrapReadyStatus {
		return status, true, nil
	}
	if strings.HasPrefix(status, agent.SupervisorBootstrapErrorPrefix) && strings.HasSuffix(status, "\n") {
		return status, true, nil
	}
	return "", false, nil
}

func stopSpawnedCommand(cmd *exec.Cmd, waitCh <-chan error) error {
	killErr := cmd.Process.Kill()
	if errors.Is(killErr, os.ErrProcessDone) {
		killErr = nil
	}
	return errors.Join(killErr, <-waitCh)
}

func readRuntimeConfig(projectRoot string) models.Config {
	if projectRoot == "" {
		return models.Config{}
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return models.Config{}
	}
	return state.Config
}
