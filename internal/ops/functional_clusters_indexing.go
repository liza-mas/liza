package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/functionalclusters"
)

func refreshTaskWorktreeFunctionalClustersIndex(projectRoot, worktreeDir string, configuredLanguages []string) []string {
	result, err := functionalclusters.RefreshIndex(functionalclusters.RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          worktreeDir,
		ConfiguredLanguages: configuredLanguages,
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
