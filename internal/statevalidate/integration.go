package statevalidate

import (
	"fmt"
	"reflect"

	"github.com/liza-mas/liza/internal/models"
)

func validateIntegrationLifecycle(state *models.State, _ string, _ bool) error {
	tasksByID := make(map[string]*models.Task, len(state.Tasks))
	analysisKeys := make(map[string]string)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		tasksByID[task.ID] = task
		if task.IntegrationAnalysis == nil {
			continue
		}
		if err := validateIntegrationAnalysisMetadata(task); err != nil {
			return err
		}
		key := task.IntegrationAnalysis.Key
		if firstTaskID, exists := analysisKeys[key]; exists {
			return fmt.Errorf("duplicate integration analysis key %q on tasks %s and %s", key, firstTaskID, task.ID)
		}
		analysisKeys[key] = task.ID
	}

	lifecycle := state.Goal.Integration
	if lifecycle == nil {
		return nil
	}
	frozenRoots, err := validateContributingSet(lifecycle.ContributingSet)
	if err != nil {
		return err
	}
	if err := validateIntegrationCoverage(lifecycle.Coverage, frozenRoots, tasksByID); err != nil {
		return err
	}
	if err := validateGlobalGenerations(lifecycle.GlobalGenerations, tasksByID); err != nil {
		return err
	}
	if err := validateMutationReceipts(lifecycle.MutationReceipts); err != nil {
		return err
	}
	return validateIntegrationClosure(lifecycle.Closure, lifecycle.GlobalGenerations)
}

func validateIntegrationAnalysisMetadata(task *models.Task) error {
	metadata := task.IntegrationAnalysis
	if metadata.Key == "" {
		return fmt.Errorf("task %s integration analysis key is empty", task.ID)
	}
	if !metadata.Phase.IsValid() {
		return fmt.Errorf("task %s has invalid integration analysis phase %q", task.ID, metadata.Phase)
	}
	if metadata.SourceCommit == "" {
		return fmt.Errorf("task %s integration analysis source commit is empty", task.ID)
	}

	switch metadata.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		if metadata.Generation != 0 {
			return fmt.Errorf("task %s slice analysis generation must be zero", task.ID)
		}
		if metadata.OriginatingPlanTaskID == "" {
			return fmt.Errorf("task %s slice analysis originating plan is empty", task.ID)
		}
		if len(metadata.RootTaskIDs) == 0 {
			return fmt.Errorf("task %s slice analysis roots are empty", task.ID)
		}
	case models.IntegrationAnalysisPhaseGlobal:
		if metadata.Generation <= 0 {
			return fmt.Errorf("task %s global analysis generation must be positive", task.ID)
		}
		if metadata.OriginatingPlanTaskID != "" || len(metadata.RootTaskIDs) != 0 {
			return fmt.Errorf("task %s global analysis slice fields must be empty", task.ID)
		}
	}

	if err := validateUniqueNonEmptyStrings(metadata.RootTaskIDs, "root task"); err != nil {
		return fmt.Errorf("task %s integration analysis: %w", task.ID, err)
	}
	descendantTasks := make([]string, 0, len(metadata.DescendantChanges))
	descendantCommits := make([]string, 0, len(metadata.DescendantChanges))
	for _, change := range metadata.DescendantChanges {
		descendantTasks = append(descendantTasks, change.TaskID)
		descendantCommits = append(descendantCommits, change.Commit)
	}
	if err := validateUniqueNonEmptyStrings(descendantTasks, "descendant task"); err != nil {
		return fmt.Errorf("task %s integration analysis: %w", task.ID, err)
	}
	if err := validateUniqueNonEmptyStrings(descendantCommits, "descendant commit"); err != nil {
		return fmt.Errorf("task %s integration analysis: %w", task.ID, err)
	}
	if err := validateUniqueNonEmptyStrings(metadata.AffectedPaths, "affected path"); err != nil {
		return fmt.Errorf("task %s integration analysis: %w", task.ID, err)
	}
	if err := validateUniqueNonEmptyStrings(metadata.SourceSnapshotPaths, "source snapshot path"); err != nil {
		return fmt.Errorf("task %s integration analysis: %w", task.ID, err)
	}
	return nil
}

