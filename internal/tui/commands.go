package tui

import (
	"io"
	"maps"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
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
			NewPosition: info.Size(),
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

// Init returns the initial Cmd batch that starts the data flow.
// Subscribes to watcher, reads initial state, reads initial log, starts tick timer.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		readStateCmd(m.blackboard),
		readLogCmd(m.logPath, m.logPosition),
		tickCmd(),
	}
	if m.watcher != nil {
		cmds = append(cmds, watchStateCmd(m.watcher))
	}
	return tea.Batch(cmds...)
}
