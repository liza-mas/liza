package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/worktreeexclude"
)

// Permission bits of the directories and files MAS init creates.
const (
	initDirMode  os.FileMode = 0755
	initFileMode os.FileMode = 0644
)

// cleanPriorProject asks to remove a previous runtime directory, then prewarms
// the Semble index.
func (m *masInit) cleanPriorProject() error {
	if _, err := CleanupProjectCommand(CleanupParams{
		ProjectRoot: m.lizaPaths.ProjectRoot(),
		Stdin:       m.stdin,
		Stderr:      os.Stderr,
		AutoConfirm: m.params.AutoConfirm,
	}); err != nil {
		if errors.Is(err, ErrProjectCleanupDeclined) {
			return fmt.Errorf("initialization cancelled by user")
		}
		return err
	}
	runSembleInitPrewarm(m.lizaPaths.ProjectRoot())
	return nil
}

// createLayout creates the runtime directory and its archive directory. A
// failure here leaves nothing to clean up that init did not create itself.
func (m *masInit) createLayout() error {
	if err := os.MkdirAll(m.lizaPaths.LizaDir(), initDirMode); err != nil {
		return fmt.Errorf("failed to create %s directory: %w", paths.ProjectDirName(), err)
	}
	// Keep runtime writes out of git status (best effort: a missing exclude only
	// adds noise and risks accidental staging).
	if err := worktreeexclude.EnsureRepoExclude(m.lizaPaths.ProjectRoot(), "/"+paths.ProjectDirName()+"/"); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to exclude %s/ from git status: %v\n", paths.ProjectDirName(), err)
	}

	archiveDir := m.lizaPaths.ArchiveDir()
	if err := os.Mkdir(archiveDir, initDirMode); err != nil {
		return fmt.Errorf("failed to create archive directory: %w", err)
	}
	return nil
}

// cleanupInit removes the runtime directory of a run that failed after
// createLayout.
func (m *masInit) cleanupInit() {
	os.RemoveAll(m.lizaPaths.LizaDir())
}

// writeRuntimeFiles writes the support doc, the frozen pipeline and the models
// file into the runtime directory.
func (m *masInit) writeRuntimeFiles() error {
	// Write support doc to the project runtime directory (non-fatal).
	if err := embedded.WriteSupportDoc(m.lizaPaths.LizaDir()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write SUPPORT.md: %v\n", err)
	}

	// Freeze pipeline config into the project runtime directory.
	frozenPath := filepath.Join(m.lizaPaths.LizaDir(), "pipeline.yaml")
	if err := os.WriteFile(frozenPath, m.pipelineData, initFileMode); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to freeze pipeline config: %w", err)
	}

	if err := os.WriteFile(m.lizaPaths.ModelsPath(), initModelsFile(m.params), initFileMode); err != nil {
		m.cleanupInit()
		return fmt.Errorf("failed to write %s: %w", paths.ModelsFileName, err)
	}
	return nil
}
