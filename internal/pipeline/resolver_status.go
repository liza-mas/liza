package pipeline

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
)

// StateCategory identifies the lifecycle category for a declared role-pair state.
type StateCategory string

const (
	StateCategoryInitial           StateCategory = "initial"
	StateCategoryExecuting         StateCategory = "executing"
	StateCategorySubmitted         StateCategory = "submitted"
	StateCategoryReviewing         StateCategory = "reviewing"
	StateCategoryApproved          StateCategory = "approved"
	StateCategoryRejected          StateCategory = "rejected"
	StateCategoryPartiallyApproved StateCategory = "partially-approved"
	StateCategoryReviewing2        StateCategory = "reviewing-2"
	StateCategoryClean             StateCategory = "clean"
)

func (r *Resolver) lookupStates(rolePair string) (*RolePairStates, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return nil, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return &rp.States, nil
}

// InitialStatus returns the initial state for the given role-pair.
func (r *Resolver) InitialStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Initial), nil
}

// ExecutingStatus returns the executing state for the given role-pair.
func (r *Resolver) ExecutingStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Executing), nil
}

// SubmittedStatus returns the submitted state for the given role-pair.
func (r *Resolver) SubmittedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Submitted), nil
}

// ReviewingStatus returns the reviewing state for the given role-pair.
func (r *Resolver) ReviewingStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Reviewing), nil
}

// ApprovedStatus returns the approved state for the given role-pair.
func (r *Resolver) ApprovedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Approved), nil
}

// RejectedStatus returns the rejected state for the given role-pair.
func (r *Resolver) RejectedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Rejected), nil
}

// PartiallyApprovedStatus returns the partially-approved state for the given role-pair.
// Returns an error if the role-pair does not declare a partially-approved state.
func (r *Resolver) PartiallyApprovedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.PartiallyApproved == "" {
		return "", fmt.Errorf("role-pair %q has no partially-approved state declared", rolePair)
	}
	return models.TaskStatus(s.PartiallyApproved), nil
}

// Reviewing2Status returns the reviewing-2 state for the given role-pair.
// Returns an error if the role-pair does not declare a reviewing-2 state.
func (r *Resolver) Reviewing2Status(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.Reviewing2 == "" {
		return "", fmt.Errorf("role-pair %q has no reviewing-2 state declared", rolePair)
	}
	return models.TaskStatus(s.Reviewing2), nil
}

// CleanStatus returns the clean (terminal) state for the given role-pair.
// Returns an error if the role-pair does not declare a clean state.
func (r *Resolver) CleanStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.Clean == "" {
		return "", fmt.Errorf("role-pair %q has no clean state declared", rolePair)
	}
	return models.TaskStatus(s.Clean), nil
}

// TransitionMap generates the intra-pair transition map for all role-pairs.
// The fixed intra-pair flow is: initial→executing→submitted→reviewing→approved|rejected,
// with rejected→initial. When quorum states are declared, reviewing also transitions to
// partially-approved, partially-approved→reviewing-2, and reviewing-2→approved|rejected.
// Cross-cutting meta-states (BLOCKED, ABANDONED, etc.) are not included.
func (r *Resolver) TransitionMap() map[models.TaskStatus][]models.TaskStatus {
	tm := make(map[models.TaskStatus][]models.TaskStatus)
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		initial := models.TaskStatus(s.Initial)
		executing := models.TaskStatus(s.Executing)
		submitted := models.TaskStatus(s.Submitted)
		reviewing := models.TaskStatus(s.Reviewing)
		approved := models.TaskStatus(s.Approved)
		rejected := models.TaskStatus(s.Rejected)

		tm[initial] = []models.TaskStatus{executing}
		tm[executing] = []models.TaskStatus{submitted}
		tm[submitted] = []models.TaskStatus{reviewing}
		tm[reviewing] = []models.TaskStatus{approved, rejected}
		tm[rejected] = []models.TaskStatus{initial}
		tm[approved] = []models.TaskStatus{} // terminal within the pair

		if s.Clean != "" {
			clean := models.TaskStatus(s.Clean)
			tm[clean] = []models.TaskStatus{} // terminal within the pair
		}

		// Quorum state transitions when partially-approved and reviewing-2 are declared.
		if s.PartiallyApproved != "" && s.Reviewing2 != "" {
			partiallyApproved := models.TaskStatus(s.PartiallyApproved)
			reviewing2 := models.TaskStatus(s.Reviewing2)

			tm[reviewing] = append(tm[reviewing], partiallyApproved)
			tm[partiallyApproved] = []models.TaskStatus{reviewing2}
			tm[reviewing2] = []models.TaskStatus{approved, rejected}
		}
	}
	return tm
}

