package commands

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/render"
	"gopkg.in/yaml.v3"
)

// GetField accesses direct state fields using dot notation
// Examples: "config.mode", "sprint.status", "sprint.metrics.tasks_done"
func getField(state *models.State, fieldPath string) (any, error) {
	if fieldPath == "" {
		return nil, &errors.NotFoundError{Entity: "field", Field: fieldPath}
	}

	parts := strings.Split(fieldPath, ".")
	if slices.Contains(parts, "") {
		return nil, &errors.NotFoundError{Entity: "field", Field: fieldPath}
	}

	// Preserve existing version-path behavior.
	if parts[0] == "version" && len(parts) > 1 {
		return nil, &errors.NotFoundError{Entity: "state", Field: fieldPath}
	}

	return resolveFieldByYAMLPath(reflect.ValueOf(state), parts, "")
}

func resolveFieldByYAMLPath(current reflect.Value, parts []string, entityPath string) (any, error) {
	// Optional objects still have a schema: validate the remaining path against
	// their zero value, but report a valid absent descendant as null.
	if current.IsValid() && current.Kind() == reflect.Pointer && current.IsNil() {
		_, err := resolveFieldByYAMLPath(reflect.Zero(current.Type().Elem()), parts, entityPath)
		return nil, err
	}
	current = derefReflectValue(current)
	if !current.IsValid() {
		if entityPath == "" {
			return nil, &errors.NotFoundError{Entity: "field", Field: strings.Join(parts, ".")}
		}
		return nil, &errors.NotFoundError{Entity: entityPath, Field: ""}
	}

	if len(parts) == 0 {
		return normalizeFieldValue(current), nil
	}
	if current.Kind() != reflect.Struct {
		if entityPath == "" {
			return nil, &errors.NotFoundError{Entity: parts[0], Field: ""}
		}
		return nil, &errors.NotFoundError{Entity: entityPath, Field: parts[0]}
	}

	part := parts[0]
	next, ok := findFieldByYAMLTag(current, part)
	if !ok {
		if entityPath == "" {
			return nil, &errors.NotFoundError{Entity: part, Field: ""}
		}
		return nil, &errors.NotFoundError{Entity: entityPath, Field: part}
	}

	nextEntityPath := part
	if entityPath != "" {
		nextEntityPath = entityPath + "." + part
	}

	if len(parts) == 1 {
		// Historical asymmetry: "sprint.metrics" returns the whole struct, but
		// "sprint.timeline" required a sub-field in the original switch-based
		// implementation. Preserved here for backward compatibility.
		if nextEntityPath == "sprint.timeline" {
			if value := derefReflectValue(next); value.IsValid() && value.Kind() == reflect.Struct {
				return nil, &errors.NotFoundError{Entity: "sprint.timeline", Field: ""}
			}
		}
		return normalizeFieldValue(next), nil
	}

	return resolveFieldByYAMLPath(next, parts[1:], nextEntityPath)
}

func findFieldByYAMLTag(value reflect.Value, tagName string) (reflect.Value, bool) {
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get("inspect") == "-" {
			continue
		}
		yamlTag := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		if yamlTag == tagName {
			return value.Field(i), true
		}
	}
	return reflect.Value{}, false
}

func normalizeFieldValue(value reflect.Value) any {
	value = derefReflectValue(value)
	if !value.IsValid() {
		return nil
	}
	// Keep inspect output stable for string aliases like models.SystemMode.
	if value.Kind() == reflect.String {
		return value.String()
	}
	// Raw fields can be rendered as YAML/value as well as JSON. JSON exclusion
	// tags alone do not protect persistence-only lifecycle authority.
	switch tasks := value.Interface().(type) {
	case models.Task:
		return redactTaskLifecycleForInspection(tasks)
	case []models.Task:
		result := slices.Clone(tasks)
		for i := range result {
			result[i] = redactTaskLifecycleForInspection(result[i])
		}
		return result
	case models.ValidationReadiness:
		tasks.Generation = ""
		return tasks
	case map[string]map[string]models.ValidationReadiness:
		result := maps.Clone(tasks)
		for agentID, records := range result {
			records = maps.Clone(records)
			for taskID, record := range records {
				record.Generation = ""
				records[taskID] = record
			}
			result[agentID] = records
		}
		return result
	}
	return value.Interface()
}

