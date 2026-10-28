package ops

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// referenceSection extracts the section a Direct Reference cites and bounds
// it. Line endings are normalized first, as for carrier spans, so the bound
// applies to the same bytes a section's identity is computed over and a CRLF
// conversion cannot change whether a reference is admissible.
func referenceSection(content, heading string) (string, error) {
	section, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), heading)
	if err != nil {
		return "", err
	}
	if len(section) > referencecontract.ReferenceSectionMaxBytes {
		return "", fmt.Errorf("cited section is %d bytes, over the %d-byte bound", len(section), referencecontract.ReferenceSectionMaxBytes)
	}
	return section, nil
}

// referenceBoundsRefusal resolves every Direct Reference of an acceptance
// carrier under the bounds claim adopts it with: the file under
// ReferenceFileMaxBytes, the cited section under ReferenceSectionMaxBytes.
// Claim and planning submission both call it, so a plan whose coding children
// claim would refuse is refused before review instead of after merge (D-171).
// It returns "" when every reference resolves; otherwise the reason, and
// whether it is a failed Git read rather than content the author can fix.
func referenceBoundsRefusal(root string, refs *referencecontract.Contract) (reason string, readFailure bool) {
	for _, direct := range refs.DirectReferences {
		revision := direct.EffectiveRevision(refs.SourceRevision)
		content, _, err := readReferenceBlob(root, revision, direct.Path)
		if err != nil {
			var refused *acceptanceArtifactRefusal
			return "cannot resolve pinned reference " + direct.ID + ": " + err.Error(), !errors.As(err, &refused)
		}
		if _, err := referenceSection(content, direct.Heading); err != nil {
			var heading *referencecontract.HeadingMatchError
			if errors.As(err, &heading) {
				return "cannot resolve exact heading for reference " + direct.ID + ": " + err.Error(), false
			}
			return fmt.Sprintf("reference %s: %v", direct.ID, err), false
		}
	}
	return "", false
}

// carrierSpanIdentity is the identity of an allocation: the object id of the
// section a verdict covered, not of the file that carries it.
//
// AcceptanceSource.Blob holds this at every boundary — allocation, candidate
// submission, reviewer assignment — so all three agree on what "the reviewed
// source changed" means. Holding a whole-file object id at one boundary and a
// span at another is what lets an unrelated edit strand a child at whichever
// boundary still reads the file.
//
// It is computed exactly as Git hashes a blob, so the value stays a 40-char
// lowercase object id like every other identifier in AcceptanceSource, and
// `git hash-object` on the extracted section reproduces it. A digest of another
// shape would hold, but it would also make the field mean two things at once
// and put it at odds with the state validator's object-id rule.
func carrierSpanIdentity(content, heading string) (string, bool) {
	span, ok := carrierSpan(content, heading)
	if !ok {
		return "", false
	}
	return spanObjectID(span), true
}

// spanObjectID is the Git object id of a section's bytes.
//
// sha1 here is a content address, not an integrity digest: it is what Git
// computes for these same bytes, so `git hash-object` on the extracted section
// reproduces it. The sha256 elsewhere in this file, over canonical commands, is
// the integrity digest. Different jobs — do not unify them.
func spanObjectID(span string) string {
	hasher := sha1.New()
	fmt.Fprintf(hasher, "blob %d\x00", len(span))
	hasher.Write([]byte(span))
	return hex.EncodeToString(hasher.Sum(nil))
}

// carrierSpan isolates the part of a carrier a reviewer's verdict covers: the
// section named by the reference heading, or the whole file when the reference
// names none. Returns false when the heading cannot be resolved uniquely, so
// an ambiguous or missing heading refuses rather than allocating on a guess.
//
// Line endings are normalized on both sides of every comparison, matching the
// parser: a CRLF conversion at integration is a transport change, not a change
// to what a reviewer approved.
func carrierSpan(content, heading string) (string, bool) {
	if heading == "" {
		return content, true
	}
	section, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), heading)
	if err != nil {
		return "", false
	}
	return section, true
}

