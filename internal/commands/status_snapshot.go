package commands

import (
	"fmt"
	"log"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
)

// statusSnapshot is the one read of state and pipeline policy that every
// status section is assembled from.
type statusSnapshot struct {
	State    *models.State
	Resolver models.PipelineResolver
}

// readStatusSnapshot reads state and loads the pipeline resolver. A resolver
// that fails to load is logged and left to the sections to tolerate.
func readStatusSnapshot(projectRoot string) (statusSnapshot, error) {
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return statusSnapshot{}, fmt.Errorf("failed to read state: %w", err)
	}

	pr, prErr := ops.LoadResolverForModels(projectRoot)
	if prErr != nil {
		log.Printf("WARNING: status: failed to load pipeline resolver: %v", prErr)
	}
	return statusSnapshot{State: state, Resolver: pr}, nil
}
