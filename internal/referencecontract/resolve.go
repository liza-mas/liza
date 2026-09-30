package referencecontract

import (
	"fmt"
	"path/filepath"
	"sort"
)

// InvalidCarrierError marks a failure caused by carrier content its author can
// correct — grammar, a heading, a pin, or drift under it — as opposed to a Git
// failure, which editing the carrier would not fix. Its text is the wrapped
// error's, so wrapping never changes what a caller reports.
type InvalidCarrierError struct {
	Err error
}

func (e *InvalidCarrierError) Error() string { return e.Err.Error() }

func (e *InvalidCarrierError) Unwrap() error { return e.Err }

func invalidCarrier(err error) error { return &InvalidCarrierError{Err: err} }

// ReviewRepository extends Repository with the tree and diff queries needed to
// discover strict carriers inside a reviewed range.
type ReviewRepository interface {
	Repository
	DiffFiles(dir, commitA, commitB string) ([]string, error)
	TreePathMode(treeish, path string) (mode string, present bool, err error)
}

// ProofPolicy answers, for a merged carrier observed at path with the given
// contents, which declared reference IDs still refuse section drift under
// their pin.
type ProofPolicy func(path string, contents ...string) func(referenceID string) bool

// RefuseEveryDrift refuses drift under every declared reference.
func RefuseEveryDrift(string) bool { return true }

// RefuseNoDrift discloses drift under every declared reference instead of refusing it.
func RefuseNoDrift(string) bool { return false }

// LoadDiffCarriers discovers the strict carriers among the Markdown files
// changed in base..review, reads them at review, and resolves their declared
// references against head. requireFresh marks a merged parent's range, whose
// carriers are compared with head and whose drift proofs decides; otherwise
// the range is the current review, whose references may name paths it
// introduced and whose drift always refuses.
func LoadDiffCarriers(repo ReviewRepository, root, base, review, head string, class CarrierClass, requireFresh bool, proofs ProofPolicy) ([]Carrier, error) {
	changed, err := repo.DiffFiles(root, base, review)
	if err != nil {
		return nil, fmt.Errorf("enumerate reviewed range %s..%s: %w", base, review, err)
	}
	sort.Strings(changed)
	var observations []Carrier
	for _, path := range changed {
		if filepath.Ext(path) != ".md" {
			continue
		}
		mode, present, modeErr := repo.TreePathMode(review, path)
		if modeErr != nil {
			return nil, modeErr
		}
		if !present || (mode != "100644" && mode != "100755") {
			continue
		}
		content, readErr := repo.ReadBlob(review, path)
		if readErr != nil {
			return nil, readErr
		}
		contract, parseErr := Parse(content)
		if parseErr != nil {
			return nil, invalidCarrier(fmt.Errorf("parse reviewed carrier %q: %w", path, parseErr))
		}
		if contract == nil {
			continue
		}
		oid, oidErr := repo.BlobOID(review, path)
		if oidErr != nil {
			return nil, oidErr
		}
		revision := review
		// A current-review carrier is unmerged: its author can still re-pin, so
		// every drift under it refuses. A merged parent's pins cannot move.
		refuses := RefuseEveryDrift
		if requireFresh {
			reviewedContent := content
			// A merged parent's carrier can be edited by later merges. The
			// integrated version is the current agreed content, so adopt it
			// rather than refusing to build context; only deletion blocks.
			_, presentAtHead, headErr := repo.TreePathMode(head, path)
			if headErr != nil {
				return nil, fmt.Errorf("freshness check %q: %w", path, headErr)
			}
			if !presentAtHead {
				return nil, invalidCarrier(fmt.Errorf("reviewed carrier %q was deleted at integration HEAD", path))
			}
			headOID, headErr := repo.BlobOID(head, path)
			if headErr != nil {
				return nil, fmt.Errorf("freshness check %q: %w", path, headErr)
			}
			if oid != headOID {
				headContent, readHeadErr := repo.ReadBlob(head, path)
				if readHeadErr != nil {
					return nil, readHeadErr
				}
				headContract, parseHeadErr := Parse(headContent)
				if parseHeadErr != nil {
					return nil, invalidCarrier(fmt.Errorf("parse reviewed carrier %q at integration HEAD: %w", path, parseHeadErr))
				}
				if headContract == nil {
					return nil, invalidCarrier(fmt.Errorf("reviewed carrier %q lost its Source References at integration HEAD", path))
				}
				content, contract, oid, revision = headContent, headContract, headOID, head
			}
			refuses = proofs(path, reviewedContent, content)
		}
		// Current-review carriers may declare references to paths their own
		// reviewed range introduced; those resolve at the review commit.
		localBase, localReview := "", ""
		if !requireFresh {
			localBase, localReview = base, review
		}
		refs, resolveErr := ResolveDirectReferences(repo, head, localBase, localReview, contract, refuses)
		if resolveErr != nil {
			return nil, fmt.Errorf("reviewed carrier %q: %w", path, resolveErr)
		}
		observations = append(observations, Carrier{Path: path, Span: content, Revision: revision, Class: class, BlobOID: oid, Refs: refs})
	}
	return observations, nil
}