// acceptanceCarrierSpan reads a carrier at one commit and isolates the same
// section. A carrier absent at that commit yields false, never an empty match.
func acceptanceCarrierSpan(root, commit, path, heading string) (string, bool) {
	content, _, err := readAcceptanceBlob(root, commit, path)
	if err != nil {
		return "", false
	}
	return carrierSpan(content, heading)
}

// sameBlobAt reports whether path resolves to the same blob at both commits.
// Any lookup failure answers false, so callers fall through to their full
// comparison exactly as when either side cannot be resolved. Both sides are
// resolved by one git process.
func sameBlobAt(g *git.Git, commitA, commitB, path string) bool {
	oids, err := g.BlobOIDs([]git.BlobPath{{Revision: commitA, Path: path}, {Revision: commitB, Path: path}})
	if err != nil {
		return false
	}
	return oids[0].Err == nil && oids[1].Err == nil && oids[0].OID == oids[1].OID
}

// acceptanceArtifactRefusal is a readBoundedBlob refusal decided by the path
// or the committed tree entry, so an unchanged commit is refused identically.
// Any other reader error is a failed Git read.
type acceptanceArtifactRefusal struct{ reason string }

func (r *acceptanceArtifactRefusal) Error() string { return r.reason }

// readAcceptanceBlob bounds the object before reading it and never consults
// the worktree or follows symlinks. Used for carriers and manifests; Direct
// Reference targets go through readReferenceBlob.
func readAcceptanceBlob(root, commit, path string) (string, string, error) {
	return readBoundedBlob(root, commit, path, referencecontract.AcceptanceMaxBytes)
}

// readReferenceBlob reads a Direct Reference's file under the larger file cap;
// referenceSection then bounds the section the reference actually cites.
func readReferenceBlob(root, commit, path string) (string, string, error) {
	return readBoundedBlob(root, commit, path, referencecontract.ReferenceFileMaxBytes)
}

func readBoundedBlob(root, commit, path string, limit int64) (string, string, error) {
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		return "", "", &acceptanceArtifactRefusal{"invalid acceptance artifact path"}
	}
	g := git.New(root)
	// One ls-tree reports mode, type, object ID and size together; the blob
	// itself is read only after the size bound holds.
	entry, present, err := g.TreeEntryAt(commit, path)
	return readBoundedEntry(g, path, entry, present, err, limit)
}

// readAcceptanceEntry applies readAcceptanceBlob's bounds to a tree entry the
// caller already looked up, so a caller that inspected the entry itself does
// not pay a second ls-tree for the same path.
func readAcceptanceEntry(g *git.Git, path string, entry git.TreeEntry, present bool, err error) (string, string, error) {
	return readBoundedEntry(g, path, entry, present, err, referencecontract.AcceptanceMaxBytes)
}

func readBoundedEntry(g *git.Git, path string, entry git.TreeEntry, present bool, err error, limit int64) (string, string, error) {
	if err != nil {
		return "", "", fmt.Errorf("cannot inspect acceptance artifact: %s: %w", path, err)
	}
	if !present || (entry.Mode != gitModeRegularFile && entry.Mode != gitModeExecutableFile) {
		return "", "", &acceptanceArtifactRefusal{"acceptance artifact must be a present regular committed file: " + path}
	}
	if entry.Type != git.ObjectTypeBlob || entry.OID == "" {
		return "", "", &acceptanceArtifactRefusal{"cannot resolve acceptance artifact: " + path}
	}
	blob := entry.OID
	if entry.Size < 0 {
		return "", "", &acceptanceArtifactRefusal{"acceptance artifact cannot be sized: " + path}
	}
	if entry.Size > limit {
		return "", "", &acceptanceArtifactRefusal{fmt.Sprintf("acceptance artifact exceeds %d KiB (%d bytes): %s", limit/1024, entry.Size, path)}
	}
	content, err := g.ReadBlobObject(blob)
	if err != nil {
		return "", "", fmt.Errorf("cannot read acceptance artifact: %s: %w", path, err)
	}
	return content, blob, nil
}
