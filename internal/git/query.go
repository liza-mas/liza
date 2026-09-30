package git

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/gitenv"
)

// TreePathMode returns the raw Git object mode for a path in a treeish.
func (g *Git) TreePathMode(treeish, path string) (mode string, present bool, err error) {
	if path == "" {
		return "", false, fmt.Errorf("tree path mode requires a non-empty path")
	}
	output, err := g.exec("ls-tree", "--full-tree", "-z", treeish, "--", literalPathspec(path))
	if err != nil {
		return "", false, fmt.Errorf("failed to query tree path mode for %q at %q: %w", path, treeish, err)
	}
	if output == "" {
		return "", false, nil
	}

	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(records) != 1 {
		return "", false, fmt.Errorf("expected one tree entry for %q at %q, got %d", path, treeish, len(records))
	}

	mode, _, ok := strings.Cut(records[0], " ")
	if !ok || mode == "" {
		return "", false, fmt.Errorf("malformed tree entry for %q at %q: %q", path, treeish, records[0])
	}
	return mode, true, nil
}

// TreeEntry is one path in a treeish as reported by `git ls-tree -l`.
// Size is -1 for entries git does not size (trees and gitlinks).
type TreeEntry struct {
	Mode string
	Type string
	OID  string
	Size int64
}

// TreeEntryAt returns mode, type, object ID and size for a path in a treeish
// from a single ls-tree process, so a caller that needs all four does not
// spawn one git process per attribute.
func (g *Git) TreeEntryAt(treeish, path string) (entry TreeEntry, present bool, err error) {
	if path == "" {
		return TreeEntry{}, false, fmt.Errorf("tree entry requires a non-empty path")
	}
	output, err := g.exec("ls-tree", "--full-tree", "-l", "-z", treeish, "--", literalPathspec(path))
	if err != nil {
		return TreeEntry{}, false, fmt.Errorf("failed to query tree entry for %q at %q: %w", path, treeish, err)
	}
	if output == "" {
		return TreeEntry{}, false, nil
	}

	records := strings.Split(strings.TrimSuffix(output, "\x00"), "\x00")
	if len(records) != 1 {
		return TreeEntry{}, false, fmt.Errorf("expected one tree entry for %q at %q, got %d", path, treeish, len(records))
	}

	meta, _, ok := strings.Cut(records[0], "\t")
	fields := strings.Fields(meta)
	if !ok || len(fields) != 4 {
		return TreeEntry{}, false, fmt.Errorf("malformed tree entry for %q at %q: %q", path, treeish, records[0])
	}
	entry = TreeEntry{Mode: fields[0], Type: fields[1], OID: fields[2], Size: -1}
	if fields[3] != "-" {
		size, parseErr := strconv.ParseInt(fields[3], 10, 64)
		if parseErr != nil || size < 0 {
			return TreeEntry{}, false, fmt.Errorf("malformed tree entry size for %q at %q: %q", path, treeish, records[0])
		}
		entry.Size = size
	}
	return entry, true, nil
}

// ReadBlobObject returns the exact bytes of the blob with the given object ID,
// typically one just reported by TreeEntryAt. It refuses non-blob objects.
func (g *Git) ReadBlobObject(oid string) (string, error) {
	if oid == "" || strings.HasPrefix(oid, "-") {
		return "", fmt.Errorf("blob read requires an object ID, got %q", oid)
	}
	output, err := gitenv.CombinedOutput(g.projectRoot, "cat-file", "blob", oid)
	if err != nil {
		return "", fmt.Errorf("failed to read blob %s: %w\nOutput: %s", oid, err, output)
	}
	return string(output), nil
}

func literalPathspec(path string) string {
	return ":(literal)" + path
}

// ResolveCommit resolves ref to a full commit object ID in the project
// repository. --end-of-options keeps untrusted ref text out of Git's option
// parser.
func (g *Git) ResolveCommit(ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("commit resolution requires a non-empty ref")
	}
	commit, err := g.exec("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to resolve commit ref %q: %w", ref, err)
	}
	return commit, nil
}

