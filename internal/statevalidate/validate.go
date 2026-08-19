package statevalidate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statehygiene"
)

const artifactRefMultipleRefsCause = "multiple_refs_not_supported"
const artifactRefNotFoundCause = "file_not_found"
const artifactRefInvalidModeCause = "invalid_artifact_mode"
const artifactRefEmptyPathCause = "empty_ref_path"
const artifactRefPathTraversalCause = "path_traversal_outside_repo"
const artifactRefAbsoluteOutsideRepoCause = "absolute_path_outside_repo"
const artifactRefInvalidPathSyntaxCause = "invalid_path_syntax"

// ArtifactRefError carries safe diagnostics for invalid artifact references.
type ArtifactRefError struct {
	Field       string
	Value       string
	Path        string
	Mode        string
	TaskID      string
	OutputIndex *int
	Cause       string
}

func (e *ArtifactRefError) Error() string {
	field := e.Field
	if field == "" {
		field = "spec_ref"
	}
	switch e.Cause {
	case artifactRefMultipleRefsCause:
		return formatArtifactRefError(field, "contains multiple refs; use one repo-relative ref", e.Value, e.TaskID, e.OutputIndex)
	case artifactRefEmptyPathCause:
		return formatArtifactRefError(field, "has empty path after fragment stripping", e.Value, e.TaskID, e.OutputIndex)
	case artifactRefPathTraversalCause:
		return formatArtifactRefError(field, "points outside repository", e.Value, e.TaskID, e.OutputIndex)
	case artifactRefAbsoluteOutsideRepoCause:
		return formatArtifactRefError(field, "absolute path outside repository", e.Value, e.TaskID, e.OutputIndex)
	case artifactRefInvalidPathSyntaxCause:
		return formatArtifactRefError(field, "has invalid path syntax; use a clean path and put annotations outside artifact-ref fields", e.Value, e.TaskID, e.OutputIndex)
	case artifactRefInvalidModeCause:
		value := e.Value
		if e.Path != "" {
			value = e.Path
		}
		reason := "is not a regular file"
		if e.Mode != "" {
			reason += fmt.Sprintf(" (mode %s)", e.Mode)
		}
		return formatArtifactRefError(field, reason, value, e.TaskID, e.OutputIndex)
	default:
		value := e.Value
		if e.Path != "" {
			value = e.Path
		}
		return formatArtifactRefError(field, "file not found", value, e.TaskID, e.OutputIndex)
	}
}

func (e *ArtifactRefError) SafeDetails() map[string]any {
	details := map[string]any{
		"field": e.Field,
		"value": e.Value,
		"cause": e.Cause,
	}
	if e.TaskID != "" {
		details["task_id"] = e.TaskID
	}
	if e.Path != "" {
		details["path"] = e.Path
	}
	if e.Mode != "" {
		details["mode"] = e.Mode
	}
	if e.OutputIndex != nil {
		details["output_index"] = *e.OutputIndex
	}
	return details
}

func formatArtifactRefError(field, reason, value, taskID string, outputIndex *int) string {
	suffix := ""
	if taskID != "" {
		suffix = fmt.Sprintf(" (task: %s", taskID)
	}
	if outputIndex != nil {
		if suffix == "" {
			suffix = " ("
		} else {
			suffix += ", "
		}
		suffix += fmt.Sprintf("output: %d", *outputIndex)
	}
	if suffix != "" {
		suffix += ")"
	}
	return fmt.Sprintf("%s %s: %s%s", field, reason, value, suffix)
}

// ValidateArtifactRefScalar rejects delimiter-joined refs. Artifact ref fields
// are scalar repo-relative refs; multi-reference formats must be explicit data.
func ValidateArtifactRefScalar(field, value, taskID string) error {
	if value == "" {
		return nil
	}
	if cause := validateArtifactRefSyntax(value); cause != "" {
		return &ArtifactRefError{
			Field:  field,
			Value:  value,
			TaskID: taskID,
			Cause:  cause,
		}
	}
	return nil
}

func validateArtifactRefSyntax(value string) string {
	if strings.Contains(value, ";") {
		return artifactRefMultipleRefsCause
	}
	refFile := paths.SplitRefFile(value)
	if refFile == "" {
		return artifactRefEmptyPathCause
	}
	ext := filepath.Ext(filepath.Base(refFile))
	if ext == "" {
		return ""
	}
	if strings.ContainsAny(ext, "()[]{} ,") || strings.IndexFunc(ext, unicode.IsSpace) >= 0 {
		return artifactRefInvalidPathSyntaxCause
	}
	return ""
}

