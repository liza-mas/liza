package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"golang.org/x/text/unicode/norm"
)

// AssessmentFingerprintExtraKey identifies the content identity on an assessment.
const AssessmentFingerprintExtraKey = "assessment_fingerprint_v2"

const legacyAssessmentFingerprintExtraKey = "assessment_fingerprint_v1"

// AssessmentFingerprintCandidate is the blocker payload and disposition an
// assessment would establish. Callers supply current blocker values for a
// history-only assessment, or the values to be persisted for reconciliation.
type AssessmentFingerprintCandidate struct {
	Reason        string
	Questions     []string
	RepairRequest *models.RepairRequest
	Note          string
}

type assessmentDependency struct {
	TaskID                    string    `json:"task_id"`
	Created                   time.Time `json:"created"`
	Outcome                   string    `json:"outcome"`
	ParentTasks               []string  `json:"parent_tasks,omitempty"`
	DependsOn                 []string  `json:"depends_on,omitempty"`
	SupersededBy              []string  `json:"superseded_by,omitempty"`
	NonAssessmentHistoryCount int       `json:"non_assessment_history_count,omitempty"`
}

// BuildAssessmentFingerprint returns a lowercase SHA-256 digest of the six
// material assessment inputs in valid durable state. It does not mutate its
// inputs. Assessment-only history and lifecycle revisions are deliberately
// excluded so recording an assessment cannot invalidate its own fingerprint.
func BuildAssessmentFingerprint(state *models.State, task *models.Task, candidate AssessmentFingerprintCandidate) string {
	humanCount := 0
	for _, note := range state.HumanNotes {
		if note.For == task.ID || note.For == "all" {
			humanCount++
		}
	}
	// JSON sorts map keys; typed repair/dependency records have fixed field order.
	material := map[string]any{
		"self": map[string]any{
			"status":                       task.Status,
			"non_assessment_history_count": assessmentHistoryCount(task),
			"depends_on":                   uniqueSortedStrings(task.DependsOn),
		},
		"blocker": map[string]any{
			"reason":         normalizeAssessmentText(candidate.Reason),
			"questions":      normalizeAssessmentStrings(candidate.Questions),
			"repair_request": normalizeAssessmentRepair(candidate.RepairRequest),
		},
		"disposition":  normalizeAssessmentText(candidate.Note),
		"dependencies": assessmentDependencies(state, task.DependsOn),
		"descendants":  assessmentDescendants(state, task),
		"human":        humanCount,
	}
	data, err := json.Marshal(material)
	if err != nil {
		// All fields are fixed JSON-safe types. Only an invalid task timestamp
		// can fail encoding; never silently hash an empty buffer in that case.
		panic("assessment fingerprint: invalid task timestamp")
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// IsAssessmentFingerprint accepts only the persisted 64-character lowercase
// hexadecimal representation. Foreign YAML values fail open at the caller.
func IsAssessmentFingerprint(value any) (string, bool) {
	digest, ok := value.(string)
	if !ok || len(digest) != sha256.Size*2 {
		return "", false
	}
	for _, ch := range digest {
		if !('0' <= ch && ch <= '9') && !('a' <= ch && ch <= 'f') {
			return "", false
		}
	}
	return digest, true
}

func assessmentHistoryCount(task *models.Task) int {
	count := 0
	for _, entry := range task.History {
		if entry.Event != models.TaskEventOrchestratorAssessment {
			count++
		}
	}
	return count
}

func assessmentDependencies(state *models.State, ids []string) []assessmentDependency {
	resolver := models.NewDependencyResolver(state)
	seen := make(map[string]bool)
	dependencies := make([]assessmentDependency, 0)
	for _, id := range ids {
		for _, pathID := range resolver.Resolve(id).Path {
			if seen[pathID] {
				continue
			}
			seen[pathID] = true
			entry := assessmentDependency{TaskID: pathID, Outcome: "missing"}
			if dependency := state.FindTask(pathID); dependency != nil {
				entry.Created = dependency.Created
				entry.Outcome = assessmentDependencyOutcome(dependency)
				entry.ParentTasks = uniqueSortedStrings(dependency.EffectiveParentTasks())
				entry.DependsOn = uniqueSortedStrings(dependency.DependsOn)
				entry.SupersededBy = uniqueSortedStrings(dependency.SupersededBy)
				if entry.Outcome == "unknown" {
					// An absent status has no pending-work interpretation.
					entry.NonAssessmentHistoryCount = assessmentHistoryCount(dependency)
				}
			}
			dependencies = append(dependencies, entry)
		}
	}
	sort.Slice(dependencies, func(i, j int) bool { return dependencies[i].TaskID < dependencies[j].TaskID })
	return dependencies
}

func assessmentDescendants(state *models.State, task *models.Task) []assessmentDependency {
	descendants := dependencyDescendantTasks(state, task)
	ids := make([]string, 0, len(descendants))
	for _, descendant := range descendants {
		ids = append(ids, descendant.ID)
	}
	return assessmentDependencies(state, ids)
}

// Intermediate lifecycle steps do not change a blocked consumer's options.
// Topology is recorded separately, including every reachable replacement.
func assessmentDependencyOutcome(task *models.Task) string {
	switch task.Status {
	case models.TaskStatusMerged:
		return "satisfied"
	case models.TaskStatusBlocked, models.TaskStatusAbandoned, models.TaskStatusIntegrationFailed:
		return "failed_or_blocked"
	case models.TaskStatusSuperseded:
		if len(task.SupersededBy) == 0 {
			return "failed_or_blocked"
		}
		return "replaced"
	case "":
		return "unknown"
	default:
		// Intermediate statuses belong to the configured pipeline, not a
		// closed Go enum. Only engine-owned outcomes end pending work.
		return "pending"
	}
}

func normalizeAssessmentText(value string) string {
	return strings.Join(strings.Fields(norm.NFC.String(value)), " ")
}

func normalizeAssessmentStrings(values []string) []string {
	normalized := make([]string, len(values))
	for i, value := range values {
		normalized[i] = normalizeAssessmentText(value)
	}
	return normalized
}

func normalizeAssessmentRepair(request *models.RepairRequest) models.RepairRequest {
	if request == nil {
		return models.RepairRequest{}
	}
	normalized := models.RepairRequest{
		Operation:  normalizeAssessmentText(request.Operation),
		Target:     normalizeAssessmentText(request.Target),
		Command:    normalizeAssessmentText(request.Command),
		Evidence:   normalizeAssessmentStrings(request.Evidence),
		Validation: normalizeAssessmentStrings(request.Validation),
	}
	for _, update := range request.DependencyUpdates {
		normalized.DependencyUpdates = append(normalized.DependencyUpdates, models.DependencyUpdate{
			TaskID:            normalizeAssessmentText(update.TaskID),
			ExpectedDependsOn: normalizeAssessmentStrings(update.ExpectedDependsOn),
			DesiredDependsOn:  normalizeAssessmentStrings(update.DesiredDependsOn),
		})
	}
	return normalized
}
