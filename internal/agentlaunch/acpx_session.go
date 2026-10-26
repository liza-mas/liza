package agentlaunch

import "github.com/liza-mas/liza/internal/brand"

// ACPXSessionName is the default ACPX session name of an agent: the configured
// binary name followed by the agent ID.
func ACPXSessionName(agentID string) string {
	binaryName := brand.RuntimeValues().BinaryName
	if agentID == "" {
		return binaryName + "-agent"
	}
	return binaryName + "-" + agentID
}
