package statevalidate

import (
	"fmt"
	"reflect"

	"github.com/liza-mas/liza/internal/models"
)

func validateIntegrationLifecycle(v *violations, state *models.State) {
	tasksByID := make(map[string]*models.Task, len(state.Tasks))
	analysisKeys := make(map[string]string)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		tasksByID[task.ID] = task
		if task.IntegrationAnalysis == nil {
			continue
		}
		validateIntegrationAnalysisMetadata(v, task)
		key := task.IntegrationAnalysis.Key
		if firstTaskID, exists := analysisKeys[key]; exists {
			// Identity by the later task only: removing the first holder must
			// not make the remaining duplicates look new.
			v.addID(fmt.Sprintf("duplicate integration analysis key %q on task %s", key, task.ID),
				fmt.Errorf("duplicate integration analysis key %q on tasks %s and %s", key, firstTaskID, task.ID))
			continue
		}
		analysisKeys[key] = task.ID
	}

	lifecycle := state.Goal.Integration
	if lifecycle == nil {
		return
	}
	validatePrematureRecoveryInto(v, state)
	frozenRoots := validateContributingSet(v, lifecycle.ContributingSet)
	validateIntegrationCoverage(v, lifecycle.Coverage, frozenRoots, tasksByID)
	validateGlobalGenerations(v, lifecycle.GlobalGenerations, tasksByID, lifecycle.FirstGlobalGeneration())
	validateMutationReceipts(v, lifecycle.MutationReceipts)
	validateIntegrationClosure(v, lifecycle.Closure, lifecycle.GlobalGenerations)
}

func validateIntegrationAnalysisMetadata(v *violations, task *models.Task) {
	metadata := task.IntegrationAnalysis
	if metadata.Key == "" {
		v.add(fmt.Errorf("task %s integration analysis key is empty", task.ID))
	}
	if !metadata.Phase.IsValid() {
		v.add(fmt.Errorf("task %s has invalid integration analysis phase %q", task.ID, metadata.Phase))
	}
	if metadata.SourceCommit == "" {
		v.add(fmt.Errorf("task %s integration analysis source commit is empty", task.ID))
	}

	switch metadata.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		if metadata.Generation != 0 {
			v.add(fmt.Errorf("task %s slice analysis generation must be zero", task.ID))
		}
		if metadata.OriginatingPlanTaskID == "" {
			v.add(fmt.Errorf("task %s slice analysis originating plan is empty", task.ID))
		}
		if len(metadata.RootTaskIDs) == 0 {
			v.add(fmt.Errorf("task %s slice analysis roots are empty", task.ID))
		}
	case models.IntegrationAnalysisPhaseGlobal:
		if metadata.Generation <= 0 {
			v.add(fmt.Errorf("task %s global analysis generation must be positive", task.ID))
		}
		if metadata.OriginatingPlanTaskID != "" {
			v.add(fmt.Errorf("task %s global analysis slice fields must be empty (originating plan)", task.ID))
		}
		if len(metadata.RootTaskIDs) != 0 {
			v.add(fmt.Errorf("task %s global analysis slice fields must be empty (roots)", task.ID))
		}
	}

	prefix := fmt.Sprintf("task %s integration analysis: ", task.ID)
	validateUniqueNonEmptyStringsInto(v, prefix, metadata.RootTaskIDs, "root task")
	descendantTasks := make([]string, 0, len(metadata.DescendantChanges))
	descendantCommits := make([]string, 0, len(metadata.DescendantChanges))
	for _, change := range metadata.DescendantChanges {
		descendantTasks = append(descendantTasks, change.TaskID)
		descendantCommits = append(descendantCommits, change.Commit)
	}
	validateUniqueNonEmptyStringsInto(v, prefix, descendantTasks, "descendant task")
	validateUniqueNonEmptyStringsInto(v, prefix, descendantCommits, "descendant commit")
	validateUniqueNonEmptyStringsInto(v, prefix, metadata.AffectedPaths, "affected path")
	validateUniqueNonEmptyStringsInto(v, prefix, metadata.SourceSnapshotPaths, "source snapshot path")
}

