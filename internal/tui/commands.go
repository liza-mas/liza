package tui

import (
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"gopkg.in/yaml.v3"
)

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

// readStateCmd reads state.yaml via Blackboard.Read() and returns a StateMsg.
// Returns errMsg on read failure.
func readStateCmd(bb *db.Blackboard) tea.Cmd {
	return func() tea.Msg {
		state, err := bb.Read()
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
			// Partial/corrupt YAML — don't advance position, retry next tick
			return LogEntriesMsg{}
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
			StateCache:  cacheCopy,
		}

		alerts := commands.RunChecksWithState(state, config)

		// Write each alert to alerts.log
		for _, a := range alerts {
			_ = commands.WriteAlert(alertsLogPath, a)
		}

		// Convert to TUI AlertMsg types
		alertMsgs := make([]AlertMsg, len(alerts))
		for i, a := range alerts {
			alertMsgs[i] = AlertMsg{
				Timestamp: a.Timestamp,
				Level:     string(a.Level),
				Category:  a.Category,
				Message:   a.Message,
			}
		}

		return alertsMsg{
			Alerts:     alertMsgs,
			StateCache: cacheCopy,
		}
	}
}

// tickCmd returns a tea.Cmd that fires a TickMsg after 10 seconds.
func tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// spawnAgentCmd spawns a new agent process for the given role.
// Uses exec.Command("liza", "agent", role).Start() per spec §Keybindings.
// The child process is detached with stdout/stderr redirected to os.DevNull.
// Returns CmdResultMsg with success/error status.
func spawnAgentCmd(projectRoot, role string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("liza", "agent", role)
		cmd.Dir = projectRoot
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("spawn %s: %v", role, err)}
		}
		cmd.Stdout = devNull
		cmd.Stderr = devNull

		if err := cmd.Start(); err != nil {
			devNull.Close()
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("spawn %s: %v", role, err)}
		}
		go cmd.Wait() // Reap child to prevent zombie accumulation
		devNull.Close()
		return CmdResultMsg{Success: true, Message: "Spawned " + role}
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
// Calls ops.Resume() directly.
// Returns CmdResultMsg with result.
func resumeSystemCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		_, err := ops.Resume(projectRoot, "operator")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("resume: %v", err)}
		}
		return CmdResultMsg{Success: true, Message: "System resumed"}
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

// addTaskCmd adds a new task from form input.
// Calls ops.AddTask() directly.
// Returns CmdResultMsg with result.
//
//lint:ignore U1000 used by update.go
func addTaskCmd(projectRoot string, input *commands.TaskInput) tea.Cmd {
	return func() tea.Msg {
		p := paths.New(projectRoot)
		opsInput := &ops.AddTaskInput{
			ID:          input.ID,
			Type:        input.Type,
			RolePair:    input.RolePair,
			Description: input.Description,
			SpecRef:     input.SpecRef,
			DoneWhen:    input.DoneWhen,
			Scope:       input.Scope,
			Priority:    input.Priority,
			DependsOn:   input.DependsOn,
		}
		_, err := ops.AddTask(p.StatePath(), p.LogPath(), opsInput, "operator")
		if err != nil {
			return CmdResultMsg{Success: false, Message: fmt.Sprintf("add task: %v", err)}
		}
		return CmdResultMsg{Success: true, Message: "Task " + input.ID + " added"}
	}
}

// loadRolesCmd loads role and role-pair names from pipeline config.
// Returns rolesMsg with sorted names.
// Returns rolesMsg with nil fields if config not found (non-fatal).
func loadRolesCmd(projectRoot string) tea.Cmd {
	return func() tea.Msg {
		resolver, err := ops.LoadResolverForModels(projectRoot)
		if err != nil {
			return rolesMsg{}
		}
		pr, ok := resolver.(*pipeline.Resolver)
		if !ok {
			return rolesMsg{}
		}
		return rolesMsg{
			Roles:     pr.AllRoleNames(),
			RolePairs: pr.RolePairNames(),
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
