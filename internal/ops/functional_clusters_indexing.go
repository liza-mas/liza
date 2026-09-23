package ops

import (
	"fmt"
	"sync"

	"github.com/liza-mas/liza/internal/functionalclusters"
)

var (
	functionalClustersRuntimeRunnerMu sync.Mutex
	functionalClustersRuntimeRunner   functionalclusters.RuntimeRunner
)

func refreshTaskWorktreeFunctionalClustersIndex(worktreeDir string, configuredLanguages []string) []string {
	result, err := functionalclusters.RefreshIndex(functionalclusters.RefreshOptions{
		TargetRoot:          worktreeDir,
		ConfiguredLanguages: configuredLanguages,
		Runner:              currentFunctionalClustersRuntimeRunner(),
	})
	warnings := functionalClustersRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("functional-clusters: %v", err))
	}
	return warnings
}

func functionalClustersRefreshWarnings(result functionalclusters.RefreshResult) []string {
	warnings := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		warnings = append(warnings, fmt.Sprintf("functional-clusters: %s", failure.Diagnostic))
	}
	return warnings
}

func currentFunctionalClustersRuntimeRunner() functionalclusters.RuntimeRunner {
	functionalClustersRuntimeRunnerMu.Lock()
	defer functionalClustersRuntimeRunnerMu.Unlock()
	return functionalClustersRuntimeRunner
}
