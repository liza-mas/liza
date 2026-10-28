package install

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/liza-mas/liza/internal/tui"
)

// The exported TUI API is unchanged from the starting tree, so no twin is needed.
func toggleAutoResume(projectRoot string) error {
	model, err := tui.New(projectRoot)
	if err != nil {
		return fmt.Errorf("TUI New failed")
	}
	defer model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if cmd == nil {
		return fmt.Errorf("TUI auto-resume key returned no command")
	}
	result, ok := cmd().(tui.CmdResultMsg)
	if !ok || !result.Success {
		return fmt.Errorf("TUI auto-resume command failed")
	}
	return nil
}
