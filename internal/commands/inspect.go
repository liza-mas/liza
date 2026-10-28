package commands

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/render"
)

// InspectOptions contains options for the inspect command
type InspectOptions struct {
	Format        string   // Output format: json, yaml, table, value
	ProjectRoot   string   // Project root directory
	Internal      bool     // If true, return structured data for composition (not formatted string)
	Summary       bool     // If true, return compact entity summaries
	OutputSummary bool     // If true, return compact task output entries
	Active        bool     // If true, return only non-terminal tasks
	Zombies       bool     // If true, return live agent processes missing from state
	Fields        []string // Requested task fields, repeated or comma-separated
	WarnWriter    io.Writer
	// pipelineResolver carries the pipeline ID routing already loaded, so a
	// plain task-ID query does not parse it again for dependency waits.
	pipelineResolver models.PipelineResolver
}

// format is the requested output format as the inspect vocabulary type.
func (opts InspectOptions) format() inspectFormat {
	return inspectFormat(opts.Format)
}

// Validate checks if the inspect options are valid
func (opts *InspectOptions) Validate() error {
	if !slices.Contains(inspectFormats, opts.format()) {
		return fmt.Errorf("invalid format: %s (must be json, yaml, table, or value)", opts.Format)
	}
	if opts.Summary && opts.OutputSummary {
		return &errors.ValidationError{Message: "--summary and --output-summary are mutually exclusive"}
	}
	if len(opts.Fields) > 0 {
		if opts.Summary || opts.OutputSummary || opts.Zombies || opts.format() == inspectFormatTable {
			return &errors.ValidationError{Message: "--field cannot be combined with --summary, --output-summary, --zombies, or table format"}
		}
		var fields []string
		for _, group := range opts.Fields {
			for _, field := range strings.Split(group, ",") {
				field = strings.TrimSpace(field)
				if field == "" {
					return &errors.ValidationError{Message: "--field requires a non-empty task field"}
				}
				if !slices.Contains(fields, field) {
					fields = append(fields, field)
				}
			}
		}
		opts.Fields = fields
	}
	return nil
}

// InspectCommand is the main entry point for the inspect command.
// Routes to appropriate handlers based on the query
func InspectCommand(args []string, opts InspectOptions) (string, error) {
	if err := opts.Validate(); err != nil {
		return "", err
	}

	if len(args) == 0 {
		return "", fmt.Errorf("no field or entity specified")
	}

	// Read state
	statePath := paths.New(opts.ProjectRoot).StatePath()
	blackboard := db.For(statePath)
	state, err := blackboard.ReadSnapshot()
	if err != nil {
		return "", fmt.Errorf("failed to read state: %w", err)
	}

	// Parse the query
	query := args[0]

	// Check if query is a known entity type
	if isInspectTarget(query) {
		// Entity query
		return handleEntityQuery(state, inspectTarget(query), args[1:], opts)
	}
	if len(args) != 1 {
		return "", &errors.ValidationError{Message: "a field or ID query accepts exactly one argument; use tasks <task-id> for a literal task ID"}
	}
	if root, _, dotted := strings.Cut(query, queryPathSeparator); dotted {
		// Reserved state/computed paths retain precedence over task IDs.
		if isReservedTasksComputedQuery(query) {
			return handleFieldQuery(state, query, opts)
		}
		queryRoot := inspectQueryRoot(root)
		if queryRoot == inspectRootTask {
			path := strings.TrimPrefix(query, taskQueryPrefix)
			if dot := strings.LastIndex(path, queryPathSeparator); dot > 0 {
				field := path[dot+1:]
				if isTaskComputedField(field) {
					if task := state.FindTask(path[:dot]); task != nil {
						return handleTaskFieldQuery(task, field, opts)
					}
				}
			}
		}
		if queryRoot == inspectRootTasks || queryRoot == inspectRootTask {
			return handleTaskQuery(state, strings.TrimPrefix(query, root+queryPathSeparator), opts)
		}
		_, stateFieldErr := getField(state, root)
		if stateFieldErr == nil || isInspectTarget(root) || queryRoot == inspectRootAgent || queryRoot == inspectRootVersion {
			return handleFieldQuery(state, query, opts)
		}
		return handleTaskQuery(state, query, opts)
	}

	// Load role names from pipeline config for agent ID pattern detection
	var roleNames []string
	if opts.ProjectRoot != "" {
		if cfg, err := pipeline.LoadFrozen(opts.ProjectRoot); err == nil {
			resolver := pipeline.NewResolver(cfg)
			roleNames = resolver.AllRoleNames()
			opts.pipelineResolver = resolver
		}
	}

	// Check if query looks like an agent ID pattern
	if isAgentIDPattern(query, roleNames) {
		// Route to agent handler with the ID
		return handleEntityQuery(state, inspectTargetAgents, []string{query}, opts)
	}

	// Otherwise, assume it's a task ID and try to look it up
	return handleEntityQuery(state, inspectTargetTasks, []string{query}, opts)
}