// validateContributingSet returns the frozen roots of every scope with a plan
// ID, including scopes that have other violations, so coverage validation
// still checks what references them.
func validateContributingSet(v *violations, set *models.IntegrationContributingSet) map[string][]string {
	if set == nil {
		return nil
	}
	frozenRoots := make(map[string][]string, len(set.Scopes))
	rootOwners := make(map[string]string)
	// The set is immutable once frozen, so a scope's index is a stable owner.
	for i, scope := range set.Scopes {
		v.within(fmt.Sprintf("contributing scope %d", i), func(v *violations) {
			if scope.PlanTaskID == "" {
				v.add(fmt.Errorf("integration contributing plan is empty"))
			} else if _, exists := frozenRoots[scope.PlanTaskID]; exists {
				v.add(fmt.Errorf("duplicate contributing plan %q", scope.PlanTaskID))
			}
			if len(scope.RootTaskIDs) == 0 {
				v.add(fmt.Errorf("contributing plan %s has no root tasks", scope.PlanTaskID))
			}
			validateUniqueNonEmptyStringsInto(v, fmt.Sprintf("contributing plan %s: ", scope.PlanTaskID), scope.RootTaskIDs, "root task")
			for _, rootTaskID := range scope.RootTaskIDs {
				if firstPlan, exists := rootOwners[rootTaskID]; exists {
					v.add(fmt.Errorf("root task %q belongs to multiple contributing plans %s and %s", rootTaskID, firstPlan, scope.PlanTaskID))
					continue
				}
				rootOwners[rootTaskID] = scope.PlanTaskID
			}
		})
		if _, exists := frozenRoots[scope.PlanTaskID]; !exists && scope.PlanTaskID != "" {
			frozenRoots[scope.PlanTaskID] = scope.RootTaskIDs
		}
	}
	return frozenRoots
}

func validateIntegrationCoverage(
	v *violations,
	coverage []models.IntegrationCoverageRecord,
	frozenRoots map[string][]string,
	tasksByID map[string]*models.Task,
) {
	plans := make(map[string]struct{}, len(coverage))
	sliceReferences := make(map[string]string)
	// Coverage is append-only, so a record's index is a stable owner: the
	// same defect on two records stays two violations.
	for i, record := range coverage {
		v.within(fmt.Sprintf("integration coverage %d", i), func(v *violations) {
			validateCoverageRecord(v, record, plans, sliceReferences, frozenRoots, tasksByID)
		})
	}
}

func validateCoverageRecord(
	v *violations,
	record models.IntegrationCoverageRecord,
	plans map[string]struct{},
	sliceReferences map[string]string,
	frozenRoots map[string][]string,
	tasksByID map[string]*models.Task,
) {
	if _, exists := plans[record.PlanTaskID]; exists {
		v.add(fmt.Errorf("duplicate integration coverage plan %q", record.PlanTaskID))
	}
	plans[record.PlanTaskID] = struct{}{}
	roots, exists := frozenRoots[record.PlanTaskID]
	if !exists {
		v.add(fmt.Errorf("coverage references unknown contributing plan %q", record.PlanTaskID))
	}
	if !record.Kind.IsValid() {
		v.add(fmt.Errorf("invalid integration coverage kind %q for plan %s", record.Kind, record.PlanTaskID))
	}
	payloadCount := 0
	if len(record.ApprovalAttestations) > 0 {
		payloadCount++
	}
	if record.SliceReport != nil {
		payloadCount++
	}
	if payloadCount != 1 {
		v.add(fmt.Errorf("integration coverage for plan %s must have exactly one payload", record.PlanTaskID))
	}

	switch record.Kind {
	case models.IntegrationCoverageApprovalAttestation:
		if len(record.ApprovalAttestations) == 0 || record.SliceReport != nil {
			v.add(fmt.Errorf("approval-attestation coverage for plan %s must have exactly one matching payload", record.PlanTaskID))
		}
		validateApprovalAttestations(v, fmt.Sprintf("plan %s: ", record.PlanTaskID), record.ApprovalAttestations)
	case models.IntegrationCoverageSliceReport:
		if record.SliceReport == nil || len(record.ApprovalAttestations) != 0 {
			v.add(fmt.Errorf("slice-report coverage for plan %s must have exactly one matching payload", record.PlanTaskID))
		}
		// The slice report's own checks need the report; the roots
		// comparison needs the plan's frozen roots.
		if record.SliceReport == nil {
			return
		}
		reference := record.SliceReport.AnalysisTaskID + "\x00" + record.SliceReport.AnalysisKey
		if firstPlan, reused := sliceReferences[reference]; reused {
			v.add(fmt.Errorf("slice analysis is reused by coverage plans %s and %s", firstPlan, record.PlanTaskID))
		} else {
			sliceReferences[reference] = record.PlanTaskID
		}
		validateSliceReport(v, record.PlanTaskID, roots, exists, record.SliceReport, tasksByID)
	}
}