// ResolveDirectReferences pins each direct reference at its declared revision
// and compares the referenced section with integration HEAD (blob identity is
// the fast path; an unrelated edit elsewhere in the file is not drift). When
// localBase/localReview are non-empty, a path absent at both HEAD and
// localBase was introduced in the reviewed range and matches at localReview
// instead; a path present at localBase but absent at HEAD was deleted.
//
// Drift refuses when refuses(referenceID) holds. Otherwise the reference is
// kept with its drift disclosed (ADR-0133): the current section when it still
// resolves, the pinned one with the reason when it does not. A pin that cannot
// be read refuses regardless — there is nothing to disclose.
func ResolveDirectReferences(repo ReviewRepository, head, localBase, localReview string, contract *Contract, refuses func(referenceID string) bool) ([]Reference, error) {
	refs := make([]Reference, 0, len(contract.DirectReferences))
	for _, ref := range contract.DirectReferences {
		// An unresolvable pin stays operational: this interface cannot tell a
		// revision Git lacks from a Git failure. Upgrade trigger: a lookup that
		// reports absence distinctly.
		revision, err := repo.ResolveCommit(ref.EffectiveRevision(contract.SourceRevision))
		if err != nil {
			return nil, err
		}
		content, err := repo.ReadBlob(revision, ref.Path)
		if err != nil {
			if _, present, probeErr := repo.TreePathMode(revision, ref.Path); probeErr == nil && !present {
				return nil, invalidCarrier(fmt.Errorf("direct reference %q: %q is not present at its pinned revision %s; pin a commit that contains it — a file added in this submission must be committed before the commit that pins it", ref.ID, ref.Path, revision))
			}
			return nil, err
		}
		pinnedOID, err := repo.BlobOID(revision, ref.Path)
		if err != nil {
			return nil, err
		}
		pinned := Reference{Path: ref.Path, Heading: ref.Heading, Revision: revision, BlobOID: pinnedOID}
		// unresolvable keeps the pinned text when the section cannot be read
		// at the fresh revision, or refuses with err.
		unresolvable := func(reason string, err error) error {
			if refuses(ref.ID) {
				return err
			}
			pinned.PinnedRevision, pinned.Unresolved = revision, reason
			refs = append(refs, pinned)
			return nil
		}
		_, presentAtHead, err := repo.TreePathMode(head, ref.Path)
		if err != nil {
			return nil, err
		}
		freshRevision, where := head, "integration HEAD"
		if !presentAtHead && localReview != "" {
			_, presentAtBase, baseErr := repo.TreePathMode(localBase, ref.Path)
			if baseErr != nil {
				return nil, baseErr
			}
			if presentAtBase {
				return nil, invalidCarrier(fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID))
			}
			freshRevision, where = localReview, "review commit"
		}
		pinned.Span, err = ExtractSection(content, ref.Heading)
		if err != nil {
			return nil, invalidCarrier(err)
		}
		if !presentAtHead && localReview == "" {
			if err := unresolvable("path deleted", invalidCarrier(fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID))); err != nil {
				return nil, err
			}
			continue
		}
		freshOID, err := repo.BlobOID(freshRevision, ref.Path)
		if err != nil {
			return nil, err
		}
		if pinnedOID != freshOID {
			freshContent, readErr := repo.ReadBlob(freshRevision, ref.Path)
			if readErr != nil {
				return nil, readErr
			}
			freshSpan, spanErr := ExtractSection(freshContent, ref.Heading)
			if spanErr != nil {
				if err := unresolvable(spanErr.Error(), invalidCarrier(fmt.Errorf("direct reference %q is stale at %s: %w", ref.ID, where, spanErr))); err != nil {
					return nil, err
				}
				continue
			}
			if freshSpan != pinned.Span {
				if refuses(ref.ID) {
					return nil, invalidCarrier(fmt.Errorf("direct reference %q is stale at %s", ref.ID, where))
				}
				refs = append(refs, Reference{
					Path: ref.Path, Heading: ref.Heading, Revision: freshRevision, BlobOID: freshOID,
					Span: freshSpan, PinnedRevision: revision,
				})
				continue
			}
		}
		refs = append(refs, pinned)
	}
	return refs, nil
}
