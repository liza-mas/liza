package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
	"github.com/mattn/go-runewidth"
)

// Agent panel column widths. The identity columns widen with the terminal
// (agentIdentityColumnWidths); the current-task column takes the rest.
const (
	agentStatusColumnWidth        = 14
	agentCLIColumnWidth           = 10
	agentHealthColumnWidth        = 10
	agentTimeOnTaskColumnWidth    = 14
	agentLastHeartbeatColumnWidth = 16
	agentPIDColumnWidth           = 10
)

// agentColumn is one column of the agent panel.
type agentColumn struct {
	header string
	width  int
	value  func(id string, a models.Agent) string
	// styled marks a value carrying ANSI styling: it is padded by the visual
	// width of the agent's status text instead of being truncated.
	styled bool
}

// renderAgentPanel renders the agent panel as a bordered table.
// Columns adapt to terminal width per spec §Agent Panel column priority table.
// Agents sorted by ID for stable display order.
// budget is the maximum total panel height (including border). The panel
// renders at its natural content height (no blank padding) with MaxHeight
// as ceiling.
func (m Model) renderAgentPanel(budget int) string {
	title := m.styles.PanelTitle.Render("● AGENTS")

	// Handle empty/nil state
	if m.state == nil || len(m.state.Agents) == 0 {
		content := title + "\n  No agents"
		if m.state != nil && len(m.state.AgentHealth) > 0 {
			content += "\n" + renderCurrentAgentHealthForState(m.state)
		}
		return m.styles.AgentPanel.MaxHeight(budget).Render(content)
	}

	cols := m.agentColumns()
	healthDetails := renderCurrentAgentHealthForState(m.state)

	// Reserve room for degraded-capacity details so reason/hint lines survive
	// the same MaxHeight budget used by the normal dashboard path.
	maxRows := max(budget-panelOverhead-lineCount(healthDetails), 0)
	ids := m.sortedAgentIDs()
	rows := m.renderAgentRows(cols, ids[:min(len(ids), maxRows)])

	content := title + "\n" + renderAgentHeaderRow(cols) + "\n" + strings.Join(rows, "\n")
	if healthDetails != "" {
		content += "\n" + healthDetails
	}
	return m.styles.AgentPanel.MaxHeight(budget).Render(content)
}