func validateApprovalAttestations(v *violations, prefix string, attestations []models.IntegrationApprovalAttestation) {
	if len(attestations) == 0 {
		v.add(fmt.Errorf("%sapproval attestation set is empty", prefix))
		return
	}
	reviewedTasks := make(map[string]struct{}, len(attestations))
	for i := range attestations {
		attestation := &attestations[i]
		// Attestations live inside an append-only coverage record, so the
		// index is a stable owner even when the reviewed task ID is empty.
		v.within(fmt.Sprintf("attestation %d", i), func(v *violations) {
			validateApprovalAttestation(v, prefix, attestation)
			if _, exists := reviewedTasks[attestation.ReviewedTaskID]; exists {
				v.add(fmt.Errorf("%sduplicate approval attestation reviewed task ID %q", prefix, attestation.ReviewedTaskID))
				return
			}
			reviewedTasks[attestation.ReviewedTaskID] = struct{}{}
		})
	}
}

func validateApprovalAttestation(v *violations, prefix string, attestation *models.IntegrationApprovalAttestation) {
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
			v.add(fmt.Errorf("%sapproval attestation %s is empty", prefix, field.name))
		}
	}
	if len(attestation.Validation) == 0 {
		v.add(fmt.Errorf("%sapproval attestation validation is empty", prefix))
	}
	validateUniqueNonEmptyStringsInto(v, prefix, attestation.Validation, "approval validation")
}

