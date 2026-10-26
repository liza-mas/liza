package prompts

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/scipsearch"
)

// BasePromptConfig contains configuration for building the base prompt
type BasePromptConfig struct {
	Role             string
	AgentID          string
	TaskID           string // empty for orchestrator
	SpecsDir         string
	ProjectRoot      string
	StatePath        string
	GoalDesc         string
	GoalSpecRef      string
	HasMandatoryDocs bool

	ScipSearchIndexes  []ScipSearchIndex
	StacklitIndexes    []StacklitIndex
	FunctionalClusters []FunctionalClusterIndex
	SembleSearch       SembleSearchMetadata
}

// ScipSearchIndex is prompt-safe metadata for one successful generated SCIP index.
type ScipSearchIndex struct {
	Language  string
	IndexPath string
}

// StacklitIndex is prompt-safe metadata for one generated Stacklit index.
type StacklitIndex struct {
	IndexPath string
}

// FunctionalClusterIndex is prompt-safe metadata for one generated Functional Clusters artifact.
type FunctionalClusterIndex struct {
	IndexPath string
}

// SembleSearchMetadata is prompt-safe metadata for one authorized Semble target root.
type SembleSearchMetadata struct {
	TargetRoot      string
	ShellTargetRoot string
}

type basePromptTemplateData struct {
	BasePromptConfig
	ScipSearchIndexes  []scipSearchPromptIndex
	StacklitIndexes    []stacklitPromptIndex
	FunctionalClusters []functionalClusterPromptIndex
	SembleSearch       *SembleSearchMetadata
}

type scipSearchPromptIndex struct {
	DisplayLanguage           string
	IndexPath                 string
	ShellIndexPath            string
	CapabilitySummary         string
	ImplementationGuidance    string
	ShowImplementationCommand bool
}

type stacklitPromptIndex struct {
	IndexPath      string
	ShellIndexPath string
}

type functionalClusterPromptIndex struct {
	IndexPath      string
	ShellIndexPath string
}

// SiblingTaskSummary provides minimal context about phase dependency tasks.
type SiblingTaskSummary struct {
	ID          string
	Description string
	Status      string
	PlanRef     string
	RolePair    string
}

// BuildBasePrompt creates the base bootstrap prompt for all agents
func BuildBasePrompt(config BasePromptConfig) (string, error) {
	data := basePromptTemplateData{
		BasePromptConfig:   config,
		ScipSearchIndexes:  buildScipSearchPromptIndexes(config.ScipSearchIndexes),
		StacklitIndexes:    buildStacklitPromptIndexes(config.StacklitIndexes),
		FunctionalClusters: buildFunctionalClusterPromptIndexes(config.FunctionalClusters),
		SembleSearch:       buildSembleSearchPromptMetadata(config.SembleSearch),
	}
	return executeTemplate("base_prompt", data)
}

func buildSembleSearchPromptMetadata(metadata SembleSearchMetadata) *SembleSearchMetadata {
	if metadata.TargetRoot == "" || metadata.ShellTargetRoot == "" {
		return nil
	}
	copy := metadata
	return &copy
}

func buildStacklitPromptIndexes(indexes []StacklitIndex) []stacklitPromptIndex {
	promptIndexes := make([]stacklitPromptIndex, 0, len(indexes))
	for _, index := range indexes {
		if index.IndexPath == "" {
			continue
		}
		promptIndexes = append(promptIndexes, stacklitPromptIndex{
			IndexPath:      index.IndexPath,
			ShellIndexPath: shellQuote(index.IndexPath),
		})
	}
	return promptIndexes
}

func buildFunctionalClusterPromptIndexes(indexes []FunctionalClusterIndex) []functionalClusterPromptIndex {
	promptIndexes := make([]functionalClusterPromptIndex, 0, len(indexes))
	for _, index := range indexes {
		if index.IndexPath == "" {
			continue
		}
		promptIndexes = append(promptIndexes, functionalClusterPromptIndex{
			IndexPath:      index.IndexPath,
			ShellIndexPath: shellQuote(index.IndexPath),
		})
	}
	return promptIndexes
}

func buildScipSearchPromptIndexes(indexes []ScipSearchIndex) []scipSearchPromptIndex {
	ordered := append([]ScipSearchIndex(nil), indexes...)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := scipSearchLanguageOrder(ordered[i].Language)
		right := scipSearchLanguageOrder(ordered[j].Language)
		if left != right {
			return left < right
		}
		return ordered[i].Language < ordered[j].Language
	})

	promptIndexes := make([]scipSearchPromptIndex, 0, len(ordered))
	for _, index := range ordered {
		if index.Language == "" || index.IndexPath == "" {
			continue
		}
		promptIndexes = append(promptIndexes, scipSearchPromptIndexFor(index))
	}
	return promptIndexes
}