// isAgentIDPattern returns true if the query looks like an agent ID.
// roleNames are the valid runtime role names from the pipeline config.
func isAgentIDPattern(query string, roleNames []string) bool {
	for _, role := range roleNames {
		if strings.HasPrefix(query, role+"-") {
			return true
		}
	}
	return false
}

// handleFieldQuery handles queries for specific fields (direct or computed)
func handleFieldQuery(state *models.State, fieldPath string, opts InspectOptions) (string, error) {
	if opts.Summary || opts.OutputSummary || opts.Active || opts.Zombies || len(opts.Fields) > 0 {
		return "", &errors.ValidationError{Message: "field queries do not support --field, --summary, --output-summary, --active, or --zombies"}
	}
	// Try direct field access first
	value, err := getField(state, fieldPath)
	if err == nil {
		return formatOutput(value, opts.format())
	}

	// If direct access failed with NotFoundError, try computed field
	if errors.IsNotFound(err) {
		value, err = getComputedField(state, fieldPath)
		if err != nil {
			return "", err
		}
		return formatOutput(value, opts.format())
	}

	return "", err
}

// handleEntityQuery handles queries for entities (tasks, agents, etc.)
func handleEntityQuery(state *models.State, entity inspectTarget, args []string, opts InspectOptions) (string, error) {
	if len(args) > 1 || len(args) > 0 && entity != inspectTargetTasks && entity != inspectTargetAgents {
		return "", &errors.ValidationError{Message: "only tasks and agents accept one optional ID; surplus arguments are not supported"}
	}
	if entity != inspectTargetTasks && (opts.Summary || opts.OutputSummary || opts.Active || len(opts.Fields) > 0) {
		return "", fmt.Errorf("--field, --summary, --output-summary, and --active are only supported for tasks")
	}
	if entity != inspectTargetAgents && opts.Zombies {
		return "", fmt.Errorf("--zombies is only supported for agents")
	}

	switch entity {
	case inspectTargetConfig:
		return formatOutput(state.Config, opts.format())
	case inspectTargetSprint:
		return formatOutput(state.Sprint, opts.format())
	case inspectTargetTasks:
		taskOpts := inspectTasksOptions{
			Format:           opts.format(),
			Summary:          opts.Summary,
			OutputSummary:    opts.OutputSummary,
			Active:           opts.Active,
			ProjectRoot:      opts.ProjectRoot,
			Fields:           opts.Fields,
			PipelineResolver: opts.pipelineResolver,
		}
		if len(args) > 0 {
			return asString(inspectTask(state, args[0], taskOpts))
		}
		return asString(inspectTasks(state, taskOpts))
	case inspectTargetAgents:
		agentOpts := inspectAgentsOptions{
			Format:      opts.format(),
			Zombies:     opts.Zombies,
			ProjectRoot: opts.ProjectRoot,
			WarnWriter:  opts.WarnWriter,
		}
		if len(args) > 0 {
			if opts.Zombies {
				return "", fmt.Errorf("--zombies does not support a specific agent ID")
			}
			return asString(inspectAgent(state, args[0], agentOpts))
		}
		return asString(inspectAgents(state, agentOpts))
	case inspectTargetMetrics:
		return asString(inspectMetrics(state, inspectMetricsOptions{Format: opts.format(), ProjectRoot: opts.ProjectRoot}))
	case inspectTargetAnomalies:
		return asString(inspectAnomalies(state, inspectAnomaliesOptions{Format: opts.format()}))
	case inspectTargetQuarantinedVerdicts:
		return formatOutput(state.QuarantinedVerdicts, opts.format())
	case inspectTargetProofReaffirmations:
		return formatOutput(state.ProofReaffirmations, opts.format())
	case inspectTargetHumanNotes:
		return formatOutput(humanNotesForInspect(state.HumanNotes), opts.format())
	default:
		return "", &errors.NotFoundError{Entity: string(entity)}
	}
}

// humanNotesForInspect exposes note content and factual operator provenance,
// without publishing arbitrary inline metadata as part of the query contract.
func humanNotesForInspect(notes []models.HumanNote) []map[string]any {
	result := make([]map[string]any, 0, len(notes))
	for _, note := range notes {
		item := map[string]any{
			"timestamp": note.Timestamp,
			"message":   note.Message,
			"for":       note.For,
		}
		for _, key := range []string{"source", "operation"} {
			if value, ok := note.Extra[key].(string); ok {
				item[key] = value
			}
		}
		result = append(result, item)
	}
	return result
}

// asString extracts the string result from an (any, error) return pair.
// Used by entity handlers that return formatted strings when not in internal mode.
func asString(result any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return result.(string), nil
}

// formatOutput formats the output based on the requested format
func formatOutput(data any, format inspectFormat) (string, error) {
	switch format {
	case inspectFormatJSON:
		return render.FormatJSON(data)
	case inspectFormatYAML:
		return render.FormatYAML(data)
	case inspectFormatValue:
		return render.FormatValue(data)
	case inspectFormatTable:
		// Table format requires specific structure
		return "", fmt.Errorf("table format requires entity query (e.g., %q)", brand.Command("inspect", "tasks"))
	default:
		return render.FormatValue(data)
	}
}
