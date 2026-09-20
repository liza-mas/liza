package models

import "time"

// ProofReaffirmation records an authorized decision that an approved proof
// still holds against content that changed after the approval.
//
// It exists because the acceptance boundary cannot tell a legitimate extension
// of a referenced section from a substitution, and so refuses both. Refusing is
// right; having no way back is not. Without this record the only exits are
// superseding the child — which discards every obligation its contract
// allocated, not just the one in question — or re-reviewing a merged plan.
//
// It authorizes exactly one transition: from the section identity the reviewer
// approved to the identity it was re-affirmed against. A later change to that
// section refuses again. That is deliberate — a standing waiver on the
// reference would silently cover the substitution the boundary exists to catch.
type ProofReaffirmation struct {
	ParentTask      string    `yaml:"parent_task" json:"parent_task"`
	CarrierPath     string    `yaml:"carrier_path" json:"carrier_path"`
	Heading         string    `yaml:"heading" json:"heading"`
	ReferenceID     string    `yaml:"reference_id" json:"reference_id"`
	ReviewedSection string    `yaml:"reviewed_section" json:"reviewed_section"`
	CurrentSection  string    `yaml:"current_section" json:"current_section"`
	Actor           string    `yaml:"actor" json:"actor"`
	Timestamp       time.Time `yaml:"timestamp" json:"timestamp"`
	Reason          string    `yaml:"reason" json:"reason"`
}

// Authorizes reports whether this record covers the transition actually
// observed. Every field must match: the allocation it was granted for, the
// reference it names, and both section identities.
func (r ProofReaffirmation) Authorizes(parentTask, carrierPath, heading, referenceID, reviewed, current string) bool {
	return r.ParentTask == parentTask &&
		r.CarrierPath == carrierPath &&
		r.Heading == heading &&
		r.ReferenceID == referenceID &&
		r.ReviewedSection == reviewed &&
		r.CurrentSection == current
}

// FindProofReaffirmation returns the record authorizing this transition, or nil.
func (s *State) FindProofReaffirmation(parentTask, carrierPath, heading, referenceID, reviewed, current string) *ProofReaffirmation {
	for i := range s.ProofReaffirmations {
		if s.ProofReaffirmations[i].Authorizes(parentTask, carrierPath, heading, referenceID, reviewed, current) {
			return &s.ProofReaffirmations[i]
		}
	}
	return nil
}
