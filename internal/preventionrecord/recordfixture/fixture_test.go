package recordfixture

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = Environ(t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func buildInto(t *testing.T) Repo {
	t.Helper()
	repo, err := Build(filepath.Join(t.TempDir(), "repo"))
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestBuildDeterministic(t *testing.T) {
	a, b := buildInto(t), buildInto(t)
	if len(a.Cases) != 18 || len(a.Cases) != len(b.Cases) {
		t.Fatalf("cases: %d and %d", len(a.Cases), len(b.Cases))
	}
	for i := range a.Cases {
		if a.Cases[i].SHA == "" || a.Cases[i].SHA != b.Cases[i].SHA {
			t.Errorf("case %s: SHAs %q and %q differ", a.Cases[i].ID, a.Cases[i].SHA, b.Cases[i].SHA)
		}
	}
	if a.Head != b.Head || runGit(t, a.Dir, "rev-parse", "HEAD") != a.Head {
		t.Errorf("head %q and %q", a.Head, b.Head)
	}
}

func TestBuildTagsEveryCase(t *testing.T) {
	repo := buildInto(t)
	if got := runGit(t, repo.Dir, "symbolic-ref", "--short", "HEAD"); got != Branch {
		t.Errorf("branch %q, want %q", got, Branch)
	}
	if got := runGit(t, repo.Dir, "rev-list", "--merges", "--count", "HEAD"); got != "0" {
		t.Errorf("merge commits: %s", got)
	}
	var previous string
	for _, c := range repo.Cases {
		if got := runGit(t, repo.Dir, "rev-parse", "--verify", c.Tag+"^{commit}"); got != c.SHA {
			t.Errorf("tag %s at %q, want %q", c.Tag, got, c.SHA)
		}
		// Lightweight tag: the tag name resolves to the commit without a tag object.
		if got := runGit(t, repo.Dir, "cat-file", "-t", c.Tag); got != "commit" {
			t.Errorf("tag %s is a %s object", c.Tag, got)
		}
		parents := runGit(t, repo.Dir, "log", "-1", "--format=%P", c.SHA)
		if parents != previous {
			t.Errorf("case %s: parent %q, want %q", c.ID, parents, previous)
		}
		dates := strings.Fields(runGit(t, repo.Dir, "log", "-1", "--format=%at %ct", c.SHA))
		if len(dates) != 2 || dates[0] != dates[1] {
			t.Errorf("case %s: author and committer dates %v", c.ID, dates)
		}
		previous = c.SHA
	}
	if got := runGit(t, repo.Dir, "rev-list", "--count", "HEAD"); got != "18" {
		t.Errorf("commit count %s", got)
	}
}

func TestBuildSubjectsMatchTable(t *testing.T) {
	repo := buildInto(t)
	for _, c := range repo.Cases {
		if got := runGit(t, repo.Dir, "log", "-1", "--format=%s", c.SHA); got != c.Subject {
			t.Errorf("case %s: subject %q, want %q", c.ID, got, c.Subject)
		}
	}
	revert, _ := repo.Case("c15")
	c05, _ := repo.Case("c05")
	body := runGit(t, repo.Dir, "log", "-1", "--format=%b", revert.SHA)
	if !strings.Contains(body, "This reverts commit "+c05.SHA+".") {
		t.Errorf("c15 body does not name c05: %q", body)
	}
	if _, err := os.Stat(filepath.Join(repo.Dir, "cases", "c05.txt")); !os.IsNotExist(err) {
		t.Errorf("c05's file survives its revert: %v", err)
	}
}

func TestBuildRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(keep, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("err = %v, want a non-empty refusal", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("directory changed: %v, %d entries", err, len(entries))
	}

	empty := t.TempDir()
	if _, err := Build(empty); err != nil {
		t.Fatalf("empty directory refused: %v", err)
	}
}

func TestEnvironNamesOnly(t *testing.T) {
	t.Setenv("RECORDFIXTURE_SENTINEL", "must-not-be-inherited")
	for _, entry := range Environ("home") {
		if strings.HasPrefix(entry, "RECORDFIXTURE_SENTINEL=") {
			t.Fatal("the fixture environment inherits a host variable")
		}
	}
}
