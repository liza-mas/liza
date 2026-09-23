package ops

import (
	"fmt"
	"sync"

	"github.com/liza-mas/liza/internal/stacklit"
)

var (
	stacklitRuntimeRunnerMu sync.Mutex
	stacklitRuntimeRunner   stacklit.RuntimeRunner
)

func refreshTaskWorktreeStacklitIndex(worktreeDir string) []string {
	result, err := stacklit.RefreshIndex(stacklit.RefreshOptions{
		TargetRoot: worktreeDir,
		Runner:     currentStacklitRuntimeRunner(),
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

func currentStacklitRuntimeRunner() stacklit.RuntimeRunner {
	stacklitRuntimeRunnerMu.Lock()
	defer stacklitRuntimeRunnerMu.Unlock()
	return stacklitRuntimeRunner
}