// sortedAgentIDs lists the agent IDs in display order.
func (m Model) sortedAgentIDs() []string {
	ids := make([]string, 0, len(m.state.Agents))
	for id := range m.state.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// agentColumns lists the columns visible at the current column tier.
func (m Model) agentColumns() []agentColumn {
	idWidth, roleWidth := m.agentIdentityColumnWidths()

	cols := []agentColumn{
		{header: "ID", width: idWidth, value: func(id string, _ models.Agent) string { return id }},
		{header: "STATUS", width: agentStatusColumnWidth, value: m.agentStatusCell, styled: true},
	}

	if m.columnTier >= ColumnTierStandard {
		cols = append(cols,
			agentColumn{header: "ROLE", width: roleWidth, value: func(_ string, a models.Agent) string { return a.Role }},
			agentColumn{header: "CLI", width: agentCLIColumnWidth, value: agentCLICell},
			agentColumn{header: "HEALTH", width: agentHealthColumnWidth, value: m.agentHealthCell},
			agentColumn{header: "CURRENT_TASK", width: m.agentCurrentTaskColumnWidth(), value: agentCurrentTaskCell},
		)
	}

	if m.columnTier >= ColumnTierWide {
		cols = append(cols,
			agentColumn{header: "TIME_ON_TASK", width: agentTimeOnTaskColumnWidth, value: m.agentTimeOnTaskCell},
			agentColumn{header: "LAST_HEARTBEAT", width: agentLastHeartbeatColumnWidth, value: agentLastHeartbeatCell},
		)
	}

	if m.columnTier >= ColumnTierFull {
		cols = append(cols, agentColumn{header: "PID", width: agentPIDColumnWidth, value: agentPIDCell})
	}
	return cols
}

func (m Model) agentStatusCell(_ string, a models.Agent) string {
	text, color := m.agentStatusText(a)
	return lipgloss.NewStyle().Foreground(color).Render(text)
}

func (m Model) agentHealthCell(id string, a models.Agent) string {
	health, ok := m.state.AgentHealth[id]
	if !ok || !health.IsCurrentDegradedFor(a) {
		return emptyCell
	}
	return string(health.State)
}

func agentCurrentTaskCell(_ string, a models.Agent) string {
	if a.CurrentTask != nil {
		return *a.CurrentTask
	}
	return emptyCell
}

func agentCLICell(_ string, a models.Agent) string {
	if a.Provider == "" {
		return emptyCell
	}
	return a.Provider
}

func agentLastHeartbeatCell(_ string, a models.Agent) string {
	if a.Heartbeat.IsZero() {
		return emptyCell
	}
	return render.FormatDuration(time.Since(a.Heartbeat)) + " ago"
}

func (m Model) agentTimeOnTaskCell(id string, a models.Agent) string {
	if a.CurrentTask == nil {
		return emptyCell
	}
	task := m.state.FindTask(*a.CurrentTask)
	if task == nil {
		return emptyCell
	}
	d := commands.AgentTimeOnTask(task, id)
	if d == 0 {
		return emptyCell
	}
	return render.FormatDuration(d)
}

func agentPIDCell(_ string, a models.Agent) string {
	if a.PID == 0 {
		return emptyCell
	}
	return fmt.Sprintf("%d", a.PID)
}

func renderAgentHeaderRow(cols []agentColumn) string {
	var headerParts []string
	for _, c := range cols {
		header := truncateVisual(c.header, c.width-1)
		headerParts = append(headerParts, padRight(header, c.width))
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8")).
		Render("  " + strings.Join(headerParts, ""))
}

// renderAgentRows renders one row per agent ID.
func (m Model) renderAgentRows(cols []agentColumn, ids []string) []string {
	var rows []string
	for _, id := range ids {
		a := m.state.Agents[id]
		var parts []string
		for _, c := range cols {
			parts = append(parts, m.renderAgentCell(c, id, a))
		}
		rows = append(rows, "  "+strings.Join(parts, ""))
	}
	return rows
}

func (m Model) renderAgentCell(c agentColumn, id string, a models.Agent) string {
	val := c.value(id, a)
	if c.styled {
		// The value is ANSI-styled; pad by visual width of raw text.
		rawText, _ := m.agentStatusText(a)
		padding := max(c.width-runewidth.StringWidth(rawText), 0)
		return val + strings.Repeat(" ", padding)
	}
	return padRight(truncateVisual(val, c.width-1), c.width)
}

// agentStatusText is the STATUS cell for an agent. A registered agent parked
// by a pause gate keeps its IDLE/WAITING status in state, which reads as "no
// work"; show the gate instead so the row says a human must act. An agent
// mid-turn stays as-is: the gate applies only between turns.
func (m Model) agentStatusText(a models.Agent) (string, lipgloss.Color) {
	if label := m.agentPauseLabel(a); label != "" {
		return "⏸ " + label, ColorHandoff
	}
	return StatusDot(string(a.Status)) + " " + string(a.Status), StatusColor(string(a.Status))
}

// Agent identity columns: the compact widths apply below the expanded minimum
// terminal width.
const (
	expandedIdentityMinWidth = 90
	compactAgentIDWidth      = 20
	compactAgentRoleWidth    = 16
	expandedAgentIDWidth     = 25
	expandedAgentRoleWidth   = 21
	// pausedBreakerLabel shortens the circuit-breaker pause gate to fit the STATUS column.
	pausedBreakerLabel = "BREAKER"
)

func (m Model) agentPauseLabel(a models.Agent) string {
	if m.state == nil || (a.Status != models.AgentStatusIdle && a.Status != models.AgentStatusWaiting) {
		return ""
	}
	reason := agent.RolePauseReason(m.state, m.roleTypes[a.Role])
	end := strings.Index(reason, "]")
	if !strings.HasPrefix(reason, "[") || end < 0 {
		return ""
	}
	if label := reason[1:end]; label != alerts.CategoryCircuitBreaker.String() {
		return label
	}
	return pausedBreakerLabel
}

func (m Model) agentIdentityColumnWidths() (idWidth, roleWidth int) {
	if m.width < expandedIdentityMinWidth {
		return compactAgentIDWidth, compactAgentRoleWidth
	}
	return expandedAgentIDWidth, expandedAgentRoleWidth
}

func (m Model) agentCurrentTaskColumnWidth() int {
	const (
		rowPrefixWidth   = 2
		panelBorderWidth = 2
		minCurrentTask   = 6
	)

	idWidth, roleWidth := m.agentIdentityColumnWidths()
	fixedWidth := idWidth + agentStatusColumnWidth + roleWidth + agentCLIColumnWidth + agentHealthColumnWidth
	if m.columnTier >= ColumnTierWide {
		fixedWidth += agentTimeOnTaskColumnWidth + agentLastHeartbeatColumnWidth
	}
	if m.columnTier >= ColumnTierFull {
		fixedWidth += agentPIDColumnWidth
	}

	// Fit the longest current task ID (cells truncate at width-1), within the
	// row width left after the fixed columns.
	contentWidth := runewidth.StringWidth("CURRENT_TASK")
	if m.state != nil {
		for _, a := range m.state.Agents {
			if a.CurrentTask != nil {
				contentWidth = max(contentWidth, runewidth.StringWidth(*a.CurrentTask))
			}
		}
	}

	rowBudget := max(m.width-panelBorderWidth-rowPrefixWidth, 0)
	available := max(rowBudget-fixedWidth, minCurrentTask)
	return min(contentWidth+1, available)
}

func currentAgentHealthDetailLineCount(state *models.State) int {
	details := currentAgentHealthDetails(state)
	if len(details) == 0 {
		return 0
	}
	return len(details) + 1
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func renderCurrentAgentHealthForState(state *models.State) string {
	details := currentAgentHealthDetails(state)
	if len(details) == 0 {
		return ""
	}

	lines := []string{"  Degraded capacity:"}
	for _, detail := range details {
		line := fmt.Sprintf("  %s %s %s", detail.agentID, detail.health.Role, detail.health.Reason)
		if detail.health.RecoverHint != "" {
			line += " " + detail.health.RecoverHint
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

type currentAgentHealthDetail struct {
	agentID string
	health  models.AgentHealth
}

func currentAgentHealthDetails(state *models.State) []currentAgentHealthDetail {
	if state == nil || len(state.AgentHealth) == 0 {
		return nil
	}
	ids := make([]string, 0, len(state.AgentHealth))
	for agentID, health := range state.AgentHealth {
		if health.State != models.AgentHealthDegraded {
			continue
		}
		if agentState, ok := state.Agents[agentID]; ok && !health.IsCurrentDegradedFor(agentState) {
			continue
		}
		ids = append(ids, agentID)
	}
	sort.Strings(ids)

	details := make([]currentAgentHealthDetail, 0, len(ids))
	for _, agentID := range ids {
		details = append(details, currentAgentHealthDetail{
			agentID: agentID,
			health:  state.AgentHealth[agentID],
		})
	}
	return details
}
