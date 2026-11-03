package db

import (
	"github.com/liza-mas/liza/internal/deepcopy"
	"github.com/liza-mas/liza/internal/models"
)

// CloneState returns a deep copy of state. Operations use it to build a
// candidate they adopt only after validating it whole.
//
// The parsed-state cache hands the same *models.State to every ReadCached
// caller, and ReadCached promises a result the caller may mutate freely.
// Copying the parsed structure is an order of magnitude cheaper than
// re-parsing the YAML, which is what the cache exists to avoid.
// TestStateModelShapeIsCloneable guards the model against shallow aliasing.
func CloneState(state *models.State) *models.State {
	return deepcopy.Of(state)
}
