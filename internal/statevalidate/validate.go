package statevalidate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

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
		return fmt.Errorf("failed to read state file: %w", err)
	}

	// Load pipeline resolver
	var resolver *pipeline.Resolver
	cfg, cfgErr := pipeline.LoadFrozen(projectRoot)
	if cfgErr != nil {
		return fmt.Errorf("failed to load pipeline config: %w", cfgErr)
	}
	if cfg != nil {
		resolver = pipeline.NewResolver(cfg)
	}

	validators := []func(*models.State, string, bool) error{
		validateRoleNames,
		validateRequiredFields,
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateTaskStates(state, projectRoot, skipSpecFileCheck, resolver)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateTaskInvariants(state, projectRoot, skipSpecFileCheck, resolver, cfg)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateDependencies(state, projectRoot, skipSpecFileCheck, resolver, cfg)
		},
		func(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
			return validateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter)
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

// ValidateAgentInvariants exposes agent-only invariant checks for package-level tests.
func ValidateAgentInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool, warnWriter io.Writer) error {
	if warnWriter == nil {
		warnWriter = io.Discard
	}
	return validateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter)
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
	specFile := specRef
	if idx := strings.Index(specFile, "#"); idx != -1 {
		specFile = specFile[:idx]
	}
	specPath := specFile
	if !filepath.IsAbs(specPath) {
		specPath = filepath.Join(projectRoot, specFile)
	}
	if _, err := os.Stat(specPath); err == nil {
		return nil
	}
	// Fallback: file may exist on integration branch but not on the repo-root
	// filesystem (e.g. merged by a sibling worktree). Try git cat-file -e.
	// If git is not on PATH or the branch doesn't exist, this falls through
	// gracefully to the "file not found" error below.
	if integrationBranch != "" && projectRoot != "" && !filepath.IsAbs(specFile) {
		cmd := gitenv.Command("cat-file", "-e", integrationBranch+":"+specFile)
		cmd.Dir = projectRoot
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("spec_ref file not found: %s", specFile)
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
