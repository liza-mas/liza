package referencecontract

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeReviewRepo is a deterministic in-memory ReviewRepository that records
// every call, so tests can pin the order of repository reads.
type fakeReviewRepo struct {
	commits     map[string]string // pin -> commit
	files       map[string]string // revision+"\x00"+path -> content
	objectTypes map[string]string // oid -> type
	probeErr    error
	calls       []string
}

func newFakeReviewRepo() *fakeReviewRepo {
	return &fakeReviewRepo{
		commits:     map[string]string{},
		files:       map[string]string{},
		objectTypes: map[string]string{},
	}
}

func (r *fakeReviewRepo) commit(pin, revision string) { r.commits[pin] = revision }

func (r *fakeReviewRepo) file(revision, path, content string) {
	r.files[revision+"\x00"+path] = content
}

func (r *fakeReviewRepo) record(format string, args ...any) {
	r.calls = append(r.calls, fmt.Sprintf(format, args...))
}

func (r *fakeReviewRepo) ResolveCommit(ref string) (string, error) {
	r.record("ResolveCommit(%s)", ref)
	revision, ok := r.commits[ref]
	if !ok {
		return "", errors.New("unknown revision " + ref)
	}
	return revision, nil
}

func (r *fakeReviewRepo) ReadBlob(revision, path string) (string, error) {
	r.record("ReadBlob(%s,%s)", revision, path)
	content, ok := r.files[revision+"\x00"+path]
	if !ok {
		return "", errors.New("blob missing")
	}
	return content, nil
}

func (r *fakeReviewRepo) BlobOID(revision, path string) (string, error) {
	r.record("BlobOID(%s,%s)", revision, path)
	content, ok := r.files[revision+"\x00"+path]
	if !ok {
		return "", errors.New("oid missing")
	}
	return "oid:" + content, nil
}

func (r *fakeReviewRepo) TreePathMode(treeish, path string) (string, bool, error) {
	r.record("TreePathMode(%s,%s)", treeish, path)
	if r.probeErr != nil {
		return "", false, r.probeErr
	}
	_, ok := r.files[treeish+"\x00"+path]
	return "100644", ok, nil
}

func (r *fakeReviewRepo) ObjectType(oid string) (string, bool, error) {
	r.record("ObjectType(%s)", oid)
	if r.probeErr != nil {
		return "", false, r.probeErr
	}
	objectType, ok := r.objectTypes[oid]
	return objectType, ok, nil
}

func (r *fakeReviewRepo) DiffFiles(dir, commitA, commitB string) ([]string, error) {
	return nil, errors.New("unused")
}

func resolveContract(refs ...DirectReference) *Contract {
	return &Contract{SourceRevision: "pin", DirectReferences: refs}
}

func refuseAll(string) bool  { return true }
func refuseNone(string) bool { return false }

