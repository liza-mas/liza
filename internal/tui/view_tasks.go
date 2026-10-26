package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
	"github.com/mattn/go-runewidth"
)

// Task panel column widths. The ID and DESCRIPTION columns flex with the
// terminal (taskIDColumnWidth, fitFlexColumns).
const (
	taskStatusColumnWidth       = 29
	taskAttemptColumnWidth      = 6
	taskAssignedColumnWidth     = 16
	taskReviewingColumnWidth    = 16
	taskDepsColumnWidth         = 16
	taskAgeColumnWidth          = 8
	taskTimeInStatusColumnWidth = 16
	taskDescriptionMinWidth     = 16
	taskRowPrefixWidth          = 2
	taskPanelBorderWidth        = 2
)

// taskColumn is one column of the task panel. A zero width marks the flex column.
type taskColumn struct {
	header string
	width  int
	value  func(t models.Task) string
	// styled marks a value carrying ANSI styling: it is padded by the visual
	// width of the task's raw status text instead of being truncated.
	styled bool
}

// renderTaskPanel renders the task panel as a bordered table.
// Columns adapt to terminal width per spec §Task Panel column priority table.
// Terminal tasks (MERGED, ABANDONED, SUPERSEDED) shown dimmed at bottom.
// Panel header includes sprint metrics.
// budget is the maximum total panel height (including border). The panel
// renders at its natural content height (no blank padding) with MaxHeight
// as ceiling.
func (m Model) renderTaskPanel(budget int) string {
	title := m.renderTaskPanelTitle()

	// Handle empty/nil state
	if m.state == nil || len(m.state.Tasks) == 0 {
		return m.styles.TaskPanel.MaxHeight(budget).Render(title + "\n  No tasks")
	}

	tasks := m.listedTasks()
	if len(tasks) == 0 {
		return m.styles.TaskPanel.MaxHeight(budget).Render(title + "\n  No matching tasks")
	}

	cols := m.taskColumns()
	maxRows := max(budget-panelOverhead, 0)
	rows := m.renderTaskRows(cols, tasks[:min(len(tasks), maxRows)])

	content := title + "\n" + renderTaskHeaderRow(cols) + "\n" + strings.Join(rows, "\n")
	return m.styles.TaskPanel.MaxHeight(budget).Render(content)
}

// renderTaskPanelTitle is the panel title, with the sprint metrics right-aligned.
func (m Model) renderTaskPanelTitle() string {
	titleText := "✔ TASKS"
	if label := m.taskFilter.Label(); label != "" {
		titleText += " · " + label
	}
	title := m.styles.PanelTitle.Render(titleText)
	if m.state == nil {
		return title
	}

	total := len(m.state.Sprint.Scope.Planned)
	if total == 0 {
		total = len(m.state.Tasks)
	}
	sm := m.state.ComputeSprintMetricsWithTerminalStates(m.sprintTerminals)
	metrics := fmt.Sprintf("%d/%d done │ %d blocked │ %d%% approval",
		sm.TasksDone, total, sm.TasksBlocked, sm.TaskOutcomeApprovalRatePercent)

	metricsStyled := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(metrics)
	titleWidth := lipgloss.Width(title)
	metricsWidth := lipgloss.Width(metricsStyled)
	// Account for panel border padding (2 chars each side)
	availWidth := m.width - 4
	gap := availWidth - titleWidth - metricsWidth
	if gap < 2 {
		gap = 2
	}
	return title + strings.Repeat(" ", gap) + metricsStyled
}

// listedTasks projects the rows of the task panel: the tasks the filter lists,
// oldest first, active tasks before terminal ones.
func (m Model) listedTasks() []models.Task {
	// Sort tasks by created timestamp (oldest first), preserving creation order
	tasks := make([]models.Task, len(m.state.Tasks))
	copy(tasks, m.state.Tasks)
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].Created.Before(tasks[j].Created)
	})

	// Partition: active tasks first, terminal tasks after.
	// Both partitions preserve Created-ascending order.
	var active, terminal []models.Task
	for _, t := range tasks {
		if !m.taskVisible(t) {
			continue
		}
		if m.isOperationallyTerminal(t) {
			terminal = append(terminal, t)
		} else {
			active = append(active, t)
		}
	}
	return append(active, terminal...)
}

