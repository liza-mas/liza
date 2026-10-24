package a9

import (
	"os/exec"

	"github.com/liza-mas/liza/internal/process"
)

// spawnAdapter is the A9 spawn-gate adapter (AC2). The gate in internal/process
// is not reachable through the binary: its only CLI callers filter provider
// signals first (R21), so the exported entry points are called directly. This is
// the only file of the Task 5 group importing a module package.
func spawnAdapter(configured bool, projectRoot, role, cli string, extraArgs ...string) (*exec.Cmd, error) {
	if configured {
		return process.SpawnConfiguredAgent(projectRoot, role, cli, extraArgs...)
	}
	return process.SpawnAgent(projectRoot, role, cli, extraArgs...)
}
