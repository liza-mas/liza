package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/stacklit"
)

func refreshTaskWorktreeStacklitIndex(projectRoot, worktreeDir string) []string {
	result, err := stacklit.RefreshIndex(stacklit.RefreshOptions{
		ProjectRoot: projectRoot,
		TargetRoot:  worktreeDir,
	})
	warnings := stacklitRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("stacklit: %v", err))
	}
	return warnings
}

func stacklitRefreshWarnings(result stacklit.RefreshResult) []string {
	warnings := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		warnings = append(warnings, fmt.Sprintf("stacklit: %s", failure.Diagnostic))
	}
	return warnings
}