// taskColumns lists the columns visible at the current column tier.
func (m Model) taskColumns() []taskColumn {
	cols := []taskColumn{
		{header: "ID", width: m.taskIDColumnWidth(), value: func(t models.Task) string { return t.ID }},
		{header: "STATUS", width: taskStatusColumnWidth, value: m.taskStatusCell, styled: true},
	}

	if m.columnTier >= ColumnTierStandard {
		cols = append(cols,
			taskColumn{header: "ATT", width: taskAttemptColumnWidth, value: taskAttemptCell},
			taskColumn{header: "ASSIGNED_TO", width: taskAssignedColumnWidth, value: taskAssignedCell},
			taskColumn{header: "REVIEWING_BY", width: taskReviewingColumnWidth, value: taskReviewingByCell},
		)
	}

	if m.columnTier >= ColumnTierWide {
		cols = append(cols, taskColumn{header: "DESCRIPTION", value: taskDescriptionCell})
	}

	if m.columnTier >= ColumnTierFull {
		cols = append(cols,
			taskColumn{header: "DEPS", width: taskDepsColumnWidth, value: taskDepsCell},
			taskColumn{header: "AGE", width: taskAgeColumnWidth, value: taskAgeCell},
			taskColumn{header: "TIME_IN_STATUS", width: taskTimeInStatusColumnWidth, value: taskTimeInStatusCell},
		)
	}

	m.fitFlexColumns(cols)
	return cols
}

// fitFlexColumns gives the flex column the width left after the fixed columns.
func (m Model) fitFlexColumns(cols []taskColumn) {
	fixedWidth := taskRowPrefixWidth + taskPanelBorderWidth
	for _, c := range cols {
		fixedWidth += c.width
	}
	for i := range cols {
		if cols[i].width == 0 {
			cols[i].width = max(m.width-fixedWidth, taskDescriptionMinWidth)
		}
	}
}

// rawTaskStatusText is the STATUS cell text without styling.
func rawTaskStatusText(t models.Task) string {
	return StatusDot(string(t.Status)) + " " + string(t.Status)
}

func (m Model) taskStatusCell(t models.Task) string {
	color := TaskStatusColor(t.Status, m.stateCategories)
	return lipgloss.NewStyle().Foreground(color).Render(rawTaskStatusText(t))
}

func taskAttemptCell(t models.Task) string {
	return fmt.Sprintf("%d.%d", t.EffectiveAttempt(), t.Iteration)
}

func taskAssignedCell(t models.Task) string {
	if t.AssignedTo != nil {
		return *t.AssignedTo
	}
	return emptyCell
}

func taskReviewingByCell(t models.Task) string {
	if t.ReviewingBy != nil {
		return *t.ReviewingBy
	}
	return emptyCell
}

func taskDescriptionCell(t models.Task) string {
	if t.Description == "" {
		return emptyCell
	}
	return t.Description
}

func taskDepsCell(t models.Task) string {
	if len(t.DependsOn) == 0 {
		return emptyCell
	}
	return strings.Join(t.DependsOn, ",")
}

func taskAgeCell(t models.Task) string {
	if t.Created.IsZero() {
		return emptyCell
	}
	return render.FormatDuration(time.Since(t.Created))
}

func taskTimeInStatusCell(t models.Task) string {
	if len(t.History) == 0 && t.Created.IsZero() {
		return emptyCell
	}
	return render.FormatDuration(models.TimeInStatus(&t, time.Now()))
}

func renderTaskHeaderRow(cols []taskColumn) string {
	var headerParts []string
	for _, c := range cols {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", c.width, c.header))
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8")).
		Render("  " + strings.Join(headerParts, ""))
}

// renderTaskRows renders one row per task; terminal tasks are dimmed.
func (m Model) renderTaskRows(cols []taskColumn, tasks []models.Task) []string {
	var rows []string
	for _, t := range tasks {
		var parts []string
		for _, c := range cols {
			parts = append(parts, renderTaskCell(c, t))
		}
		row := "  " + strings.Join(parts, "")
		if m.isOperationallyTerminal(t) {
			row = m.styles.Dimmed.Render(row)
		}
		rows = append(rows, row)
	}
	return rows
}

func renderTaskCell(c taskColumn, t models.Task) string {
	val := c.value(t)
	if c.styled {
		// The value is ANSI-styled; pad by visual width of raw text.
		padding := max(c.width-runewidth.StringWidth(rawTaskStatusText(t)), 0)
		return val + strings.Repeat(" ", padding)
	}
	return padRight(truncateVisual(val, c.width-1), c.width)
}

func (m Model) taskIDColumnWidth() int {
	const (
		preferredIDWidth = 67
		minIDWidth       = 8
	)

	otherWidth := taskStatusColumnWidth
	if m.columnTier >= ColumnTierStandard {
		otherWidth += taskAttemptColumnWidth + taskAssignedColumnWidth + taskReviewingColumnWidth
	}
	if m.columnTier >= ColumnTierWide {
		otherWidth += taskDescriptionMinWidth
	}
	if m.columnTier >= ColumnTierFull {
		otherWidth += taskDepsColumnWidth + taskAgeColumnWidth + taskTimeInStatusColumnWidth
	}

	rowBudget := max(m.width-taskPanelBorderWidth-taskRowPrefixWidth, 0)
	available := rowBudget - otherWidth
	if available <= minIDWidth {
		return max(available, 1)
	}
	return min(available, preferredIDWidth)
}