// validateSliceReport compares the report with frozenRoots only when
// rootsKnown: a report for an unknown plan is already a violation, and
// comparing it with no roots would report a second one for the same cause.
func validateSliceReport(
	v *violations,
	planTaskID string,
	frozenRoots []string,
	rootsKnown bool,
	report *models.IntegrationSliceReport,
	tasksByID map[string]*models.Task,
) {
	if report.AnalysisTaskID == "" {
		v.add(fmt.Errorf("slice report analysis task ID is empty"))
	}
	if report.AnalysisKey == "" {
		v.add(fmt.Errorf("slice report analysis key is empty"))
	}
	if !report.Verdict.IsValid() {
		v.add(fmt.Errorf("slice report has invalid verdict %q", report.Verdict))
	}
	if report.SourceCommit == "" {
		if report.Verdict == models.IntegrationAnalysisVerdictClean {
			v.add(fmt.Errorf("clean slice report source commit is empty"))
		} else {
			v.add(fmt.Errorf("slice report source commit is empty"))
		}
	}
	if report.ReportCommit == "" {
		v.add(fmt.Errorf("slice report report commit is empty"))
	}
	task := tasksByID[report.AnalysisTaskID]
	if task == nil || task.IntegrationAnalysis == nil {
		// Guard: the comparisons below read the analysis task.
		v.add(fmt.Errorf("slice report references missing analysis task %q", report.AnalysisTaskID))
		return
	}
	metadata := task.IntegrationAnalysis
	if metadata.Phase != models.IntegrationAnalysisPhaseSlice {
		v.add(fmt.Errorf("slice report task %s is not a slice analysis", task.ID))
	}
	if metadata.Key != report.AnalysisKey {
		v.add(fmt.Errorf("slice report key does not match analysis metadata key for task %s", task.ID))
	}
	if metadata.SourceCommit != report.SourceCommit {
		v.add(fmt.Errorf("slice report source commit does not match analysis metadata for task %s", task.ID))
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != report.ReportCommit {
		v.add(fmt.Errorf("slice report commit does not match task %s review commit", task.ID))
	}
	if metadata.OriginatingPlanTaskID != planTaskID {
		v.add(fmt.Errorf("slice coverage plan does not match analysis metadata plan for task %s", task.ID))
	}
	if rootsKnown && !sameStringSet(metadata.RootTaskIDs, frozenRoots) {
		v.add(fmt.Errorf("slice analysis roots do not match frozen roots for plan %s", planTaskID))
	}
}

func validateGlobalGenerations(v *violations, generations []models.IntegrationGlobalGeneration, tasksByID map[string]*models.Task, firstGeneration ...int) {
	first := 1
	if len(firstGeneration) > 0 {
		first = firstGeneration[0]
	}
	// Generations are append-only, so the index is a stable owner.
	for i, generation := range generations {
		v.within(fmt.Sprintf("global generation index %d", i), func(v *violations) {
			validateGlobalGeneration(v, generation, i+first, tasksByID)
		})
	}
}

func validateGlobalGeneration(v *violations, generation models.IntegrationGlobalGeneration, expected int, tasksByID map[string]*models.Task) {
	if generation.Generation != expected {
		v.add(fmt.Errorf("global generation %d, want %d", generation.Generation, expected))
	}
	if generation.AnalysisTaskID == "" {
		v.add(fmt.Errorf("global generation %d analysis task ID is empty", expected))
	}
	if generation.AnalysisKey == "" {
		v.add(fmt.Errorf("global generation %d analysis key is empty", expected))
	}
	if !generation.Verdict.IsValid() {
		v.add(fmt.Errorf("global generation %d has invalid verdict %q", expected, generation.Verdict))
	}
	if generation.SourceCommit == "" {
		if generation.Verdict == models.IntegrationAnalysisVerdictClean {
			v.add(fmt.Errorf("clean global generation source commit is empty"))
		} else {
			v.add(fmt.Errorf("global generation %d source commit is empty", expected))
		}
	}
	if generation.ReportCommit == "" {
		v.add(fmt.Errorf("global generation %d report commit is empty", expected))
	}
	task := tasksByID[generation.AnalysisTaskID]
	if task == nil || task.IntegrationAnalysis == nil {
		// Guard: the comparisons below read the analysis task.
		v.add(fmt.Errorf("global generation %d references missing analysis task %q", expected, generation.AnalysisTaskID))
		return
	}
	metadata := task.IntegrationAnalysis
	for _, field := range []struct {
		name     string
		mismatch bool
	}{
		{"phase", metadata.Phase != models.IntegrationAnalysisPhaseGlobal},
		{"generation", metadata.Generation != generation.Generation},
		{"key", metadata.Key != generation.AnalysisKey},
		{"source commit", metadata.SourceCommit != generation.SourceCommit},
	} {
		if field.mismatch {
			v.add(fmt.Errorf("global generation %d does not match analysis metadata for task %s (%s)", expected, task.ID, field.name))
		}
	}
	if task.ReviewCommit == nil || *task.ReviewCommit != generation.ReportCommit {
		v.add(fmt.Errorf("global generation %d report commit does not match task %s review commit", expected, task.ID))
	}
}

func validateMutationReceipts(v *violations, receipts []models.IntegrationMutationReceipt) {
	for i, receipt := range receipts {
		if receipt.TaskID == "" {
			v.add(fmt.Errorf("mutation receipt %d task ID is empty", i))
		}
		if receipt.BeforeCommit == "" {
			v.add(fmt.Errorf("mutation receipt before commit is empty at index %d", i))
		}
		if receipt.AfterCommit == "" {
			v.add(fmt.Errorf("mutation receipt after commit is empty at index %d", i))
		}
		if receipt.BeforeCommit != "" && receipt.BeforeCommit == receipt.AfterCommit {
			v.add(fmt.Errorf("mutation receipt commits must differ at index %d", i))
		}
	}
}

func validateIntegrationClosure(v *violations, closure *models.IntegrationClosure, generations []models.IntegrationGlobalGeneration) {
	if closure == nil {
		return
	}
	if !closure.Status.IsValid() {
		v.add(fmt.Errorf("invalid integration closure status %q", closure.Status))
	}
	switch closure.Status {
	case models.IntegrationClosureStatusClean:
		if closure.SourceCommit == "" {
			v.add(fmt.Errorf("clean integration closure source commit is empty"))
		}
		var generation *models.IntegrationGlobalGeneration
		for i := range generations {
			if generations[i].Generation == closure.Generation {
				generation = &generations[i]
				break
			}
		}
		if generation == nil {
			// Guard: the comparisons below read the generation.
			v.add(fmt.Errorf("clean integration closure references missing generation %d", closure.Generation))
			return
		}
		if generation.Verdict != models.IntegrationAnalysisVerdictClean {
			v.add(fmt.Errorf("clean integration closure references non-clean generation %d", closure.Generation))
		}
		if closure.AnalysisKey != generation.AnalysisKey {
			v.add(fmt.Errorf("clean integration closure does not match generation %d (analysis key)", closure.Generation))
		}
		if closure.SourceCommit != generation.SourceCommit {
			v.add(fmt.Errorf("clean integration closure does not match generation %d (source commit)", closure.Generation))
		}
	case models.IntegrationClosureStatusBlocked:
		if closure.Reason == "" {
			v.add(fmt.Errorf("blocked integration closure reason is empty"))
		}
	case models.IntegrationClosureStatusExhausted:
		if closure.Reason == "" {
			v.add(fmt.Errorf("exhausted integration closure reason is empty"))
		}
	}
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
	if previousLifecycle == nil && candidateLifecycle != nil && candidateLifecycle.PrematureRecovery != nil {
		return fmt.Errorf("premature recovery requires a prior empty frozen cohort")
	}
	recovering := previousLifecycle != nil && previousLifecycle.PrematureRecovery == nil && candidateLifecycle != nil && candidateLifecycle.PrematureRecovery != nil
	if recovering {
		if err := validatePrematureRecoveryTransition(previous, candidate); err != nil {
			return err
		}
	}
	if previousLifecycle != nil {
		if candidateLifecycle == nil {
			return fmt.Errorf("integration lifecycle cannot be cleared")
		}
		if previousLifecycle.PrematureRecovery != nil && !reflect.DeepEqual(previousLifecycle.PrematureRecovery, candidateLifecycle.PrematureRecovery) {
			return fmt.Errorf("integration premature recovery receipt cannot change")
		}
		if previousLifecycle.ContributingSet != nil && !recovering {
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

// ValidatePrematureRecovery checks a recorded premature-recovery receipt
// against the abandoned analysis task it must retain. Recovery replay uses it
// to vouch for its own evidence without validating unrelated records.
func ValidatePrematureRecovery(state *models.State) error {
	return validatePrematureRecovery(state)
}

func validatePrematureRecovery(state *models.State) error {
	return collectErr(func(v *violations) { validatePrematureRecoveryInto(v, state) })
}

func validatePrematureRecoveryInto(v *violations, state *models.State) {
	lifecycle := state.Goal.Integration
	if lifecycle == nil || lifecycle.PrematureRecovery == nil {
		return
	}
	recovery := lifecycle.PrematureRecovery
	for _, defect := range []struct {
		field  string
		broken bool
	}{
		{"at", recovery.At.IsZero()},
		{"reason", recovery.Reason == ""},
		{"report_commit", recovery.ReportCommit == ""},
		{"source_commit", recovery.SourceCommit == ""},
		{"preservation_ref", recovery.PreservationRef != "refs/integration-recovery/"+recovery.AnalysisTaskID},
		{"previous_contributing_set", len(recovery.PreviousContributingSet.Scopes) != 0},
	} {
		if defect.broken {
			v.add(fmt.Errorf("invalid integration premature recovery receipt: %s", defect.field))
		}
	}
	task := state.FindTask(recovery.AnalysisTaskID)
	if task == nil || task.IntegrationAnalysis == nil {
		// Guard: the metadata comparison needs the retained task.
		v.add(fmt.Errorf("premature recovery must retain its abandoned analysis task"))
		return
	}
	if task.Status != models.TaskStatusAbandoned {
		v.addID("premature recovery must retain its abandoned analysis task (status)",
			fmt.Errorf("premature recovery must retain its abandoned analysis task (status %s)", task.Status))
	}
	m := task.IntegrationAnalysis
	for _, field := range []struct {
		name     string
		mismatch bool
	}{
		{"key", m.Key != "global:1"},
		{"phase", m.Phase != models.IntegrationAnalysisPhaseGlobal},
		{"generation", m.Generation != 1},
		{"source commit", m.SourceCommit != recovery.SourceCommit},
		{"descendant changes", len(m.DescendantChanges) != 0},
	} {
		if field.mismatch {
			v.add(fmt.Errorf("premature recovery does not match retained global analysis metadata (%s)", field.name))
		}
	}
}
func validatePrematureRecoveryTransition(previous, candidate *models.State) error {
	if err := validatePrematureRecovery(candidate); err != nil {
		return err
	}
	old, next := previous.Goal.Integration, candidate.Goal.Integration
	r := next.PrematureRecovery
	if previous.Config.Mode != models.SystemModePaused || candidate.Config.Mode != models.SystemModePaused || old.ContributingSet == nil || len(old.ContributingSet.Scopes) != 0 || next.ContributingSet != nil || len(old.Coverage) != 0 || len(old.GlobalGenerations) != 0 || old.Closure != nil || len(next.Coverage) != 0 || len(next.GlobalGenerations) != 0 || next.Closure != nil {
		return fmt.Errorf("premature recovery requires paused empty cohort without accepted integration evidence")
	}
	task := previous.FindTask(r.AnalysisTaskID)
	if task == nil || task.Status.IsTerminal() || task.ReviewingBy != nil || task.ReviewCommit == nil || *task.ReviewCommit != r.ReportCommit || task.MergeCommit != nil || len(task.Approvals) != 0 || task.ApprovedBy != nil {
		return fmt.Errorf("premature recovery requires an unreviewed submitted analysis")
	}
	count := 0
	for _, t := range previous.Tasks {
		if t.IntegrationAnalysis != nil {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("premature recovery requires exactly one integration analysis")
	}
	return nil
}

// validateUniqueNonEmptyStringsInto reports each empty entry and each extra
// copy of a value as its own violation, prefixed with its owner.
func validateUniqueNonEmptyStringsInto(v *violations, prefix string, values []string, label string) {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			v.add(fmt.Errorf("%s%s is empty", prefix, label))
			continue
		}
		if _, exists := seen[value]; exists {
			v.add(fmt.Errorf("%sduplicate %s %q", prefix, label, value))
			continue
		}
		seen[value] = struct{}{}
	}
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
		delete(values, value)
	}
	return len(values) == 0
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
