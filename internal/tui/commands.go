package tui

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/process"
	"gopkg.in/yaml.v3"
)

var terminateAgent = ops.TerminateAgent

// watchStateCmd blocks on the watcher's Events channel and returns
// stateChangedMsg when the state file is modified. Returns watcherClosedMsg
// if the channel closes. Returns errMsg on watcher errors.
func watchStateCmd(watcher StateWatcher) tea.Cmd {
	return func() tea.Msg {
		select {
		case _, ok := <-watcher.Events():
			if !ok {
				return watcherClosedMsg{}
			}
			return stateChangedMsg{}
		case err, ok := <-watcher.Errors():
			if !ok {
				return watcherClosedMsg{}
			}
			return errMsg{err}
		}
	}
}

// readStateCmd reads one published state.yaml snapshot without taking the
// state lock and returns a StateMsg.
// Returns errMsg on read failure.
func readStateCmd(bb *db.Blackboard) tea.Cmd {
	return func() tea.Msg {
		state, err := bb.ReadSnapshot()
		if err != nil {
			return errMsg{err}
		}
		return StateMsg{State: state}
	}
}

// readLogCmd reads new entries from log.yaml starting at the given byte offset.
// Returns LogEntriesMsg with parsed entries and the new byte position.
// Returns empty LogEntriesMsg if no new data or file doesn't exist.
func readLogCmd(logPath string, offset int64) tea.Cmd {
	return func() tea.Msg {
		f, err := os.Open(logPath)
		if err != nil {
			if os.IsNotExist(err) {
				return LogEntriesMsg{}
			}
			return errMsg{err}
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			return errMsg{err}
		}

		if info.Size() <= offset {
			return LogEntriesMsg{}
		}

		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return errMsg{err}
		}
		data, err := io.ReadAll(f)
		if err != nil {
			return errMsg{err}
		}

		if len(data) == 0 {
			return LogEntriesMsg{}
		}

		var entries []log.Entry
		if err := yaml.Unmarshal(data, &entries); err != nil {
			return errMsg{fmt.Errorf("log.yaml parse: %w", err)}
		}

		return LogEntriesMsg{
			Entries:     entries,
			NewPosition: offset + int64(len(data)),
		}
	}
}

// runChecksCmd runs all anomaly checks against the provided state snapshot.
// Copies the state cache before entering the goroutine to avoid data races.
// Writes alerts to alerts.log and returns alertsMsg with results and updated cache.
func runChecksCmd(projectRoot, alertsLogPath string, state *models.State, cache map[string]time.Time) tea.Cmd {
	// Copy cache before closure to avoid data race with the model's map
	cacheCopy := make(map[string]time.Time, len(cache))
	maps.Copy(cacheCopy, cache)

	return func() tea.Msg {
		if state == nil {
			return alertsMsg{StateCache: cacheCopy}
		}

		config := commands.WatchConfig{
			ProjectRoot: projectRoot,
			AlertsLog:   alertsLogPath,
			WarnWriter:  io.Discard,
			StateCache:  cacheCopy,
		}

		repairOutcome := commands.RunAutoRepairAgentPool(context.Background(), state, config)
		config.RecentlySpawnedAgentPIDs = recentlySpawnedAgentPIDs(repairOutcome.Spawned)
		snapshot := commands.RunChecksWithStateSnapshot(state, config)
		snapshot.Alerts = commands.FilterAlertsAfterAutoRepair(snapshot.Alerts, repairOutcome)
		snapshot.Alerts = append(snapshot.Alerts, repairOutcome.Alerts...)

		// Write each alert to alerts.log
		var writeErr error
		for _, a := range snapshot.Alerts {
			if err := alerts.Write(alertsLogPath, a); err != nil && writeErr == nil {
				writeErr = err
			}
		}

		// Convert to TUI AlertMsg types
		alertMsgs := make([]AlertMsg, len(snapshot.Alerts))
		for i, a := range snapshot.Alerts {
			key := alerts.Key(a)
			if !snapshot.ActiveKeys[key] {
				key = ""
			}
			alertMsgs[i] = AlertMsg{
				Timestamp: a.Timestamp,
				Level:     string(a.Level),
				Category:  a.Category,
				Message:   a.Message,
				Key:       key,
			}
		}

		return alertsMsg{
			Alerts:          alertMsgs,
			ActiveAlertKeys: snapshot.ActiveKeys,
			StateCache:      cacheCopy,
			WriteErr:        writeErr,
		}
	}
}

func recentlySpawnedAgentPIDs(spawned []commands.SpawnedAgent) []int {
	pids := make([]int, 0, len(spawned))
	for _, agent := range spawned {
		if agent.PID > 0 {
			pids = append(pids, agent.PID)
		}
	}
	return pids
}

// tickCmd returns a tea.Cmd that fires a TickMsg after 10 seconds.
func tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// spawnAgentCmd spawns a new agent process for the given role and CLI backend.
// Delegates to process.SpawnAgent for detached subprocess lifecycle.
// Returns CmdResultMsg with success/error status.
func spawnAgentCmd(projectRoot, role, cli string) tea.Cmd {
	return func() tea.Msg {
		if _, err := process.SpawnAgent(projectRoot, role, cli); err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("spawn %s: %v", role, err)}
		}
		return CmdResultMsg{Success: true, Message: "Spawned " + role + " (" + cli + ")"}
	}
}