// ReadBlob returns the exact bytes of path at revision without consulting the
// working tree. It deliberately bypasses exec, whose text-oriented contract
// trims leading and trailing whitespace.
func (g *Git) ReadBlob(revision, path string) (string, error) {
	if revision == "" || path == "" {
		return "", fmt.Errorf("blob read requires a non-empty revision and path")
	}
	spec := revision + ":" + path
	if !catFileBatchable(spec) {
		return g.readBlobByArgs(revision, path)
	}
	// One cat-file --batch process resolves the name, reports the type, and
	// streams the bytes; the type check below keeps the blob-only contract.
	objects, err := g.catFileBatch([]string{spec}, true)
	if err != nil {
		return "", fmt.Errorf("failed to read blob %q at %q: %w", path, revision, err)
	}
	object := objects[0]
	if object.Missing {
		return "", fmt.Errorf("failed to resolve blob OID for %q at %q: %s", path, revision, g.missingBlobReason(revision, object.Status))
	}
	if object.Type != "blob" {
		return "", fmt.Errorf("object for %q at %q is %s, not a blob", path, revision, object.Type)
	}
	return string(object.Content), nil
}

func (g *Git) readBlobByArgs(revision, path string) (string, error) {
	oid, err := g.blobOIDByArgs(revision, path)
	if err != nil {
		return "", err
	}
	output, err := gitenv.CombinedOutput(g.projectRoot,
		"cat-file", "blob", oid)
	if err != nil {
		return "", fmt.Errorf("failed to read blob %q at %q: %w\nOutput: %s", path, revision, err, output)
	}
	return string(output), nil
}

// BlobOID returns the blob object ID for path at revision without consulting
// the working tree.
func (g *Git) BlobOID(revision, path string) (string, error) {
	oids, err := g.BlobOIDs([]BlobPath{{Revision: revision, Path: path}})
	if err != nil {
		return "", err
	}
	return oids[0].OID, oids[0].Err
}

// BlobPath names one path at one revision.
type BlobPath struct {
	Revision string
	Path     string
}

// ObjectIDResult is the answer for one batched lookup: either OID or Err is set.
type ObjectIDResult struct {
	OID string
	Err error
}

// BlobOIDs resolves several revision:path pairs to blob object IDs in a single
// git process. Each entry carries its own error, exactly as BlobOID would
// report it, so callers comparing two revisions of one file pay one process
// instead of four. The returned error is reserved for failures of the lookup
// itself (git could not run or answered malformed output).
func (g *Git) BlobOIDs(lookups []BlobPath) ([]ObjectIDResult, error) {
	results := make([]ObjectIDResult, len(lookups))
	var names []string
	var batched []int
	for i, lookup := range lookups {
		if lookup.Revision == "" || lookup.Path == "" {
			results[i].Err = fmt.Errorf("blob OID lookup requires a non-empty revision and path")
			continue
		}
		spec := lookup.Revision + ":" + lookup.Path
		if !catFileBatchable(spec) {
			results[i].OID, results[i].Err = g.blobOIDByArgs(lookup.Revision, lookup.Path)
			continue
		}
		names = append(names, spec)
		batched = append(batched, i)
	}
	if len(names) == 0 {
		return results, nil
	}
	objects, err := g.catFileBatch(names, false)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve blob OIDs: %w", err)
	}
	for j, object := range objects {
		i := batched[j]
		lookup := lookups[i]
		switch {
		case object.Missing:
			results[i].Err = fmt.Errorf("failed to resolve blob OID for %q at %q: %s", lookup.Path, lookup.Revision, g.missingBlobReason(lookup.Revision, object.Status))
		case object.Type != "blob":
			results[i].Err = fmt.Errorf("object for %q at %q is %s, not a blob", lookup.Path, lookup.Revision, object.Type)
		default:
			results[i].OID = object.OID
		}
	}
	return results, nil
}

// missingBlobReason says why a revision:path name did not resolve. cat-file
// answers "missing" whether the revision or only the path is absent, and the
// remedies differ, so one more lookup of the revision's tree decides; it runs
// only on this error path. Any other status, or a failed probe, is reported
// as Git gave it.
func (g *Git) missingBlobReason(revision, status string) string {
	if status != "missing" {
		return "object " + status
	}
	objects, err := g.catFileBatch([]string{revision + "^{tree}"}, false)
	if err != nil {
		return "object missing"
	}
	if objects[0].Missing {
		return "revision not found"
	}
	return "path not present at that revision"
}