func validateContributingSet(set *models.IntegrationContributingSet) (map[string][]string, error) {
	if set == nil {
		return nil, nil
	}
	if len(set.Scopes) == 0 {
		return nil, fmt.Errorf("integration contributing set has no scopes")
	}
	frozenRoots := make(map[string][]string, len(set.Scopes))
	rootOwners := make(map[string]string)
	for _, scope := range set.Scopes {
		if scope.PlanTaskID == "" {
			return nil, fmt.Errorf("integration contributing plan is empty")
		}
		if _, exists := frozenRoots[scope.PlanTaskID]; exists {
			return nil, fmt.Errorf("duplicate contributing plan %q", scope.PlanTaskID)
		}
		if len(scope.RootTaskIDs) == 0 {
			return nil, fmt.Errorf("contributing plan %s has no root tasks", scope.PlanTaskID)
		}
		if err := validateUniqueNonEmptyStrings(scope.RootTaskIDs, "root task"); err != nil {
			return nil, fmt.Errorf("contributing plan %s: %w", scope.PlanTaskID, err)
		}
		for _, rootTaskID := range scope.RootTaskIDs {
			if firstPlan, exists := rootOwners[rootTaskID]; exists {
				return nil, fmt.Errorf("root task %q belongs to multiple contributing plans %s and %s", rootTaskID, firstPlan, scope.PlanTaskID)
			}
			rootOwners[rootTaskID] = scope.PlanTaskID
		}
		frozenRoots[scope.PlanTaskID] = scope.RootTaskIDs
	}
	return frozenRoots, nil
}

func validateIntegrationCoverage(
	coverage []models.IntegrationCoverageRecord,
	frozenRoots map[string][]string,
	tasksByID map[string]*models.Task,
) error {
	plans := make(map[string]struct{}, len(coverage))
	sliceReferences := make(map[string]string)
	for _, record := range coverage {
		if _, exists := plans[record.PlanTaskID]; exists {
			return fmt.Errorf("duplicate integration coverage plan %q", record.PlanTaskID)
		}
		plans[record.PlanTaskID] = struct{}{}
		roots, exists := frozenRoots[record.PlanTaskID]
		if !exists {
			return fmt.Errorf("coverage references unknown contributing plan %q", record.PlanTaskID)
		}
		if !record.Kind.IsValid() {
			return fmt.Errorf("invalid integration coverage kind %q for plan %s", record.Kind, record.PlanTaskID)
		}
		payloadCount := 0
		if record.ApprovalAttestation != nil {
			payloadCount++
		}
		if record.SliceReport != nil {
			payloadCount++
		}
		if payloadCount != 1 {
			return fmt.Errorf("integration coverage for plan %s must have exactly one payload", record.PlanTaskID)
		}

		switch record.Kind {
		case models.IntegrationCoverageApprovalAttestation:
			if record.ApprovalAttestation == nil || record.SliceReport != nil {
				return fmt.Errorf("approval-attestation coverage for plan %s must have exactly one matching payload", record.PlanTaskID)
			}
			if err := validateApprovalAttestation(record.ApprovalAttestation); err != nil {
				return fmt.Errorf("plan %s: %w", record.PlanTaskID, err)
			}
		case models.IntegrationCoverageSliceReport:
			if record.SliceReport == nil || record.ApprovalAttestation != nil {
				return fmt.Errorf("slice-report coverage for plan %s must have exactly one matching payload", record.PlanTaskID)
			}
			reference := record.SliceReport.AnalysisTaskID + "\x00" + record.SliceReport.AnalysisKey
			if firstPlan, reused := sliceReferences[reference]; reused {
				return fmt.Errorf("slice analysis is reused by coverage plans %s and %s", firstPlan, record.PlanTaskID)
			}
			sliceReferences[reference] = record.PlanTaskID
			if err := validateSliceReport(record.PlanTaskID, roots, record.SliceReport, tasksByID); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateApprovalAttestation(attestation *models.IntegrationApprovalAttestation) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "reviewed task ID", value: attestation.ReviewedTaskID},
		{name: "acceptance criteria", value: attestation.AcceptanceCriteria},
		{name: "reviewed commit", value: attestation.ReviewedCommit},
		{name: "approver", value: attestation.Approver},
		{name: "merge commit", value: attestation.MergeCommit},
	}
	for _, field := range required {
		if field.value == "" {
			return fmt.Errorf("approval attestation %s is empty", field.name)
		}
	}
	if len(attestation.Validation) == 0 {
		return fmt.Errorf("approval attestation validation is empty")
	}
	if err := validateUniqueNonEmptyStrings(attestation.Validation, "approval validation"); err != nil {
		return err
	}
	return nil
}