// ValidateStateFile validates the state.yaml file against all schema rules.
// It orchestrates the full validation sequence: required fields, task states,
// task invariants, dependencies, agent invariants, discovered items, anomalies,
// and sprint configuration. Returns an error with a detailed
// description if any validation rule fails.
func ValidateStateFile(statePath string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	if warnWriter == nil {
		warnWriter = io.Discard
	}

	lizaDir := filepath.Dir(statePath)
	projectRoot := filepath.Dir(lizaDir)

	bb := db.For(statePath)
	state, err := bb.Read()
	if err != nil {
		return &lizaerrors.StateSchemaError{Operation: "validate", Err: err}
	}

	return ValidateState(state, projectRoot, skipSpecFileCheck, warnWriter)
}

// ValidateState validates an in-memory state using the same rules as
// ValidateStateFile. Callers use this before persisting candidate mutations.
func ValidateState(state *models.State, projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	if warnWriter == nil {
		warnWriter = io.Discard
	}
	if err := statehygiene.ValidateState(state); err != nil {
		return err
	}

	// Load pipeline resolver
	var resolver *pipeline.Resolver
	cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
	if cfgErr != nil {
		return &lizaerrors.PipelineConfigError{Operation: "validate", Err: cfgErr}
	}
	if cfg != nil {
		resolver = pipeline.NewResolver(cfg)
	}

	validators := []func(*models.State, string, bool) error{
		validateRoleNames,
		validateRequiredFields,
		validateUniqueTaskIDs,
		validateIntegrationLifecycle,
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateTaskStates(state, projectRoot, skipSpecFileCheck, resolver)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateTaskInvariants(state, projectRoot, skipSpecFileCheck, resolver, cfg)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateDependencies(state, projectRoot, skipSpecFileCheck, resolver, cfg, warnWriter)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			warnBlockedReasonMissingDependsOn(state, warnWriter)
			return nil
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter, resolver)
		},
		validateDiscovered,
		validateAnomalies,
		validateHandoffEvents,
		validateSprint,
	}

	for _, validator := range validators {
		if err := validator(state, projectRoot, skipSpecFileCheck); err != nil {
			return err
		}
	}

	return nil
}

func validateUniqueTaskIDs(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	firstIndexByID := make(map[string]int, len(state.Tasks))
	for i, task := range state.Tasks {
		if task.ID == "" {
			continue
		}
		firstIndex, exists := firstIndexByID[task.ID]
		if exists {
			return fmt.Errorf("duplicate task ID %q at tasks[%d] and tasks[%d]", task.ID, firstIndex, i)
		}
		firstIndexByID[task.ID] = i
	}
	return nil
}

// ValidateAgentInvariants exposes agent-only invariant checks for package-level tests.
func ValidateAgentInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	if warnWriter == nil {
		warnWriter = io.Discard
	}
	return validateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter, nil)
}

// ValidateAnomalies exposes anomaly validation for package-level tests.
func ValidateAnomalies(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	return validateAnomalies(state, projectRoot, skipSpecFileCheck)
}

// checkSpecFileExists verifies that a spec_ref points to an existing file on
// disk. Strips any fragment identifier (#section) before checking. Used by
// both required-fields and task-invariants validation to ensure specs are
// reachable.
func checkSpecFileExists(projectRoot, specRef, integrationBranch string) error {
	return checkArtifactRefFileExists(projectRoot, "spec_ref", specRef, integrationBranch, "")
}

// ValidateArtifactRefs verifies that every active artifact reference stored in state
// points to a reachable repo file in the project working tree or integration
// branch. It intentionally does not enforce lifecycle/audit invariants; merge
// gates use it after a candidate integration update to catch commits that delete
// durable artifacts still referenced by the blackboard.
func ValidateArtifactRefs(state *models.State, projectRoot string) error {
	refs, err := CollectArtifactRefs(state, projectRoot)
	return validateCollectedArtifactRefs(state, projectRoot, refs, err)
}

// ValidateMergeArtifactRefs validates refs protected by a single task merge.
// Output refs from unrelated in-flight tasks are ignored; they are not durable
// integration artifacts until those tasks merge.
func ValidateMergeArtifactRefs(state *models.State, projectRoot, mergingTaskID string) error {
	refs, err := CollectMergeArtifactRefs(state, projectRoot, mergingTaskID)
	return validateCollectedArtifactRefs(state, projectRoot, refs, err)
}