// blobOIDByArgs is the argument-based lookup kept for names a batch stdin line
// cannot carry.
func (g *Git) blobOIDByArgs(revision, path string) (string, error) {
	spec := revision + ":" + path
	oid, err := g.exec("rev-parse", "--verify", "--end-of-options", spec)
	if err != nil {
		return "", fmt.Errorf("failed to resolve blob OID for %q at %q: %w", path, revision, err)
	}
	objectType, err := g.exec("cat-file", "-t", oid)
	if err != nil {
		return "", fmt.Errorf("failed to inspect object for %q at %q: %w", path, revision, err)
	}
	if objectType != "blob" {
		return "", fmt.Errorf("object for %q at %q is %s, not a blob", path, revision, objectType)
	}
	return oid, nil
}

// ResolveCommits resolves several refs to full commit object IDs in a single
// git process. Each entry carries its own error, as ResolveCommit would report
// it; the returned error is reserved for failures of the lookup itself. Refs
// are read from stdin, so they never reach git's option parser.
func (g *Git) ResolveCommits(refs []string) ([]ObjectIDResult, error) {
	results := make([]ObjectIDResult, len(refs))
	var names []string
	var batched []int
	for i, ref := range refs {
		if ref == "" {
			results[i].Err = fmt.Errorf("commit resolution requires a non-empty ref")
			continue
		}
		name := ref + "^{commit}"
		if !catFileBatchable(name) {
			results[i].OID, results[i].Err = g.ResolveCommit(ref)
			continue
		}
		names = append(names, name)
		batched = append(batched, i)
	}
	if len(names) == 0 {
		return results, nil
	}
	objects, err := g.catFileBatch(names, false)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve commits: %w", err)
	}
	for j, object := range objects {
		i := batched[j]
		switch {
		case object.Missing:
			results[i].Err = fmt.Errorf("failed to resolve commit ref %q: object %s", refs[i], object.Status)
		case object.Type != "commit":
			results[i].Err = fmt.Errorf("failed to resolve commit ref %q: resolved to %s", refs[i], object.Type)
		default:
			results[i].OID = object.OID
		}
	}
	return results, nil
}

// CalculateDrift returns the number of commits between baseCommit and targetBranch
// This indicates how far the integration branch has moved since the task started
func (g *Git) CalculateDrift(baseCommit, targetBranch string) (int, error) {
	// git rev-list --count base..target
	output, err := g.exec("rev-list", "--count", baseCommit+".."+targetBranch)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate drift: %w", err)
	}

	count, err := strconv.Atoi(output)
	if err != nil {
		return 0, fmt.Errorf("failed to parse commit count: %w", err)
	}

	return count, nil
}

// IsAncestor checks if commitA is an ancestor of commitB
func (g *Git) IsAncestor(commitA, commitB string) (bool, error) {
	_, err := g.exec("merge-base", "--is-ancestor", commitA, commitB)
	if err != nil {
		// Check if error is "not an ancestor" (exit code 1)
		if strings.Contains(err.Error(), "exit status 1") {
			return false, nil
		}
		return false, fmt.Errorf("merge-base failed: %w", err)
	}
	return true, nil
}

// GetMergeBase returns the best common ancestor for two commits or refs.
func (g *Git) GetMergeBase(commitA, commitB string) (string, error) {
	base, err := g.exec("merge-base", commitA, commitB)
	if err != nil {
		return "", fmt.Errorf("merge-base failed: %w", err)
	}
	return base, nil
}

// GetWorktreeHEAD returns the commit SHA of the worktree's HEAD
func (g *Git) GetWorktreeHEAD(taskID string) (string, error) {
	wtPath := g.GetWorktreePath(taskID)
	return g.execInDir(wtPath, "rev-parse", "HEAD")
}

// ResolveWorktreeCommit resolves ref to a full commit SHA inside a task worktree.
func (g *Git) ResolveWorktreeCommit(taskID, ref string) (string, error) {
	wtPath := g.GetWorktreePath(taskID)
	commit, err := g.execInDir(wtPath, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to resolve worktree commit ref %q: %w", ref, err)
	}
	return commit, nil
}

// GetWorktreeBranch returns the current branch name in a worktree
func (g *Git) GetWorktreeBranch(wtPath string) (string, error) {
	branch, err := g.execInDir(wtPath, "branch", "--show-current")
	if err != nil {
		return "", fmt.Errorf("failed to get worktree branch: %w", err)
	}
	return branch, nil
}
