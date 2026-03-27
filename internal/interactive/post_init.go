package interactive

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PrintPostInitSummary prints a styled "What's Next" banner after initialization.
func PrintPostInitSummary(mode string, agents []string) {
	var content string

	if mode == "pairing" {
		agentList := strings.Join(agents, ", ")
		content = fmt.Sprintf(`Liza pairing mode enabled (%s)

Next steps:
  1. Open your AI agent (Claude, Codex, etc.)
  2. The Liza contract is now active
     Your agent follows Liza quality standards
  3. To try the full multi-agent system later:
       liza init "Your project goal" --spec specs/vision.md`, agentList)
	} else {
		content = `Liza workspace initialized

Next steps:
  1. Run the TUI:       liza tui
  2. Spawn agents:      press [s] to spawn an orchestrator and workers`
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("2")). // green
		Padding(1, 2)

	fmt.Println()
	fmt.Println(style.Render(content))
	fmt.Println()
}