// Copy only the metadata being redacted; inspection must not mutate the state
// used by lifecycle authority or receipt matching.
func redactTaskLifecycleForInspection(task models.Task) models.Task {
	if task.Lifecycle == nil {
		return task
	}
	lifecycle := *task.Lifecycle
	lifecycle.Receipts = slices.Clone(lifecycle.Receipts)
	for i := range lifecycle.Receipts {
		lifecycle.Receipts[i].GenerationDigest = ""
	}
	if lifecycle.Preparation != nil {
		preparation := *lifecycle.Preparation
		preparation.GenerationDigest = ""
		lifecycle.Preparation = &preparation
	}
	task.Lifecycle = &lifecycle
	return task
}

func derefReflectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

// handleTaskQuery resolves a literal ID before trying the longest existing ID
// prefix. IDs themselves may contain dots; the space form always selects a
// literal ID, including one that collides with a reserved state query.
func handleTaskQuery(state *models.State, query string, opts InspectOptions) (string, error) {
	if task := state.FindTask(query); task != nil {
		return handleEntityQuery(state, "tasks", []string{query}, opts)
	}
	var selected *models.Task
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if strings.HasPrefix(query, task.ID+".") && (selected == nil || len(task.ID) > len(selected.ID)) {
			selected = task
		}
	}
	if selected == nil {
		return "", &errors.NotFoundError{Entity: "task", ID: query}
	}
	return handleTaskFieldQuery(selected, strings.TrimPrefix(query, selected.ID+"."), opts)
}

func handleTaskFieldQuery(task *models.Task, field string, opts InspectOptions) (string, error) {
	if opts.Summary || opts.OutputSummary || opts.Active || opts.Zombies || len(opts.Fields) > 0 {
		return "", &errors.ValidationError{Message: "field queries do not support --field, --summary, --output-summary, --active, or --zombies"}
	}
	value, err := taskInspectionField(task, field)
	if err != nil {
		return "", err
	}
	return formatOutput(value, opts.Format)
}

// taskInspectionField is shared by dotted queries and projections. Sanitize
// before traversal so intermediate lifecycle objects cannot bypass redaction.
func taskInspectionField(task *models.Task, field string) (any, error) {
	parts := strings.Split(field, ".")
	if slices.Contains(parts, "") || slices.Contains(parts, "generation_digest") {
		return nil, &errors.NotFoundError{Entity: "task", ID: task.ID, Field: field}
	}
	switch field {
	case "transition_id", "age", "time_in_status":
		return taskComputedField(task, field)
	}
	safeTask := redactTaskLifecycleForInspection(*task)
	value, err := resolveFieldByYAMLPath(reflect.ValueOf(safeTask), parts, "task."+task.ID)
	if err != nil || value == nil {
		return value, err
	}
	// This new query surface uses YAML field names throughout, including
	// nested values in JSON. Preserve inline fields and omission rules too.
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result any
	err = yaml.Unmarshal(data, &result)
	return result, err
}

// getComputedField calculates derived data from state
// Supports computed fields like agents.active_count, sprint.elapsed, etc.
func getComputedField(state *models.State, fieldPath string) (any, error) {
	parts := strings.Split(fieldPath, ".")
	if len(parts) < 2 {
		return nil, &errors.NotFoundError{Entity: "computed", Field: fieldPath}
	}

	entity := parts[0]
	if slices.Contains(parts, "") || ((entity == "agent" || entity == "task") && len(parts) != 3) ||
		(entity != "agent" && entity != "task" && len(parts) != 2) {
		return nil, &errors.NotFoundError{Entity: entity, Field: fieldPath}
	}

	switch entity {
	case "agents":
		return getAgentsComputedField(state, parts[1])
	case "tasks":
		return getTasksComputedField(state, parts[1])
	case "sprint":
		return getSprintComputedField(state, parts[1])
	case "agent":
		if len(parts) < 3 {
			return nil, &errors.NotFoundError{Entity: "agent", Field: "id required"}
		}
		agentID := parts[1]
		field := parts[2]
		return getAgentComputedField(state, agentID, field)
	case "task":
		if len(parts) < 3 {
			return nil, &errors.NotFoundError{Entity: "task", Field: "id required"}
		}
		taskID := parts[1]
		field := parts[2]
		return getTaskComputedField(state, taskID, field)
	default:
		return nil, &errors.NotFoundError{Entity: entity, Field: fieldPath}
	}
}

