package commands

import (
	"io"
	"os"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// warnWriter is the destination for non-fatal validation warnings.
// Defaults to os.Stderr; tests override it to capture output without
// monkey-patching the global stderr (which is not goroutine-safe).
var warnWriter io.Writer = os.Stderr

// SetWarnWriter sets the destination for non-fatal validation warnings.
func SetWarnWriter(w io.Writer) {
	warnWriter = w
}

// ValidateCommand validates the state.yaml file against all schema rules.
// Returns an error with detailed description if validation fails.
func ValidateCommand(statePath string, skipSpecFileCheck bool) error {
	return statevalidate.ValidateStateFile(statePath, skipSpecFileCheck, warnWriter)
}

func validateAgentInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	return statevalidate.ValidateAgentInvariants(state, projectRoot, skipSpecFileCheck, warnWriter)
}

func validateAnomalies(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	return statevalidate.ValidateAnomalies(state, projectRoot, skipSpecFileCheck)
}