func requireInvalidCarrier(t *testing.T, err error, want string) {
	t.Helper()
	var invalid *InvalidCarrierError
	if !errors.As(err, &invalid) {
		t.Fatalf("error = %v, want InvalidCarrierError", err)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func requireCalls(t *testing.T, repo *fakeReviewRepo, want ...string) {
	t.Helper()
	if got := strings.Join(repo.calls, " "); got != strings.Join(want, " ") {
		t.Fatalf("repository calls:\n got  %s\n want %s", got, strings.Join(want, " "))
	}
}

var docRef = DirectReference{ID: "doc", Path: "a.md", Heading: "Sec"}

func TestResolveDirectReferencesUndriftedReadsInOrder(t *testing.T) {
	repo := newFakeReviewRepo()
	repo.commit("pin", "pinrev")
	repo.file("pinrev", "a.md", "## Sec\nbody\n")
	repo.file("head", "a.md", "## Sec\nbody\n")

	refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
	if err != nil {
		t.Fatal(err)
	}
	want := Reference{ID: "doc", Path: "a.md", Heading: "Sec", Revision: "pinrev", BlobOID: "oid:## Sec\nbody\n", Span: "## Sec\nbody\n"}
	if len(refs) != 1 || refs[0] != want {
		t.Fatalf("refs = %+v, want [%+v]", refs, want)
	}
	requireCalls(t, repo,
		"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)",
		"TreePathMode(head,a.md)", "BlobOID(head,a.md)")
}

func TestResolveDirectReferencesUsesReferenceRevisionOverride(t *testing.T) {
	repo := newFakeReviewRepo()
	repo.commit("override", "overriderev")
	repo.file("overriderev", "a.md", "## Sec\nbody\n")
	repo.file("head", "a.md", "## Sec\nbody\n")
	ref := docRef
	ref.Revision = "override"

	refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(ref), refuseAll)
	if err != nil {
		t.Fatal(err)
	}
	if refs[0].Revision != "overriderev" {
		t.Fatalf("revision = %q, want overriderev", refs[0].Revision)
	}
	if repo.calls[0] != "ResolveCommit(override)" {
		t.Fatalf("first call = %q, want ResolveCommit(override)", repo.calls[0])
	}
}

func TestResolveDirectReferencesMissingPinProbesOnlyOnErrorPath(t *testing.T) {
	t.Run("absent object is the author's to fix", func(t *testing.T) {
		repo := newFakeReviewRepo()
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		requireInvalidCarrier(t, err, `direct reference "doc": pinned revision pin does not exist in this repository; pin a commit that contains "a.md"`)
		requireCalls(t, repo, "ResolveCommit(pin)", "ObjectType(pin)")
	})
	t.Run("non-commit object", func(t *testing.T) {
		repo := newFakeReviewRepo()
		repo.objectTypes["pin"] = "blob"
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		requireInvalidCarrier(t, err, `pinned revision pin is a blob, not a commit`)
		requireCalls(t, repo, "ResolveCommit(pin)", "ObjectType(pin)")
	})
	t.Run("commit that will not resolve stays a Git failure", func(t *testing.T) {
		repo := newFakeReviewRepo()
		repo.objectTypes["pin"] = "commit"
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		var invalid *InvalidCarrierError
		if err == nil || errors.As(err, &invalid) || err.Error() != "unknown revision pin" {
			t.Fatalf("error = %v, want the raw ResolveCommit error", err)
		}
	})
	t.Run("failed probe stays a Git failure", func(t *testing.T) {
		repo := newFakeReviewRepo()
		repo.probeErr = errors.New("probe failed")
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		var invalid *InvalidCarrierError
		if err == nil || errors.As(err, &invalid) || err.Error() != "unknown revision pin" {
			t.Fatalf("error = %v, want the raw ResolveCommit error", err)
		}
	})
}

func TestResolveDirectReferencesFileMissingAtPin(t *testing.T) {
	t.Run("absent path is the author's to fix", func(t *testing.T) {
		repo := newFakeReviewRepo()
		repo.commit("pin", "pinrev")
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		requireInvalidCarrier(t, err, `"a.md" is not present at its pinned revision pinrev; pin a commit that contains it`)
		requireCalls(t, repo, "ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "TreePathMode(pinrev,a.md)")
	})
	t.Run("failed probe returns the read error", func(t *testing.T) {
		repo := newFakeReviewRepo()
		repo.commit("pin", "pinrev")
		repo.probeErr = errors.New("probe failed")
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		var invalid *InvalidCarrierError
		if err == nil || errors.As(err, &invalid) || err.Error() != "blob missing" {
			t.Fatalf("error = %v, want the raw ReadBlob error", err)
		}
	})
}

func TestResolveDirectReferencesPinnedHeadingMissingPrecedesFreshReads(t *testing.T) {
	repo := newFakeReviewRepo()
	repo.commit("pin", "pinrev")
	repo.file("pinrev", "a.md", "## Other\nbody\n")
	repo.file("head", "a.md", "## Sec\nbody\n")

	refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseNone)
	if refs != nil {
		t.Fatalf("refs = %+v, want nil on error", refs)
	}
	requireInvalidCarrier(t, err, "Sec")
	requireCalls(t, repo,
		"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)", "TreePathMode(head,a.md)")
}

func TestResolveDirectReferencesDeletedAtHead(t *testing.T) {
	setup := func() *fakeReviewRepo {
		repo := newFakeReviewRepo()
		repo.commit("pin", "pinrev")
		repo.file("pinrev", "a.md", "## Sec\nbody\n")
		return repo
	}
	t.Run("refused", func(t *testing.T) {
		repo := setup()
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		if refs != nil {
			t.Fatalf("refs = %+v, want nil on error", refs)
		}
		requireInvalidCarrier(t, err, `direct reference "doc" was deleted at integration HEAD`)
		requireCalls(t, repo,
			"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)", "TreePathMode(head,a.md)")
	})
	t.Run("disclosed", func(t *testing.T) {
		repo := setup()
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseNone)
		if err != nil {
			t.Fatal(err)
		}
		want := Reference{ID: "doc", Path: "a.md", Heading: "Sec", Revision: "pinrev", BlobOID: "oid:## Sec\nbody\n",
			Span: "## Sec\nbody\n", PinnedRevision: "pinrev", Unresolved: "path deleted"}
		if len(refs) != 1 || refs[0] != want {
			t.Fatalf("refs = %+v, want [%+v]", refs, want)
		}
	})
}

