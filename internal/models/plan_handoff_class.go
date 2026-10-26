package models

// PlanHandoffClass classifies a merged planning task for the orchestrator's
// plan-to-children hand-off. One classification feeds wake rendering, the
// PLANNING_COMPLETE verifier, plan-check admission, and transition admission,
// so none of them can disagree about what a plan still needs.
type PlanHandoffClass string

const (
	// PlanHandoffNotSource: no hand-off left to run (see Pending).
	PlanHandoffNotSource PlanHandoffClass = ""
	// PlanHandoffOutOfDomain: a source with no reviewed hand-off (auto-only,
	// many-to-one or empty output). Behaves as before D73.
	PlanHandoffOutOfDomain PlanHandoffClass = "out_of_domain"
	// PlanHandoffNeedsReview: in domain, no disposition yet.
	PlanHandoffNeedsReview PlanHandoffClass = "needs_review"
	// PlanHandoffPassed: passed and every in-domain upstream is admissible.
	PlanHandoffPassed PlanHandoffClass = "passed"
	// PlanHandoffWaitingUpstream: passed, but an in-domain upstream still
	// awaits its own review.
	PlanHandoffWaitingUpstream PlanHandoffClass = "waiting_upstream"
	// PlanHandoffNeedsReconciliation: passed, but an in-domain upstream was
	// replanned or held afterwards; the plan must be replanned or held.
	PlanHandoffNeedsReconciliation PlanHandoffClass = "needs_reconciliation"
	// PlanHandoffHeld: held for a human action.
	PlanHandoffHeld PlanHandoffClass = "held"
	// A terminal pending correction needs adoption or replacement, while the
	// original remains fenced against generation.
	PlanHandoffAmendmentReady PlanHandoffClass = "amendment_ready"
)
