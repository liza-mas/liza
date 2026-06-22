package commands

import (
	"fmt"
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
	Format        string // Output format: json, yaml, table, value
	ProjectRoot   string // Project root directory
	Internal      bool   // If true, return structured data for composition (not formatted string)
	Summary       bool   // If true, return compact entity summaries
	OutputSummary bool   // If true, return compact task output entries
	Active        bool   // If true, return only non-terminal tasks
	Zombies       bool   // If true, return live agent processes missing from state
}

// Validate checks if the inspect options are valid
func (opts *InspectOptions) Validate() error {
	validFormats := []string{"json", "yaml", "table", "value", ""}
	if !slices.Contains(validFormats, opts.Format) {
		return fmt.Errorf("invalid format: %s (must be json, yaml, table, or value)", opts.Format)
	}
	if opts.Summary && opts.OutputSummary {
		return &errors.ValidationError{Message: "--summary and --output-summary are mutually exclusive"}
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
	state, err := blackboard.Read()
	if err != nil {
		return "", fmt.Errorf("failed to read state: %w", err)
	}

	// Parse the query
	query := args[0]

	// Determine if this is a field query or entity query
	// Field queries contain dots (e.g., "config.mode", "sprint.status")
	// Entity queries are single words (e.g., "tasks", "agents", "summary")
	if strings.Contains(query, ".") {
		// Field query - direct or computed
		return handleFieldQuery(state, query, opts)
	}

	// Check if query is a known entity type
	if isKnownEntityType(query) {
		// Entity query
		return handleEntityQuery(state, query, args[1:], opts)
	}

	// Load role names from pipeline config for agent ID pattern detection
	var roleNames []string
	if opts.ProjectRoot != "" {
		if cfg, err := pipeline.LoadFrozen(opts.ProjectRoot); err == nil {
			roleNames = pipeline.NewResolver(cfg).AllRoleNames()
		}
	}

	// Check if query looks like an agent ID pattern
	if isAgentIDPattern(query, roleNames) {
		// Route to agent handler with the ID
		return handleEntityQuery(state, "agents", []string{query}, opts)
	}

	// Otherwise, assume it's a task ID and try to look it up
	return handleEntityQuery(state, "tasks", []string{query}, opts)
}

// isKnownEntityType returns true if the query is a known entity type
func isKnownEntityType(query string) bool {
	knownTypes := []string{"config", "sprint", "tasks", "agents", "metrics", "anomalies"}
	return slices.Contains(knownTypes, query)
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
	// Try direct field access first
	value, err := getField(state, fieldPath)
	if err == nil {
		return formatOutput(value, opts.Format)
	}

	// If direct access failed with NotFoundError, try computed field
	if errors.IsNotFound(err) {
		value, err = getComputedField(state, fieldPath)
		if err != nil {
			return "", err
		}
		return formatOutput(value, opts.Format)
	}

	return "", err
}

// handleEntityQuery handles queries for entities (tasks, agents, etc.)
func handleEntityQuery(state *models.State, entity string, args []string, opts InspectOptions) (string, error) {
	if entity != "tasks" && (opts.Summary || opts.OutputSummary || opts.Active) {
		return "", fmt.Errorf("--summary, --output-summary, and --active are only supported for tasks")
	}
	if entity != "agents" && opts.Zombies {
		return "", fmt.Errorf("--zombies is only supported for agents")
	}

	switch entity {
	case "config":
		return formatOutput(state.Config, opts.Format)
	case "sprint":
		return formatOutput(state.Sprint, opts.Format)
	case "tasks":
		taskOpts := inspectTasksOptions{
			Format:        opts.Format,
			Summary:       opts.Summary,
			OutputSummary: opts.OutputSummary,
			Active:        opts.Active,
			ProjectRoot:   opts.ProjectRoot,
		}
		if len(args) > 0 {
			return asString(inspectTask(state, args[0], taskOpts))
		}
		return asString(inspectTasks(state, taskOpts))
	case "agents":
		agentOpts := inspectAgentsOptions{
			Format:      opts.Format,
			Zombies:     opts.Zombies,
			ProjectRoot: opts.ProjectRoot,
		}
		if len(args) > 0 {
			if opts.Zombies {
				return "", fmt.Errorf("--zombies does not support a specific agent ID")
			}
			return asString(inspectAgent(state, args[0], agentOpts))
		}
		return asString(inspectAgents(state, agentOpts))
	case "metrics":
		return asString(inspectMetrics(state, inspectMetricsOptions{Format: opts.Format}))
	case "anomalies":
		return asString(inspectAnomalies(state, inspectAnomaliesOptions{Format: opts.Format}))
	default:
		return "", &errors.NotFoundError{Entity: entity}
	}
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
func formatOutput(data any, format string) (string, error) {
	switch format {
	case "json":
		return render.FormatJSON(data)
	case "yaml":
		return render.FormatYAML(data)
	case "value":
		return render.FormatValue(data)
	case "table":
		// Table format requires specific structure
		return "", fmt.Errorf("table format requires entity query (e.g., %q)", brand.Command("inspect", "tasks"))
	default:
		return render.FormatValue(data)
	}
}