func TestResolveDirectReferencesLocalRange(t *testing.T) {
	setup := func() *fakeReviewRepo {
		repo := newFakeReviewRepo()
		repo.commit("pin", "pinrev")
		repo.file("pinrev", "a.md", "## Sec\nbody\n")
		return repo
	}
	t.Run("deleted since range base", func(t *testing.T) {
		repo := setup()
		repo.file("base", "a.md", "## Sec\nbody\n")
		_, err := ResolveDirectReferences(repo, "head", "base", "review", resolveContract(docRef), refuseNone)
		requireInvalidCarrier(t, err, `direct reference "doc" was deleted at integration HEAD`)
		requireCalls(t, repo,
			"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)",
			"TreePathMode(head,a.md)", "TreePathMode(base,a.md)")
	})
	t.Run("absent everywhere", func(t *testing.T) {
		repo := setup()
		_, err := ResolveDirectReferences(repo, "head", "base", "review", resolveContract(docRef), refuseNone)
		requireInvalidCarrier(t, err, `"a.md" is absent at integration HEAD, the range base and the review commit (pin pinrev)`)
		requireCalls(t, repo,
			"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)",
			"TreePathMode(head,a.md)", "TreePathMode(base,a.md)", "TreePathMode(review,a.md)")
	})
	t.Run("introduced by the range is compared at the review commit", func(t *testing.T) {
		repo := setup()
		repo.file("review", "a.md", "## Sec\nreviewed body\n")
		refs, err := ResolveDirectReferences(repo, "head", "base", "review", resolveContract(docRef), refuseAll)
		var invalid *InvalidCarrierError
		if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "is stale at review commit") {
			t.Fatalf("err = %v, refs = %+v, want stale at review commit", err, refs)
		}
		requireCalls(t, repo,
			"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)",
			"TreePathMode(head,a.md)", "TreePathMode(base,a.md)", "TreePathMode(review,a.md)",
			"BlobOID(review,a.md)", "ReadBlob(review,a.md)")
	})
}

