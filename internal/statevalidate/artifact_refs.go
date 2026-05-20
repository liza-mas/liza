package statevalidate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// ArtifactRef is a normalized, repo-relative artifact path with its state owner.
type ArtifactRef struct {
	Path  string
	Raw   string
	Owner ArtifactRefOwner
}

// ArtifactRefOwner identifies the state field that owns an artifact reference.
type ArtifactRefOwner struct {
	Field       string
	TaskID      string
	OutputIndex *int
}

// CollectArtifactRefs returns every protected artifact ref in deterministic
// order, normalized to repo-relative paths with fragments stripped.
func CollectArtifactRefs(state *models.State, projectRoot string) ([]ArtifactRef, error) {
	if state == nil {
		return nil, fmt.Errorf("state is nil")
	}

	collector := artifactRefCollector{projectRoot: projectRoot}
	if err := collector.add("goal.spec_ref", state.Goal.SpecRef, "", nil); err != nil {
		return nil, err
	}
	for _, task := range state.Tasks {
		if err := collector.add("spec_ref", task.SpecRef, task.ID, nil); err != nil {
			return nil, err
		}
		if err := collector.add("epic_ref", task.EpicRef, task.ID, nil); err != nil {
			return nil, err
		}
		if err := collector.add("plan_ref", task.PlanRef, task.ID, nil); err != nil {
			return nil, err
		}
		if err := collector.add("arch_ref", task.ArchRef, task.ID, nil); err != nil {
			return nil, err
		}
		for i, entry := range task.Output {
			outputIndex := i
			outputRefs := []struct {
				field string
				value string
			}{
				{field: "spec_ref", value: entry.SpecRef},
				{field: "epic_ref", value: entry.EpicRef},
				{field: "plan_ref", value: entry.PlanRef},
				{field: "arch_ref", value: entry.ArchRef},
			}
			for _, ref := range outputRefs {
				field := fmt.Sprintf("output[%d].%s", i, ref.field)
				if err := collector.add(field, ref.value, task.ID, &outputIndex); err != nil {
					return nil, err
				}
			}
		}
	}

	sort.Slice(collector.refs, func(i, j int) bool {
		return artifactRefLess(collector.refs[i], collector.refs[j])
	})
	return collector.refs, nil
}

type artifactRefCollector struct {
	projectRoot string
	refs        []ArtifactRef
}

func (c *artifactRefCollector) add(field, raw, taskID string, outputIndex *int) error {
	if raw == "" {
		return nil
	}
	owner := ArtifactRefOwner{
		Field:       field,
		TaskID:      taskID,
		OutputIndex: cloneInt(outputIndex),
	}
	path, err := normalizeArtifactRefPath(raw, c.projectRoot)
	if err != nil {
		return &ArtifactRefError{
			Field:       owner.Field,
			Value:       raw,
			TaskID:      owner.TaskID,
			OutputIndex: cloneInt(owner.OutputIndex),
			Cause:       err.cause,
		}
	}
	c.refs = append(c.refs, ArtifactRef{
		Path:  path,
		Raw:   raw,
		Owner: owner,
	})
	return nil
}

type artifactRefNormalizeError struct {
	cause string
}

func normalizeArtifactRefPath(raw, projectRoot string) (string, *artifactRefNormalizeError) {
	if strings.Contains(raw, ";") {
		return "", &artifactRefNormalizeError{cause: artifactRefMultipleRefsCause}
	}

	refFile := paths.SplitRefFile(raw)
	if refFile == "" {
		return "", &artifactRefNormalizeError{cause: artifactRefEmptyPathCause}
	}
	if isAbsAnyPlatform(refFile) {
		return normalizeAbsoluteArtifactRefPath(refFile, projectRoot)
	}
	return normalizeRelativeArtifactRefPath(refFile)
}

func normalizeRelativeArtifactRefPath(refFile string) (string, *artifactRefNormalizeError) {
	cleaned := filepath.Clean(refFile)
	if cleaned == "." || cleaned == "" {
		return "", &artifactRefNormalizeError{cause: artifactRefEmptyPathCause}
	}
	if pathEscapesRepo(cleaned) {
		return "", &artifactRefNormalizeError{cause: artifactRefPathTraversalCause}
	}
	return filepath.ToSlash(cleaned), nil
}

func normalizeAbsoluteArtifactRefPath(refFile, projectRoot string) (string, *artifactRefNormalizeError) {
	if !filepath.IsAbs(refFile) {
		return "", &artifactRefNormalizeError{cause: artifactRefAbsoluteOutsideRepoCause}
	}
	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", &artifactRefNormalizeError{cause: artifactRefAbsoluteOutsideRepoCause}
	}
	absRef, err := filepath.Abs(refFile)
	if err != nil {
		return "", &artifactRefNormalizeError{cause: artifactRefAbsoluteOutsideRepoCause}
	}
	rel, err := filepath.Rel(absRoot, absRef)
	if err != nil {
		return "", &artifactRefNormalizeError{cause: artifactRefAbsoluteOutsideRepoCause}
	}
	if rel == "." || rel == "" {
		return "", &artifactRefNormalizeError{cause: artifactRefEmptyPathCause}
	}
	if pathEscapesRepo(rel) {
		return "", &artifactRefNormalizeError{cause: artifactRefAbsoluteOutsideRepoCause}
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func pathEscapesRepo(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || strings.HasPrefix(filepath.ToSlash(path), "../")
}

func artifactRefLess(left, right ArtifactRef) bool {
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	if left.Owner.TaskID != right.Owner.TaskID {
		return left.Owner.TaskID < right.Owner.TaskID
	}
	leftIndex, leftHasIndex := outputIndexSortValue(left.Owner.OutputIndex)
	rightIndex, rightHasIndex := outputIndexSortValue(right.Owner.OutputIndex)
	if leftHasIndex != rightHasIndex {
		return !leftHasIndex
	}
	if leftIndex != rightIndex {
		return leftIndex < rightIndex
	}
	if left.Owner.Field != right.Owner.Field {
		return left.Owner.Field < right.Owner.Field
	}
	return left.Raw < right.Raw
}

func outputIndexSortValue(index *int) (int, bool) {
	if index == nil {
		return 0, false
	}
	return *index, true
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func isAbsAnyPlatform(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		c := path[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}