func validateSliceReport(
	planTaskID string,
	frozenRoots []string,
	report *models.IntegrationSliceReport,
	tasksByID map[string]*models.Task,
) error {
	if report.AnalysisTaskID == "" {
		return fmt.Errorf("slice report analysis task ID is empty")
	}
	if report.AnalysisKey == "" {
		return fmt.Errorf("slice report analysis key is empty")
	}
	if !report.Verdict.IsValid() {
		return fmt.Errorf("slice report has invalid verdict %q", report.Verdict)
	}
	if report.SourceCommit == "" {
		if report.Verdict == models.IntegrationAnalysisVerdictClean {
			return fmt.Errorf("clean slice report source commit is empty")
		}
		return fmt.Errorf("slice report source commit is empty")
	}
	if report.ReportCommit == "" {
		return fmt.Errorf("slice report report commit is empty")
	}
	task := tasksByID[report.AnalysisTaskID]
	if task == nil || task.IntegrationAnalysis == nil {
		return fmt.Errorf("slice report references missing analysis task %q", report.AnalysisTaskID)
	}
	metadata := task.IntegrationAnalysis
	if metadata.Phase != models.IntegrationAnalysisPhaseSlice {
		return fmt.Errorf("slice report task %s is not a slice analysis", task.ID)
	}
	if metadata.Key != report.AnalysisKey {
		return fmt.Errorf("slice report key does not match analysis metadata key for task %s", task.ID)
	}
	if metadata.SourceCommit != report.SourceCommit {
		return fmt.Errorf("slice report source commit does not match analysis metadata for task %s", task.ID)
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != report.ReportCommit {
		return fmt.Errorf("slice report commit does not match task %s review commit", task.ID)
	}
	if metadata.OriginatingPlanTaskID != planTaskID {
		return fmt.Errorf("slice coverage plan does not match analysis metadata plan for task %s", task.ID)
	}
	if !sameStringSet(metadata.RootTaskIDs, frozenRoots) {
		return fmt.Errorf("slice analysis roots do not match frozen roots for plan %s", planTaskID)
	}
	return nil
}

func validateGlobalGenerations(generations []models.IntegrationGlobalGeneration, tasksByID map[string]*models.Task) error {
	for i, generation := range generations {
		expected := i + 1
		if generation.Generation != expected {
			return fmt.Errorf("global generation %d, want %d", generation.Generation, expected)
		}
		if generation.AnalysisTaskID == "" {
			return fmt.Errorf("global generation %d analysis task ID is empty", expected)
		}
		if generation.AnalysisKey == "" {
			return fmt.Errorf("global generation %d analysis key is empty", expected)
		}
		if !generation.Verdict.IsValid() {
			return fmt.Errorf("global generation %d has invalid verdict %q", expected, generation.Verdict)
		}
		if generation.SourceCommit == "" {
			if generation.Verdict == models.IntegrationAnalysisVerdictClean {
				return fmt.Errorf("clean global generation source commit is empty")
			}
			return fmt.Errorf("global generation %d source commit is empty", expected)
		}
		if generation.ReportCommit == "" {
			return fmt.Errorf("global generation %d report commit is empty", expected)
		}
		task := tasksByID[generation.AnalysisTaskID]
		if task == nil || task.IntegrationAnalysis == nil {
			return fmt.Errorf("global generation %d references missing analysis task %q", expected, generation.AnalysisTaskID)
		}
		metadata := task.IntegrationAnalysis
		if metadata.Phase != models.IntegrationAnalysisPhaseGlobal ||
			metadata.Generation != generation.Generation ||
			metadata.Key != generation.AnalysisKey ||
			metadata.SourceCommit != generation.SourceCommit {
			return fmt.Errorf("global generation %d does not match analysis metadata for task %s", expected, task.ID)
		}
		if task.ReviewCommit == nil || *task.ReviewCommit != generation.ReportCommit {
			return fmt.Errorf("global generation %d report commit does not match task %s review commit", expected, task.ID)
		}
	}
	return nil
}

func validateMutationReceipts(receipts []models.IntegrationMutationReceipt) error {
	for i, receipt := range receipts {
		if receipt.TaskID == "" {
			return fmt.Errorf("mutation receipt %d task ID is empty", i)
		}
		if receipt.BeforeCommit == "" {
			return fmt.Errorf("mutation receipt before commit is empty at index %d", i)
		}
		if receipt.AfterCommit == "" {
			return fmt.Errorf("mutation receipt after commit is empty at index %d", i)
		}
		if receipt.BeforeCommit == receipt.AfterCommit {
			return fmt.Errorf("mutation receipt commits must differ at index %d", i)
		}
	}
	return nil
}

func validateIntegrationClosure(closure *models.IntegrationClosure, generations []models.IntegrationGlobalGeneration) error {
	if closure == nil {
		return nil
	}
	if !closure.Status.IsValid() {
		return fmt.Errorf("invalid integration closure status %q", closure.Status)
	}
	switch closure.Status {
	case models.IntegrationClosureStatusClean:
		if closure.SourceCommit == "" {
			return fmt.Errorf("clean integration closure source commit is empty")
		}
		if closure.Generation <= 0 || closure.Generation > len(generations) {
			return fmt.Errorf("clean integration closure references missing generation %d", closure.Generation)
		}
		generation := generations[closure.Generation-1]
		if generation.Verdict != models.IntegrationAnalysisVerdictClean {
			return fmt.Errorf("clean integration closure references non-clean generation %d", closure.Generation)
		}
		if closure.AnalysisKey != generation.AnalysisKey || closure.SourceCommit != generation.SourceCommit {
			return fmt.Errorf("clean integration closure does not match generation %d", closure.Generation)
		}
	case models.IntegrationClosureStatusBlocked:
		if closure.Reason == "" {
			return fmt.Errorf("blocked integration closure reason is empty")
		}
	case models.IntegrationClosureStatusExhausted:
		if closure.Reason == "" {
			return fmt.Errorf("exhausted integration closure reason is empty")
		}
	}
	return nil
}

// ValidateIntegrationLifecycleTransition rejects rewrites of integration
// evidence that has already been persisted. Candidate structural validation is
// intentionally composed separately through ValidateState.
func ValidateIntegrationLifecycleTransition(previous, candidate *models.State) error {
	if previous == nil || candidate == nil {
		return fmt.Errorf("integration lifecycle transition requires previous and candidate state")
	}
	previousLifecycle := previous.Goal.Integration
	candidateLifecycle := candidate.Goal.Integration
	if previousLifecycle != nil {
		if candidateLifecycle == nil {
			return fmt.Errorf("integration lifecycle cannot be cleared")
		}
		if previousLifecycle.ContributingSet != nil {
			if candidateLifecycle.ContributingSet == nil {
				return fmt.Errorf("integration contributing set cannot be cleared")
			}
			if !sameContributingSet(previousLifecycle.ContributingSet, candidateLifecycle.ContributingSet) {
				return fmt.Errorf("integration contributing set cannot change")
			}
		}
		if !isSlicePrefix(previousLifecycle.Coverage, candidateLifecycle.Coverage) {
			return fmt.Errorf("integration coverage records are append-only")
		}
		if !isSlicePrefix(previousLifecycle.GlobalGenerations, candidateLifecycle.GlobalGenerations) {
			return fmt.Errorf("integration global generations are append-only")
		}
		if !isSlicePrefix(previousLifecycle.MutationReceipts, candidateLifecycle.MutationReceipts) {
			return fmt.Errorf("integration mutation receipts are append-only")
		}
	}

	candidateTasks := make(map[string]*models.Task, len(candidate.Tasks))
	for i := range candidate.Tasks {
		candidateTasks[candidate.Tasks[i].ID] = &candidate.Tasks[i]
	}
	for i := range previous.Tasks {
		previousTask := &previous.Tasks[i]
		if previousTask.IntegrationAnalysis == nil {
			continue
		}
		candidateTask := candidateTasks[previousTask.ID]
		if candidateTask == nil || candidateTask.IntegrationAnalysis == nil {
			return fmt.Errorf("task %s integration analysis metadata cannot be cleared", previousTask.ID)
		}
		if !sameAnalysisMetadata(previousTask.IntegrationAnalysis, candidateTask.IntegrationAnalysis) {
			return fmt.Errorf("task %s integration analysis metadata cannot change", previousTask.ID)
		}
	}
	return nil
}

func validateUniqueNonEmptyStrings(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%s is empty", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	values := make(map[string]struct{}, len(left))
	for _, value := range left {
		values[value] = struct{}{}
	}
	for _, value := range right {
		if _, exists := values[value]; !exists {
			return false
		}
	}
	return true
}

func sameContributingSet(left, right *models.IntegrationContributingSet) bool {
	if len(left.Scopes) != len(right.Scopes) {
		return false
	}
	rightScopes := make(map[string][]string, len(right.Scopes))
	for _, scope := range right.Scopes {
		rightScopes[scope.PlanTaskID] = scope.RootTaskIDs
	}
	for _, scope := range left.Scopes {
		roots, exists := rightScopes[scope.PlanTaskID]
		if !exists || !sameStringSet(scope.RootTaskIDs, roots) {
			return false
		}
	}
	return true
}

func sameAnalysisMetadata(left, right *models.IntegrationAnalysisMetadata) bool {
	leftCopy := *left
	rightCopy := *right
	leftCopy.RootTaskIDs = nil
	rightCopy.RootTaskIDs = nil
	return sameStringSet(left.RootTaskIDs, right.RootTaskIDs) && reflect.DeepEqual(leftCopy, rightCopy)
}

func isSlicePrefix[T any](previous, candidate []T) bool {
	return len(candidate) >= len(previous) && reflect.DeepEqual(previous, candidate[:len(previous)])
}