func TestResolveDirectReferencesClassifiesDrift(t *testing.T) {
	setup := func(headContent string) *fakeReviewRepo {
		repo := newFakeReviewRepo()
		repo.commit("pin", "pinrev")
		repo.file("pinrev", "a.md", "## Sec\nbody\n## Next\nx\n")
		repo.file("head", "a.md", headContent)
		return repo
	}
	t.Run("edit elsewhere in the file is not drift", func(t *testing.T) {
		repo := setup("## Sec\nbody\n## Next\ny\n")
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		if err != nil {
			t.Fatal(err)
		}
		if refs[0].Revision != "pinrev" || refs[0].PinnedRevision != "" || refs[0].Span != "## Sec\nbody\n" {
			t.Fatalf("ref = %+v, want pinned reference without disclosure", refs[0])
		}
		requireCalls(t, repo,
			"ResolveCommit(pin)", "ReadBlob(pinrev,a.md)", "BlobOID(pinrev,a.md)",
			"TreePathMode(head,a.md)", "BlobOID(head,a.md)", "ReadBlob(head,a.md)")
	})
	t.Run("changed section refused", func(t *testing.T) {
		repo := setup("## Sec\nchanged\n## Next\nx\n")
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		if refs != nil {
			t.Fatalf("refs = %+v, want nil on error", refs)
		}
		requireInvalidCarrier(t, err, `direct reference "doc" is stale at integration HEAD`)
	})
	t.Run("changed section disclosed with current text", func(t *testing.T) {
		repo := setup("## Sec\nchanged\n## Next\nx\n")
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseNone)
		if err != nil {
			t.Fatal(err)
		}
		want := Reference{ID: "doc", Path: "a.md", Heading: "Sec", Revision: "head",
			BlobOID: "oid:## Sec\nchanged\n## Next\nx\n", Span: "## Sec\nchanged\n", PinnedRevision: "pinrev"}
		if refs[0] != want {
			t.Fatalf("ref = %+v, want %+v", refs[0], want)
		}
	})
	t.Run("section gone at head refused", func(t *testing.T) {
		repo := setup("## Renamed\nbody\n")
		_, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseAll)
		requireInvalidCarrier(t, err, `direct reference "doc" is stale at integration HEAD: `)
	})
	t.Run("section gone at head disclosed with pinned text", func(t *testing.T) {
		repo := setup("## Renamed\nbody\n")
		refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef), refuseNone)
		if err != nil {
			t.Fatal(err)
		}
		_, headingErr := ExtractSection("## Renamed\nbody\n", "Sec")
		if refs[0].Revision != "pinrev" || refs[0].PinnedRevision != "pinrev" || refs[0].Span != "## Sec\nbody\n" ||
			headingErr == nil || refs[0].Unresolved != headingErr.Error() {
			t.Fatalf("ref = %+v, want pinned text disclosed as unresolved (%v)", refs[0], headingErr)
		}
	})
	t.Run("freshness read failure is a Git failure", func(t *testing.T) {
		repo := setup("## Sec\nchanged\n")
		failing := &readFailRepo{fakeReviewRepo: repo, failRevision: "head"}
		_, err := ResolveDirectReferences(failing, "head", "", "", resolveContract(docRef), refuseNone)
		var invalid *InvalidCarrierError
		if err == nil || errors.As(err, &invalid) || err.Error() != "read failed" {
			t.Fatalf("error = %v, want the raw ReadBlob error", err)
		}
	})
}

// readFailRepo fails ReadBlob at one revision.
type readFailRepo struct {
	*fakeReviewRepo
	failRevision string
}

func (r *readFailRepo) ReadBlob(revision, path string) (string, error) {
	if revision == r.failRevision {
		r.record("ReadBlob(%s,%s)", revision, path)
		return "", errors.New("read failed")
	}
	return r.fakeReviewRepo.ReadBlob(revision, path)
}

func TestResolveDirectReferencesKeepsDeclarationOrderAndStopsAtFirstError(t *testing.T) {
	repo := newFakeReviewRepo()
	repo.commit("pin", "pinrev")
	repo.file("pinrev", "a.md", "## Sec\nbody\n")
	repo.file("head", "a.md", "## Sec\nbody\n")
	repo.file("pinrev", "b.md", "## Sec\nbody b\n")
	repo.file("head", "b.md", "## Sec\nbody b\n")
	second := DirectReference{ID: "second", Path: "b.md", Heading: "Sec"}

	refs, err := ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef, second), refuseAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].ID != "doc" || refs[1].ID != "second" {
		t.Fatalf("refs = %+v, want declaration order doc, second", refs)
	}

	missing := DirectReference{ID: "gone", Path: "c.md", Heading: "Sec"}
	repo = newFakeReviewRepo()
	repo.commit("pin", "pinrev")
	repo.file("pinrev", "a.md", "## Sec\nbody\n")
	repo.file("head", "a.md", "## Sec\nbody\n")
	refs, err = ResolveDirectReferences(repo, "head", "", "", resolveContract(docRef, missing, second), refuseAll)
	if refs != nil {
		t.Fatalf("refs = %+v, want nil on error", refs)
	}
	requireInvalidCarrier(t, err, `direct reference "gone": "c.md" is not present at its pinned revision pinrev`)
	for _, call := range repo.calls {
		if strings.Contains(call, "b.md") {
			t.Fatalf("resolution continued past the first error: %v", repo.calls)
		}
	}
}
