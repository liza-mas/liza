package statevalidate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

const candidateArtifactRefMissingCause = "candidate_artifact_missing"
const candidateArtifactRefInvalidModeCause = "candidate_artifact_invalid_mode"

// CandidateTreeLookup reports whether a repo-relative path exists in a
// candidate treeish and returns its raw Git object mode when present.
type CandidateTreeLookup interface {
	TreePathMode(treeish, path string) (mode string, present bool, err error)
}

// CandidateArtifactRefError carries deterministic candidate-tree diagnostics
// for protected artifact refs.
type CandidateArtifactRefError struct {
	CandidateTreeish string
	Path             string
	Value            string
	Mode             string
	Cause            string
	Owners           []ArtifactRefOwner
	Err              error
}

func (e *CandidateArtifactRefError) Error() string {
	parts := []string{"candidate artifact ref invalid"}
	if e.Path != "" {
		parts = append(parts, fmt.Sprintf("path %s", e.Path))
	}
	if e.Value != "" {
		parts = append(parts, fmt.Sprintf("value %s", e.Value))
	}
	if e.Cause != "" {
		parts = append(parts, fmt.Sprintf("cause %s", e.Cause))
	}
	if e.Mode != "" {
		parts = append(parts, fmt.Sprintf("mode %s", e.Mode))
	}
	for _, owner := range e.Owners {
		parts = append(parts, "owner "+formatCandidateArtifactOwner(owner))
	}
	message := strings.Join(parts, " ")
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return message
}

func (e *CandidateArtifactRefError) Unwrap() error {
	return e.Err
}

func (e *CandidateArtifactRefError) SafeDetails() map[string]any {
	details := map[string]any{
		"cause":  e.Cause,
		"owners": candidateOwnerDetails(e.Owners),
	}
	if e.CandidateTreeish != "" {
		details["candidate_treeish"] = e.CandidateTreeish
	}
	if e.Path != "" {
		details["path"] = e.Path
	}
	if e.Value != "" {
		details["value"] = e.Value
	}
	if e.Mode != "" {
		details["mode"] = e.Mode
	}
	return details
}

// ValidateCandidateStateArtifactRefs collects protected refs from state and
// validates them against a candidate Git treeish.
func ValidateCandidateStateArtifactRefs(candidateTreeish string, state *models.State, projectRoot string, lookup CandidateTreeLookup) error {
	refs, err := CollectArtifactRefs(state, projectRoot)
	if err != nil {
		var refErr *ArtifactRefError
		if errors.As(err, &refErr) {
			return &CandidateArtifactRefError{
				CandidateTreeish: candidateTreeish,
				Path:             refErr.Path,
				Value:            refErr.Value,
				Cause:            refErr.Cause,
				Owners: []ArtifactRefOwner{
					{
						Field:       refErr.Field,
						TaskID:      refErr.TaskID,
						OutputIndex: cloneInt(refErr.OutputIndex),
					},
				},
				Err: err,
			}
		}
		return fmt.Errorf("candidate artifact refs collection failed: %w", err)
	}
	return ValidateCandidateArtifactRefs(candidateTreeish, refs, lookup)
}

// ValidateCandidateArtifactRefs verifies collected protected refs against a
// candidate Git treeish, accepting only regular-file Git modes.
func ValidateCandidateArtifactRefs(candidateTreeish string, refs []ArtifactRef, lookup CandidateTreeLookup) error {
	if lookup == nil {
		return fmt.Errorf("candidate artifact refs lookup is nil")
	}

	for _, ref := range refs {
		mode, present, err := lookup.TreePathMode(candidateTreeish, ref.Path)
		if err != nil {
			return fmt.Errorf("candidate artifact ref lookup failed for %q at %q: %w", ref.Path, candidateTreeish, err)
		}
		if !present {
			return newCandidateArtifactRefError(candidateTreeish, ref.Path, "", candidateArtifactRefMissingCause, refs)
		}
		if mode != "100644" && mode != "100755" {
			return newCandidateArtifactRefError(candidateTreeish, ref.Path, mode, candidateArtifactRefInvalidModeCause, refs)
		}
	}
	return nil
}

func newCandidateArtifactRefError(candidateTreeish, path, mode, cause string, refs []ArtifactRef) error {
	return &CandidateArtifactRefError{
		CandidateTreeish: candidateTreeish,
		Path:             path,
		Mode:             mode,
		Cause:            cause,
		Owners:           candidateOwnersForPath(refs, path),
	}
}

func candidateOwnersForPath(refs []ArtifactRef, path string) []ArtifactRefOwner {
	owners := make([]ArtifactRefOwner, 0, 1)
	for _, ref := range refs {
		if ref.Path != path {
			continue
		}
		owners = append(owners, ArtifactRefOwner{
			Field:       ref.Owner.Field,
			TaskID:      ref.Owner.TaskID,
			OutputIndex: cloneInt(ref.Owner.OutputIndex),
		})
	}
	return owners
}

func candidateOwnerDetails(owners []ArtifactRefOwner) []map[string]any {
	details := make([]map[string]any, 0, len(owners))
	for _, owner := range owners {
		ownerDetails := map[string]any{
			"field": owner.Field,
		}
		if owner.TaskID != "" {
			ownerDetails["task_id"] = owner.TaskID
		}
		if owner.OutputIndex != nil {
			ownerDetails["output_index"] = *owner.OutputIndex
		}
		details = append(details, ownerDetails)
	}
	return details
}

func formatCandidateArtifactOwner(owner ArtifactRefOwner) string {
	parts := []string{owner.Field}
	if owner.TaskID != "" {
		parts = append(parts, "task "+owner.TaskID)
	}
	if owner.OutputIndex != nil {
		parts = append(parts, fmt.Sprintf("output: %d", *owner.OutputIndex))
	}
	return strings.Join(parts, " ")
}