// getAgentsComputedField calculates aggregate agent metrics
func getAgentsComputedField(state *models.State, field string) (any, error) {
	switch field {
	case "active_count":
		count := 0
		for _, agent := range state.Agents {
			if agent.Status != models.AgentStatusIdle {
				count++
			}
		}
		return count, nil
	case "utilization":
		if len(state.Agents) == 0 {
			return 0.0, nil
		}
		active := 0
		for _, agent := range state.Agents {
			if agent.Status == models.AgentStatusWorking || agent.Status == models.AgentStatusReviewing {
				active++
			}
		}
		return float64(active) / float64(len(state.Agents)) * 100, nil
	default:
		return nil, &errors.NotFoundError{Entity: "agents", Field: field}
	}
}

// getTasksComputedField calculates aggregate task metrics
func getTasksComputedField(state *models.State, field string) (any, error) {
	switch field {
	case "completion_rate":
		if len(state.Tasks) == 0 {
			return 0.0, nil
		}
		done := 0
		for _, task := range state.Tasks {
			if task.Status == models.TaskStatusMerged {
				done++
			}
		}
		return float64(done) / float64(len(state.Tasks)) * 100, nil
	case "avg_iteration_count":
		if len(state.Tasks) == 0 {
			return 0.0, nil
		}
		total := 0
		for _, task := range state.Tasks {
			total += task.Iteration
		}
		return float64(total) / float64(len(state.Tasks)), nil
	default:
		return nil, &errors.NotFoundError{Entity: "tasks", Field: field}
	}
}

// getSprintComputedField calculates sprint-related computed values
func getSprintComputedField(state *models.State, field string) (any, error) {
	switch field {
	case "elapsed":
		duration := calculateSprintElapsed(&state.Sprint)
		return render.FormatDuration(duration), nil
	case "remaining":
		duration := calculateSprintRemaining(&state.Sprint)
		return render.FormatDuration(duration), nil
	case "progress_percent":
		if len(state.Tasks) == 0 {
			return 0.0, nil
		}
		done := 0
		for _, task := range state.Tasks {
			if task.Status == models.TaskStatusMerged {
				done++
			}
		}
		return float64(done) / float64(len(state.Tasks)) * 100, nil
	default:
		return nil, &errors.NotFoundError{Entity: "sprint", Field: field}
	}
}

// getAgentComputedField calculates agent-specific computed values
func getAgentComputedField(state *models.State, agentID, field string) (any, error) {
	agent, ok := state.Agents[agentID]
	if !ok {
		return nil, &errors.NotFoundError{Entity: "agent", ID: agentID}
	}

	switch field {
	case "time_since_heartbeat":
		duration := calculateTimeSinceHeartbeat(&agent)
		return render.FormatDuration(duration), nil
	case "time_on_task":
		if agent.CurrentTask == nil {
			return "0s", nil
		}
		task := state.FindTask(*agent.CurrentTask)
		if task == nil {
			return "0s", nil
		}
		duration := calculateAgentTimeOnTask(task, agentID)
		return render.FormatDuration(duration), nil
	default:
		return nil, &errors.NotFoundError{Entity: "agent", ID: agentID, Field: field}
	}
}

// getTaskComputedField calculates task-specific computed values
func getTaskComputedField(state *models.State, taskID, field string) (any, error) {
	task := state.FindTask(taskID)
	if task == nil {
		return nil, &errors.NotFoundError{Entity: "task", ID: taskID}
	}
	return taskComputedField(task, field)
}

func taskComputedField(task *models.Task, field string) (any, error) {
	switch field {
	case "transition_id":
		return models.TaskTransitionID(task), nil
	case "age":
		duration := calculateTaskAge(task)
		return render.FormatDuration(duration), nil
	case "time_in_status":
		return render.FormatDuration(models.TimeInStatus(task, time.Now())), nil
	default:
		return nil, &errors.NotFoundError{Entity: "task", ID: task.ID, Field: field}
	}
}