// Display order of the SCIP indexes a prompt lists: the supported languages in
// this order, then any other.
const (
	scipRankGo = iota
	scipRankTypeScript
	scipRankPython
	scipRankOther
)

func scipSearchLanguageOrder(language string) int {
	switch scipsearch.Language(strings.ToLower(language)) {
	case scipsearch.LanguageGo:
		return scipRankGo
	case scipsearch.LanguageTypeScript:
		return scipRankTypeScript
	case scipsearch.LanguagePython:
		return scipRankPython
	default:
		return scipRankOther
	}
}

func scipSearchPromptIndexFor(index ScipSearchIndex) scipSearchPromptIndex {
	switch scipsearch.Language(strings.ToLower(index.Language)) {
	case scipsearch.LanguageGo:
		return scipSearchPromptIndex{
			DisplayLanguage:           "Go",
			IndexPath:                 index.IndexPath,
			ShellIndexPath:            shellQuote(index.IndexPath),
			CapabilitySummary:         "Go symbols, references, implementations, packages, and static graph hints are supported.",
			ImplementationGuidance:    "Use implementations, graph, callers, callees, and impact for static SCIP-derived hints, then verify in files.",
			ShowImplementationCommand: true,
		}
	case scipsearch.LanguageTypeScript:
		return scipSearchPromptIndex{
			DisplayLanguage:           "TypeScript",
			IndexPath:                 index.IndexPath,
			ShellIndexPath:            shellQuote(index.IndexPath),
			CapabilitySummary:         "TypeScript symbols, references, implementations, packages, and static graph hints are supported.",
			ImplementationGuidance:    "Use implementations, graph, callers, callees, and impact for static SCIP-derived hints, then verify in files.",
			ShowImplementationCommand: true,
		}
	case scipsearch.LanguagePython:
		return scipSearchPromptIndex{
			DisplayLanguage:        "Python",
			IndexPath:              index.IndexPath,
			ShellIndexPath:         shellQuote(index.IndexPath),
			CapabilitySummary:      "Python symbols, references, packages, and static graph hints are supported by scip-search; implementation rows may be absent.",
			ImplementationGuidance: "Prefer references, graph, callers, callees, and impact for Python navigation, then verify in files.",
		}
	default:
		return scipSearchPromptIndex{
			DisplayLanguage:   index.Language,
			IndexPath:         index.IndexPath,
			ShellIndexPath:    shellQuote(index.IndexPath),
			CapabilitySummary: "Symbols, references, packages, and static graph hints are available for this supplied SCIP index.",
		}
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// RenderOrchestratorDashboard pre-renders the orchestrator dashboard and wake instruction
// strings for use as RoleContextData.DashboardOutput and RoleContextData.WakeInstruction.
// The runtime resolves in and selects decision; this renders them and reads nothing else.
func RenderOrchestratorDashboard(in *OrchestratorDashboardInput, decision OrchestratorWakeDecision) (dashboard, wakeInstruction string, err error) {
	return renderOrchestratorDashboard(in, decision)
}

func renderOrchestratorDashboard(in *OrchestratorDashboardInput, decision OrchestratorWakeDecision) (dashboard, wakeInstruction string, err error) {
	wakeData := in.wakeTemplateData(decision.Integration)
	if decision.Trigger == WakeBlockedTasks {
		wakeData.BlockedScope = &blockedScopeData{TaskIDs: in.ActionableBlockedIDs}
	}
	wakeInstructions, instrErr := buildInstructionsForWakeTrigger(decision.Trigger, in.AgentID, wakeData, in.PlanningTasks, in.UnseenNotes)
	if instrErr != nil {
		return "", "", fmt.Errorf("building wake instructions: %w", instrErr)
	}
	commands, cmdErr := executeTemplate("orchestrator_commands", struct{ AgentID string }{in.AgentID})
	if cmdErr != nil {
		return "", "", fmt.Errorf("building orchestrator commands: %w", cmdErr)
	}

	var b strings.Builder
	b.WriteString("\n\n=== ORCHESTRATOR CONTEXT ===\n")
	b.WriteString(fmt.Sprintf("WAKE TRIGGER: %s\n", decision.Trigger))
	writeSprintState(&b, in)
	writeActiveTaskDigest(&b, in.State.Tasks)
	writeIntegrationProgressDiagnostic(&b, decision.Integration)
	b.WriteString("\nORCHESTRATOR COMMANDS:\n")
	b.WriteString(commands)

	// Wake instruction is rendered separately by the wake-instructions block
	return b.String(), fmt.Sprintf("INSTRUCTIONS:\n%s", wakeInstructions), nil
}

// writeSprintState writes the SPRINT STATE section: raw counts over the
// snapshot, never the actionable subsets that select a wake.
func writeSprintState(b *strings.Builder, in *OrchestratorDashboardInput) {
	state := in.State
	inProgress := countTasksByStatus(state.Tasks, models.TaskStatusImplementing) +
		countTasksByStatus(state.Tasks, models.TaskStatusReadyForReview) +
		countTasksByStatus(state.Tasks, models.TaskStatusApproved)

	b.WriteString("\nSPRINT STATE:\n")
	if state.Sprint.Number > 0 {
		b.WriteString(fmt.Sprintf("- Sprint number: %d\n", state.Sprint.Number))
	}
	if len(state.SprintHistory) > 0 {
		b.WriteString(fmt.Sprintf("- Previous sprints: %d\n", len(state.SprintHistory)))
		for _, sh := range state.SprintHistory {
			b.WriteString(fmt.Sprintf("  - %s: %s (%d tasks done)\n", sh.ID, sh.Status, sh.TasksDone))
		}
	}
	b.WriteString(fmt.Sprintf("- Total tasks: %d\n", len(state.Tasks)))
	b.WriteString(fmt.Sprintf("- Merged: %d\n", countTasksByStatus(state.Tasks, models.TaskStatusMerged)))
	b.WriteString(fmt.Sprintf("- In progress: %d\n", inProgress))
	b.WriteString(fmt.Sprintf("- Unclaimed: %d\n", countTasksByStatus(state.Tasks, models.TaskStatusReady)))
	b.WriteString(fmt.Sprintf("- Blocked: %d\n", countTasksByStatus(state.Tasks, models.TaskStatusBlocked)))
	b.WriteString(fmt.Sprintf("- Integration failed: %d\n", countTasksByStatus(state.Tasks, models.TaskStatusIntegrationFailed)))
	b.WriteString(fmt.Sprintf("- Hypothesis exhausted: %d\n", countHypothesisExhausted(state.Tasks)))
	b.WriteString(fmt.Sprintf("- Immediate discoveries: %d\n", countImmediateDiscoveries(state.Discovered)))
	b.WriteString(fmt.Sprintf("- Unseen operator notes: %d\n", len(in.UnseenNotes)))
	if in.CycleBlocked > 0 {
		b.WriteString(fmt.Sprintf("- Cycle-blocked planning: %d\n", in.CycleBlocked))
	}
}

func writeActiveTaskDigest(b *strings.Builder, tasks []models.Task) {
	b.WriteString("\nACTIVE TASK DIGEST:\n")
	count := 0
	for i := range tasks {
		task := &tasks[i]
		if task.Status.IsTerminal() {
			continue
		}
		count++
		if count > activeTaskDigestLimit {
			b.WriteString(fmt.Sprintf("- ... additional active tasks omitted; use `%s get tasks --active --summary --json` if orchestration needs a fresh active-task scan.\n", promptBinaryName()))
			break
		}
		b.WriteString(fmt.Sprintf("- %s [%s]", task.ID, task.Status))
		if task.RolePair != "" {
			b.WriteString(fmt.Sprintf(" role_pair=%s", task.RolePair))
		}
		if task.AssignedTo != nil && *task.AssignedTo != "" {
			b.WriteString(fmt.Sprintf(" assigned_to=%s", *task.AssignedTo))
		}
		if len(task.DependsOn) > 0 {
			b.WriteString(fmt.Sprintf(" depends_on=%s", strings.Join(task.DependsOn, ",")))
		}
		b.WriteString("\n")
		if task.BlockedReason != nil && *task.BlockedReason != "" {
			b.WriteString(fmt.Sprintf("  blocker: %s\n", TruncateText(*task.BlockedReason, blockerDigestLimit)))
		}
		if task.RepairRequest != nil && task.RepairRequest.Operation != "" {
			b.WriteString(fmt.Sprintf("  repair: %s", task.RepairRequest.Operation))
			if task.RepairRequest.Target != "" {
				b.WriteString(fmt.Sprintf(" -> %s", task.RepairRequest.Target))
			}
			b.WriteString("\n")
		}
	}
	if count == 0 {
		b.WriteString("- none\n")
	}
}

func countTasksByStatus(tasks []models.Task, status models.TaskStatus) int {
	count := 0
	for _, task := range tasks {
		if task.Status == status {
			count++
		}
	}
	return count
}

// countHypothesisExhausted counts non-terminal tasks that have been failed by 2+ reviewers.
func countHypothesisExhausted(tasks []models.Task) int {
	count := 0
	for _, task := range tasks {
		if len(task.FailedBy) >= hypothesisExhaustionFailures && !task.Status.IsTerminal() {
			count++
		}
	}
	return count
}

// countImmediateDiscoveries counts unresolved discoveries with "immediate" urgency.
func countImmediateDiscoveries(discovered []models.Discovery) int {
	count := 0
	for _, disc := range discovered {
		if disc.Urgency == models.DiscoveryUrgencyImmediate && disc.ConvertedToTask == nil {
			count++
		}
	}
	return count
}

// hasIntegrationTaskInSprint checks if any planned task uses the integration-pair role-pair.
func hasIntegrationTaskInSprint(state *models.State) bool {
	for _, taskID := range state.Sprint.Scope.Planned {
		task := state.FindTask(taskID)
		if task != nil && task.RolePair == integrationRolePair {
			return true
		}
	}
	return false
}

// BuildRoleContext assembles role-specific context by rendering the named template
// blocks in order and concatenating their output. Each block is a modular .tmpl file
// in templates/blocks/ that receives a unified RoleContextData.
//
// Block boundaries are normalized here rather than relying on template whitespace
// control ({{- -}} trimming), which is fragile and linter-hostile. Each non-empty
// block is TrimSpace'd and joined with a blank-line separator.
func BuildRoleContext(role string, sectionNames []string, data *RoleContextData) (string, error) {
	if data != nil && data.CorrectiveContext != nil && (data.RoleType == string(roles.RoleTypeDoer) || data.RoleType == string(roles.RoleTypeReviewer)) {
		// Recovery evidence must reach custom task roles even when their section
		// configuration omits it. Mandatory reads still render first below.
		sectionNames = append([]string{"task-corrective-context"}, sectionNames...)
	}
	if data != nil && len(data.MandatoryDocs) > 0 {
		// Mandatory reads cannot be omitted or delayed by context-section configuration.
		root := data.ProjectRoot
		if data.Worktree != "" {
			root = data.Worktree
		}
		copy := *data
		copy.MandatoryDocs = make([]string, len(data.MandatoryDocs))
		for i, doc := range data.MandatoryDocs {
			if !filepath.IsAbs(doc) {
				doc = filepath.Join(root, doc)
			}
			copy.MandatoryDocs[i] = doc
		}
		data = &copy
		sectionNames = append([]string{"mandatory-docs"}, sectionNames...)
	}
	missingReviewBoundary := data != nil && data.RoleType == string(roles.RoleTypeReviewer) && data.ReviewCommit == ""
	if missingReviewBoundary {
		// Keep the absent boundary explicit without changing the caller's task data.
		copy := *data
		copy.ReviewCommit = "REVIEW_COMMIT_MISSING"
		data = &copy
	}
	var blocks []string
	mandatoryDocsRendered := false
	correctiveContextRendered := false
	for _, section := range sectionNames {
		if section == "task-corrective-context" {
			if correctiveContextRendered {
				continue
			}
			correctiveContextRendered = true
		}
		if section == "mandatory-docs" {
			if mandatoryDocsRendered {
				continue
			}
			mandatoryDocsRendered = true
		}
		var sectionBuf bytes.Buffer
		if err := blockTmpl.ExecuteTemplate(&sectionBuf, section, data); err != nil {
			return "", fmt.Errorf("block template %q for role %q: %w", section, role, err)
		}
		rendered := strings.TrimSpace(sectionBuf.String())
		if rendered == "" {
			continue
		}
		blocks = append(blocks, rendered)
	}
	if len(blocks) == 0 {
		return "", nil
	}
	if missingReviewBoundary {
		blocks = append([]string{"REVIEW BOUNDARY MISSING: Stop; do not review or submit a verdict, including failure recovery. REVIEW_COMMIT_MISSING is a placeholder, not a reviewed SHA. Ask the authorized orchestrator to repair review_commit, then restart review with that explicit boundary. Do not infer a SHA or borrow another agent's authority."}, blocks...)
	}
	// Leading \n\n separates from base prompt; \n\n between blocks = one blank line.
	return "\n\n" + strings.Join(blocks, "\n\n") + "\n", nil
}
