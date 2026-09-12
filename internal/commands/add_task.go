package commands

import (
	"fmt"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"gopkg.in/yaml.v3"
)

// TaskInput represents the input parameters for adding a task.
// Can be loaded from a YAML file or constructed from CLI flags.
type TaskInput struct {
	ID                      string                          `yaml:"id"`
	Type                    string                          `yaml:"type,omitempty"`
	RolePair                string                          `yaml:"role_pair,omitempty"`
	Description             string                          `yaml:"description"`
	SpecRef                 string                          `yaml:"spec_ref"`
	DoneWhen                string                          `yaml:"done_when"`
	Validation              []string                        `yaml:"validation,omitempty"`
	ValidationPrerequisites []models.ValidationPrerequisite `yaml:"validation_prerequisites,omitempty"`
	DestructiveDB           bool                            `yaml:"destructive_db,omitempty"`
	Scope                   string                          `yaml:"scope"`
	Priority                int                             `yaml:"priority"`
	DependsOn               []string                        `yaml:"depends_on,omitempty"`
}

// LoadTaskInputFromFile loads task input from a YAML file.
func LoadTaskInputFromFile(path string) (*TaskInput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read task file: %w", err)
	}

	var input TaskInput
	if err := yaml.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("failed to parse task file: %w", err)
	}

	return &input, nil
}

// AddTaskCommand adds a new task.
// Delegates business logic (including post-write validation) to ops.AddTask.
func AddTaskCommand(statePath, logPath string, input *TaskInput, orchestratorID string) error {
	return addTaskCommand(statePath, logPath, input, func(opsInput *ops.AddTaskInput) (*ops.AddTaskResult, error) {
		return ops.AddTask(statePath, logPath, opsInput, orchestratorID)
	})
}

// AddTaskWithAuthorityCommand adds a task using generation-fenced authority.
func AddTaskWithAuthorityCommand(statePath, logPath string, input *TaskInput, authority models.AgentAuthority) error {
	return addTaskCommand(statePath, logPath, input, func(opsInput *ops.AddTaskInput) (*ops.AddTaskResult, error) {
		return ops.AddTaskWithAuthority(statePath, logPath, opsInput, authority)
	})
}

func addTaskCommand(statePath, logPath string, input *TaskInput, add func(*ops.AddTaskInput) (*ops.AddTaskResult, error)) error {
	opsInput := &ops.AddTaskInput{
		ID:                      input.ID,
		Type:                    input.Type,
		RolePair:                input.RolePair,
		Description:             input.Description,
		SpecRef:                 input.SpecRef,
		DoneWhen:                input.DoneWhen,
		Validation:              input.Validation,
		ValidationPrerequisites: models.CloneValidationPrerequisites(input.ValidationPrerequisites),
		DestructiveDB:           input.DestructiveDB,
		Scope:                   input.Scope,
		Priority:                input.Priority,
		DependsOn:               input.DependsOn,
	}

	result, err := add(opsInput)
	if err != nil {
		return fmt.Errorf("add task: %w", err)
	}
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}

	return nil
}