// AllDeclaredStates returns all state names declared across all role-pairs.
func (r *Resolver) AllDeclaredStates() []models.TaskStatus {
	var states []models.TaskStatus
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		states = append(states,
			models.TaskStatus(s.Initial),
			models.TaskStatus(s.Executing),
			models.TaskStatus(s.Submitted),
			models.TaskStatus(s.Reviewing),
			models.TaskStatus(s.Approved),
			models.TaskStatus(s.Rejected),
		)
		if s.PartiallyApproved != "" {
			states = append(states, models.TaskStatus(s.PartiallyApproved))
		}
		if s.Reviewing2 != "" {
			states = append(states, models.TaskStatus(s.Reviewing2))
		}
		if s.Clean != "" {
			states = append(states, models.TaskStatus(s.Clean))
		}
	}
	return states
}

// StateCategories returns all declared role-pair states keyed by lifecycle category.
func (r *Resolver) StateCategories() map[models.TaskStatus]StateCategory {
	categories := make(map[models.TaskStatus]StateCategory)
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		categories[models.TaskStatus(s.Initial)] = StateCategoryInitial
		categories[models.TaskStatus(s.Executing)] = StateCategoryExecuting
		categories[models.TaskStatus(s.Submitted)] = StateCategorySubmitted
		categories[models.TaskStatus(s.Reviewing)] = StateCategoryReviewing
		categories[models.TaskStatus(s.Approved)] = StateCategoryApproved
		categories[models.TaskStatus(s.Rejected)] = StateCategoryRejected
		if s.PartiallyApproved != "" {
			categories[models.TaskStatus(s.PartiallyApproved)] = StateCategoryPartiallyApproved
		}
		if s.Reviewing2 != "" {
			categories[models.TaskStatus(s.Reviewing2)] = StateCategoryReviewing2
		}
		if s.Clean != "" {
			categories[models.TaskStatus(s.Clean)] = StateCategoryClean
		}
	}
	return categories
}

// SprintTerminalStates returns the states at which a task is considered complete
// for sprint purposes. For role-pairs whose approved state is the source of a
// transition (i.e., they feed into the next pipeline stage), the approved state
// is sprint-terminal. MERGED is always included as the universal terminal for
// role-pairs at the end of the pipeline.
func (r *Resolver) SprintTerminalStates() []models.TaskStatus {
	sources := r.TransitionSourcePairs()

	var states []models.TaskStatus
	for name, rp := range r.config.Pipeline.RolePairs {
		if sources[name] {
			states = append(states, models.TaskStatus(rp.States.Approved))
		}
		if rp.States.Clean != "" {
			states = append(states, models.TaskStatus(rp.States.Clean))
		}
	}

	states = append(states, models.TaskStatusMerged)

	slices.Sort(states)
	return states
}

// resolvePhase returns the concrete status for a role-pair + phase combination.
func (r *Resolver) resolvePhase(rolePair, phase string) models.TaskStatus {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return ""
	}
	switch StateCategory(phase) {
	case StateCategoryInitial:
		return models.TaskStatus(rp.States.Initial)
	case StateCategoryExecuting:
		return models.TaskStatus(rp.States.Executing)
	case StateCategorySubmitted:
		return models.TaskStatus(rp.States.Submitted)
	case StateCategoryReviewing:
		return models.TaskStatus(rp.States.Reviewing)
	case StateCategoryApproved:
		return models.TaskStatus(rp.States.Approved)
	case StateCategoryRejected:
		return models.TaskStatus(rp.States.Rejected)
	case StateCategoryPartiallyApproved:
		return models.TaskStatus(rp.States.PartiallyApproved)
	case StateCategoryReviewing2:
		return models.TaskStatus(rp.States.Reviewing2)
	case StateCategoryClean:
		return models.TaskStatus(rp.States.Clean)
	default:
		return ""
	}
}

// resolve3PartPhase parses a 3-part ref (sub-pipeline.role-pair.phase) and
// returns the concrete status. The sub-pipeline prefix is stripped since
// role-pair names are globally unique.
func (r *Resolver) resolve3PartPhase(ref string) models.TaskStatus {
	_, rolePair, phase, err := parse3PartRef(ref)
	if err != nil {
		return ""
	}
	return r.resolvePhase(rolePair, phase)
}

// IsDeclaredState checks if a status is declared in the pipeline config.
func (r *Resolver) IsDeclaredState(status models.TaskStatus) bool {
	return slices.Contains(r.AllDeclaredStates(), status)
}