// pauseSystemCmd pauses the system with an optional reason.
// Calls ops.Pause() directly (same process, no subprocess overhead).
// Returns CmdResultMsg with result.
func pauseSystemCmd(projectRoot, reason string) tea.Cmd {
	return func() tea.Msg {
		_, err := ops.Pause(projectRoot, reason, "operator")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("pause: %v", err)}
		}
		return CmdResultMsg{Success: true, Message: "System paused"}
	}
}

// resumeSystemCmd resumes the system.
// Calls ops.Resume() directly, then clears any provider-scoped stop signals
// so restarted agents aren't immediately blocked.
// Returns CmdResultMsg with result.
func resumeSystemCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		result, err := ops.Resume(projectRoot, "operator")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("resume: %v", err)}
		}

		// Clear provider-scoped stop signals (mirrors CLI resume behavior).
		var clearErrors []string
		if matches, err := filepath.Glob(agent.QuotaSignalGlob(projectRoot)); err == nil {
			for _, m := range matches {
				provider := agent.ProviderFromSignalFile(m)
				if clearErr := agent.ClearQuotaSignal(projectRoot, provider); clearErr != nil {
					clearErrors = append(clearErrors, fmt.Sprintf("quota/%s: %v", provider, clearErr))
				}
			}
		}
		if matches, err := filepath.Glob(agent.ProviderUnavailableSignalGlob(projectRoot)); err == nil {
			for _, m := range matches {
				provider := agent.ProviderFromUnavailableSignalFile(m)
				if clearErr := agent.ClearProviderUnavailableSignal(projectRoot, provider); clearErr != nil {
					clearErrors = append(clearErrors, fmt.Sprintf("unavailable/%s: %v", provider, clearErr))
				}
			}
		}

		msg := "System resumed"
		if result.SystemRemainsStopped {
			msg = fmt.Sprintf("HALT response acknowledged; system remains STOPPED; run %q before restarting agents", brand.Command("start"))
		}
		if len(clearErrors) > 0 {
			msg += fmt.Sprintf(" (warning: failed to clear provider signals: %s)", strings.Join(clearErrors, "; "))
		}
		return CmdResultMsg{Success: true, Message: msg}
	}
}

// checkpointCmd creates a sprint checkpoint.
// Calls ops.SprintCheckpoint() directly.
// Returns CmdResultMsg with result.
func checkpointCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		_, err := ops.SprintCheckpoint(projectRoot, "")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("checkpoint: %v", err)}
		}
		return CmdResultMsg{Success: true, Message: "Checkpoint created"}
	}
}

// toggleAutoResumeCmd toggles the auto_resume config flag in state.yaml.
// Returns CmdResultMsg with the new value displayed for 3s.
func toggleAutoResumeCmd(bb *db.Blackboard) tea.Cmd {
	return func() tea.Msg {
		var newVal bool
		err := bb.Modify(func(s *models.State) error {
			s.Config.AutoResume = !s.Config.AutoResume
			newVal = s.Config.AutoResume
			return nil
		})
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("toggle auto-resume: %v", err)}
		}
		label := "OFF"
		if newVal {
			label = "ON"
		}
		return CmdResultMsg{Success: true, Message: "Auto-resume: " + label}
	}
}

// stopSystemCmd stops the system, then signals the TUI to quit.
// Calls ops.Stop() directly.
// Returns stopDoneMsg on success (triggers tea.Quit in Update).
// Returns CmdResultMsg with error on failure.
func stopSystemCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		_, err := ops.Stop(projectRoot, "TUI stop", "operator")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("stop: %v", err)}
		}
		return stopDoneMsg{}
	}
}

// terminateAgentCmd stops an agent process before removing its state entry.
// Uses force=true and allowRunningPID=true since the TUI is an interactive context.
// Returns CmdResultMsg with result.
func terminateAgentCmd(projectRoot, agentID string) tea.Cmd {
	return func() tea.Msg {
		_, err := terminateAgent(projectRoot, agentID, true, true, "terminated via TUI", 5*time.Second)
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("terminate %s: %v", agentID, err)}
		}
		return CmdResultMsg{Success: true, Message: "Terminated " + agentID}
	}
}

// loadRolesCmd loads role metadata from pipeline config.
// Returns rolesMsg with sorted names.
// Returns rolesMsg with nil fields if config not found (non-fatal).
func loadRolesCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		resolver, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			return rolesMsg{}
		}
		roles := resolver.AllRoleNames()

		pr, ok := resolver.(*pipeline.Resolver)
		if !ok {
			return rolesMsg{}
		}
		roleTypes := make(map[string]string)
		for _, role := range roles {
			roleType, err := resolver.RoleType(role)
			if err != nil {
				return rolesMsg{}
			}
			roleTypes[role] = roleType
		}
		return rolesMsg{
			Roles:           roles,
			RoleTypes:       roleTypes,
			SprintTerminals: pr.SprintTerminalStates(),
			StateCategories: pr.StateCategories(),
		}
	}
}

// Init returns the initial Cmd batch that starts the data flow.
// Subscribes to watcher, reads initial state, reads initial log, starts tick timer.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		readStateCmd(m.blackboard),
		readLogCmd(m.logPath, m.logPosition),
		loadRolesCmd(m.projectRoot),
		tickCmd(),
	}
	if m.watcher != nil {
		cmds = append(cmds, watchStateCmd(m.watcher))
	}
	return tea.Batch(cmds...)
}
