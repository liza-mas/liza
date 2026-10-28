package referencecontract

import "fmt"

// pinnedReference is a direct reference read at its pinned commit.
type pinnedReference struct {
	ref     Reference
	content string
}

// loadPinnedReference resolves the pin to a commit and reads the referenced
// file there.
func loadPinnedReference(repo ReviewRepository, ref DirectReference, pin string) (pinnedReference, error) {
	revision, err := repo.ResolveCommit(pin)
	if err != nil {
		// Probed only on this error path: a pin naming no commit in the
		// repository is the author's to fix; a failed probe, or a commit
		// that would not resolve, stays a Git failure (D-235).
		if objectType, present, probeErr := repo.ObjectType(pin); probeErr == nil {
			switch {
			case !present:
				return pinnedReference{}, invalidCarrier(fmt.Errorf("direct reference %q: pinned revision %s does not exist in this repository; pin a commit that contains %q", ref.ID, pin, ref.Path))
			case objectType != "commit":
				return pinnedReference{}, invalidCarrier(fmt.Errorf("direct reference %q: pinned revision %s is a %s, not a commit; pin a commit that contains %q", ref.ID, pin, objectType, ref.Path))
			}
		}
		return pinnedReference{}, err
	}
	content, err := repo.ReadBlob(revision, ref.Path)
	if err != nil {
		if _, present, probeErr := repo.TreePathMode(revision, ref.Path); probeErr == nil && !present {
			return pinnedReference{}, invalidCarrier(fmt.Errorf("direct reference %q: %q is not present at its pinned revision %s; pin a commit that contains it — a file added in this submission must be committed before the commit that pins it", ref.ID, ref.Path, revision))
		}
		return pinnedReference{}, err
	}
	pinnedOID, err := repo.BlobOID(revision, ref.Path)
	if err != nil {
		return pinnedReference{}, err
	}
	return pinnedReference{
		ref:     Reference{ID: ref.ID, Path: ref.Path, Heading: ref.Heading, Revision: revision, BlobOID: pinnedOID},
		content: content,
	}, nil
}

// locateFreshRevision decides where the reference's current text lives:
// integration HEAD, or the review commit for a path only the reviewed range
// introduced. presentAtHead is false when HEAD lacks the path.
func locateFreshRevision(repo ReviewRepository, head, localBase, localReview string, ref DirectReference, pinnedRevision string) (presentAtHead bool, freshRevision, where string, err error) {
	_, presentAtHead, err = repo.TreePathMode(head, ref.Path)
	if err != nil {
		return false, "", "", err
	}
	freshRevision, where = head, "integration HEAD"
	if !presentAtHead && localReview != "" {
		_, presentAtBase, baseErr := repo.TreePathMode(localBase, ref.Path)
		if baseErr != nil {
			return false, "", "", baseErr
		}
		if presentAtBase {
			return false, "", "", invalidCarrier(fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID))
		}
		_, presentAtReview, reviewErr := repo.TreePathMode(localReview, ref.Path)
		if reviewErr != nil {
			return false, "", "", reviewErr
		}
		if !presentAtReview {
			return false, "", "", invalidCarrier(fmt.Errorf("direct reference %q: %q is absent at integration HEAD, the range base and the review commit (pin %s); cite a file integration HEAD carries, or commit this one in the submission and re-pin the reference to that commit", ref.ID, ref.Path, pinnedRevision))
		}
		freshRevision, where = localReview, "review commit"
	}
	return presentAtHead, freshRevision, where, nil
}

// keepPinned keeps the pinned text with its disclosure when the section cannot
// be read at the fresh revision, or refuses with refusal.
func keepPinned(pinned Reference, ref DirectReference, reason string, refusal error, refuses func(referenceID string) bool) (Reference, error) {
	if refuses(ref.ID) {
		return Reference{}, refusal
	}
	pinned.PinnedRevision, pinned.Unresolved = pinned.Revision, reason
	return pinned, nil
}

// classifyDrift compares the pinned section with the fresh one. Blob identity
// is the fast path; an unrelated edit elsewhere in the file is not drift.
// Drift refuses when refuses(referenceID) holds, otherwise the current section
// is kept with the pin it drifted from.
func classifyDrift(repo ReviewRepository, ref DirectReference, pinned Reference, freshRevision, where string, refuses func(referenceID string) bool) (Reference, error) {
	freshOID, err := repo.BlobOID(freshRevision, ref.Path)
	if err != nil {
		return Reference{}, err
	}
	if pinned.BlobOID == freshOID {
		return pinned, nil
	}
	freshContent, readErr := repo.ReadBlob(freshRevision, ref.Path)
	if readErr != nil {
		return Reference{}, readErr
	}
	freshSpan, spanErr := ExtractSection(freshContent, ref.Heading)
	if spanErr != nil {
		return keepPinned(pinned, ref, spanErr.Error(), invalidCarrier(fmt.Errorf("direct reference %q is stale at %s: %w", ref.ID, where, spanErr)), refuses)
	}
	if freshSpan == pinned.Span {
		return pinned, nil
	}
	if refuses(ref.ID) {
		return Reference{}, invalidCarrier(fmt.Errorf("direct reference %q is stale at %s", ref.ID, where))
	}
	return Reference{
		ID: ref.ID, Path: ref.Path, Heading: ref.Heading, Revision: freshRevision, BlobOID: freshOID,
		Span: freshSpan, PinnedRevision: pinned.Revision,
	}, nil
}

// resolveDirectReference reads one reference at its pin, extracts its section,
// and classifies drift against the fresh revision.
func resolveDirectReference(repo ReviewRepository, head, localBase, localReview, sourceRevision string, ref DirectReference, refuses func(referenceID string) bool) (Reference, error) {
	pinned, err := loadPinnedReference(repo, ref, ref.EffectiveRevision(sourceRevision))
	if err != nil {
		return Reference{}, err
	}
	presentAtHead, freshRevision, where, err := locateFreshRevision(repo, head, localBase, localReview, ref, pinned.ref.Revision)
	if err != nil {
		return Reference{}, err
	}
	pinned.ref.Span, err = ExtractSection(pinned.content, ref.Heading)
	if err != nil {
		return Reference{}, invalidCarrier(err)
	}
	if !presentAtHead && localReview == "" {
		return keepPinned(pinned.ref, ref, "path deleted", invalidCarrier(fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID)), refuses)
	}
	return classifyDrift(repo, ref, pinned.ref, freshRevision, where, refuses)
}