func validateCollectedArtifactRefs(state *models.State, projectRoot string, refs []ArtifactRef, err error) error {
	if err != nil {
		return err
	}
	integrationBranch := state.Config.IntegrationBranch
	for _, ref := range refs {
		if err := checkCollectedArtifactRefFileExists(projectRoot, ref, integrationBranch); err != nil {
			return err
		}
	}
	return nil
}

func checkCollectedArtifactRefFileExists(projectRoot string, ref ArtifactRef, integrationBranch string) error {
	if exists, invalidMode := artifactRefFileExists(projectRoot, ref.Path, integrationBranch); exists {
		return nil
	} else if invalidMode != "" {
		return &ArtifactRefError{
			Field:       ref.Owner.Field,
			Value:       ref.Raw,
			Path:        ref.Path,
			Mode:        invalidMode,
			TaskID:      ref.Owner.TaskID,
			OutputIndex: cloneInt(ref.Owner.OutputIndex),
			Cause:       artifactRefInvalidModeCause,
		}
	}
	return &ArtifactRefError{
		Field:       ref.Owner.Field,
		Value:       ref.Raw,
		Path:        ref.Path,
		TaskID:      ref.Owner.TaskID,
		OutputIndex: cloneInt(ref.Owner.OutputIndex),
		Cause:       artifactRefNotFoundCause,
	}
}

func artifactRefsRetired(task models.Task) bool {
	return task.Status == models.TaskStatusSuperseded || task.Status == models.TaskStatusAbandoned
}

func checkArtifactRefFileExists(projectRoot, field, ref, integrationBranch, taskID string) error {
	if err := ValidateArtifactRefScalar(field, ref, taskID); err != nil {
		return err
	}
	refFile := ref
	if idx := strings.Index(refFile, "#"); idx != -1 {
		refFile = refFile[:idx]
	}
	refPath := refFile
	if !filepath.IsAbs(refPath) {
		refPath = filepath.Join(projectRoot, refFile)
	}
	if exists, invalidMode := artifactRefWorkingTreeFileExists(refPath); exists {
		return nil
	} else if invalidMode != "" {
		return &ArtifactRefError{
			Field:  field,
			Value:  ref,
			Path:   refFile,
			Mode:   invalidMode,
			TaskID: taskID,
			Cause:  artifactRefInvalidModeCause,
		}
	}
	if integrationBranch != "" && projectRoot != "" && !filepath.IsAbs(refFile) {
		if exists, invalidMode := artifactRefIntegrationFileExists(projectRoot, integrationBranch, refFile); exists {
			return nil
		} else if invalidMode != "" {
			return &ArtifactRefError{
				Field:  field,
				Value:  ref,
				Path:   refFile,
				Mode:   invalidMode,
				TaskID: taskID,
				Cause:  artifactRefInvalidModeCause,
			}
		}
	}
	return &ArtifactRefError{
		Field:  field,
		Value:  ref,
		Path:   refFile,
		TaskID: taskID,
		Cause:  artifactRefNotFoundCause,
	}
}

func artifactRefFileExists(projectRoot, repoRelativePath, integrationBranch string) (exists bool, invalidMode string) {
	var workingTreeMode string
	if projectRoot != "" {
		refPath := filepath.Join(projectRoot, filepath.FromSlash(repoRelativePath))
		if exists, invalidMode := artifactRefWorkingTreeFileExists(refPath); exists {
			return true, ""
		} else if invalidMode != "" {
			workingTreeMode = invalidMode
		}
	}
	if integrationBranch != "" && projectRoot != "" {
		if exists, invalidMode := artifactRefIntegrationFileExists(projectRoot, integrationBranch, repoRelativePath); exists {
			return true, ""
		} else if invalidMode != "" {
			return false, invalidMode
		}
	}
	return false, workingTreeMode
}

func artifactRefWorkingTreeFileExists(path string) (exists bool, invalidMode string) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode().IsRegular() {
			return true, ""
		}
		return false, info.Mode().String()
	}
	return false, ""
}

func artifactRefIntegrationFileExists(projectRoot, integrationBranch, repoRelativePath string) (exists bool, invalidMode string) {
	mode, present, err := git.New(projectRoot).TreePathMode(integrationBranch, repoRelativePath)
	if err != nil || !present {
		return false, ""
	}
	if isRegularArtifactGitMode(mode) {
		return true, ""
	}
	return false, mode
}

// buildTaskIDSet creates a lookup set of all task IDs for O(1) existence
// checks during referential integrity validation (dependencies, parent_task,
// sprint scope).
func buildTaskIDSet(tasks []models.Task) map[string]bool {
	ids := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		ids[task.ID] = true
	}
	return ids
}
