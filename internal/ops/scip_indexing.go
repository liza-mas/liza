package ops

import (
	"fmt"
	"sync"

	"github.com/liza-mas/liza/internal/scipsearch"
)

var (
	scipRuntimeRunnerMu sync.Mutex
	scipRuntimeRunner   scipsearch.RuntimeRunner
)

func refreshTaskWorktreeScipIndexes(worktreeDir string, configuredLanguages []string) []string {
	result, err := scipsearch.RefreshIndexes(scipsearch.RefreshOptions{
		TargetRoot:          worktreeDir,
		TargetKind:          scipsearch.TargetKindTaskWorktree,
		ConfiguredLanguages: configuredLanguages,
		Runner:              currentScipRuntimeRunner(),
	})
	if err != nil {
		return []string{fmt.Sprintf("scip-search: %v", err)}
	}

	warnings := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		warnings = append(warnings, fmt.Sprintf("scip-search %s: %s", failure.Language, failure.Diagnostic))
	}
	return warnings
}

func currentScipRuntimeRunner() scipsearch.RuntimeRunner {
	scipRuntimeRunnerMu.Lock()
	defer scipRuntimeRunnerMu.Unlock()
	return scipRuntimeRunner
}
