package ops

import (
	"testing"
	"time"
)

// The K-OWN values below are persisted (history Extra keys, lock and lifecycle
// operation labels, validation codes) or matched by other areas, so each is
// pinned against the literal it replaced.
func TestReviewOwnershipNamedValuesKeepTheirSpelling(t *testing.T) {
	t.Parallel()
	for name, pair := range map[string][2]string{
		"claim operation":              {claimReviewerTaskOperation, "claim-reviewer-task"},
		"update operation":             {updateReviewCommitOperation, "update-review-commit"},
		"old review commit key":        {oldReviewCommitExtraKey, "old_review_commit"},
		"new review commit key":        {newReviewCommitExtraKey, "new_review_commit"},
		"old base commit key":          {oldBaseCommitExtraKey, "old_base_commit"},
		"new base commit key":          {newBaseCommitExtraKey, "new_base_commit"},
		"orphaned base heal key":       {orphanedBaseHealExtraKey, "orphaned_base_heal"},
		"prepared by key":              {preparedByExtraKey, "prepared_by"},
		"review boundary phase":        {reviewBoundaryPhase, "review-boundary"},
		"worktree context claim code":  {reviewClaimCodeWorktreeContext, "worktree_context"},
		"git operation claim code":     {operationalCodeGitOperation, "git_operation"},
		"candidate changed code":       {string(validationCodeCandidateChanged), "candidate_changed"},
		"context changed code":         {string(validationCodeContextChanged), "context_changed"},
		"worktree unavailable code":    {string(validationCodeWorktreeUnavailable), "worktree_unavailable"},
		"heal operation":               {healOrphanedReviewBaseOperation, "heal-orphaned-review-base"},
		"stale review operator actor":  {actorSystem, "system"},
		"update review default actor":  {actorHuman, "human"},
		"healed outcome":               {string(OrphanedBaseHealed), "healed"},
		"not applicable heal outcome":  {string(OrphanedBaseHealNotApplicable), "not_applicable"},
		"integration failed outcome":   {string(OrphanedBaseHealIntegrationFailed), "integration_failed"},
		"no effects lifecycle literal": {string(LifecycleEffectsNone), "none"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", name, pair[0], pair[1])
		}
	}
	if defaultReviewClaimCooldown != 60*time.Second {
		t.Errorf("review claim cooldown = %v, want 60s", defaultReviewClaimCooldown)
	}
}
