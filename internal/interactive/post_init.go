package interactive

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/brand"
)

// PrintPostInitSummary prints a styled "What's Next" banner after initialization.
func PrintPostInitSummary(mode string, agents []string) {
	var content string

	if mode == "pairing" {
		agentList := strings.Join(agents, ", ")
		content = fmt.Sprintf(`%s pairing mode enabled (%s)

Next steps:
  1. Open your AI agent (Claude, Codex, etc.)
  2. The %s contract is now active
     Your agent follows %s quality standards
  3. Read the pairing guide:
       ~/%s/support-docs/USAGE_PAIRING.md
  4. To try the full multi-agent system later:
       %s init "Your project goal" --spec specs/vision.md`, brand.NameTitle, agentList, brand.NameTitle, brand.NameTitle, brand.GlobalDirName, brand.BinaryName)
	} else {
		content = fmt.Sprintf(`%s workspace initialized

Next steps:
  1. Read the guide:    ~/%s/support-docs/USAGE_MULTI_AGENTS.md
  2. Run the TUI:       %s tui
  3. Spawn agents:      press [s] to spawn an orchestrator and workers`, brand.NameTitle, brand.GlobalDirName, brand.BinaryName)
	}

	style := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("2")). // green
		Padding(1, 2)

	fmt.Println()
	fmt.Println(style.Render(content))
	fmt.Println()
}
