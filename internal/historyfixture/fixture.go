// Package historyfixture builds the deterministic two-repository fix history
// the fix-history analysis is proved against: an upstream repository and a
// downstream clone of it, whose commits are the cases of Cases. It runs only
// the git executable, with an environment it constructs. It is imported by
// tests and by its own materializer command, never by production code.
//
// The repositories carry no tag, no branch but main and no content naming a
// case: the case table lives only in Go.
package historyfixture

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Branch is the only branch each repository keeps.
const Branch = "main"

// Repository is one built repository.
type Repository struct {
	// Dir is the repository's working tree.
	Dir string
	// Head is the SHA of its last commit.
	Head string
}

// History is the built pair of repositories and the case table with SHAs.
type History struct {
	Upstream   Repository
	Downstream Repository
	// Cases is the case table in build order, SHA filled in.
	Cases []Case
}

// Case returns the case with the given id.
func (h History) Case(id string) (Case, bool) {
	for _, c := range h.Cases {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
}

// Repo returns the repository with the given name (Upstream or Downstream).
func (h History) Repo(name string) Repository {
	if name == Downstream {
		return h.Downstream
	}
	return h.Upstream
}

// Environ returns the environment git runs in against a fixture: nothing is
// inherited but the location of the executables, and home stands for the
// user's home and configuration directories.
func Environ(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	}
}

// Build creates both repositories under dir, which must be absent or empty:
// dir/upstream and dir/downstream. Two builds yield identical commit SHAs.
// A relative dir is resolved once, so the returned paths are absolute and
// valid whatever directory git runs in.
func Build(dir string) (History, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return History{}, fmt.Errorf("resolve %s: %w", dir, err)
	}
	dir = abs
	if err := requireEmpty(dir); err != nil {
		return History{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return History{}, fmt.Errorf("create %s: %w", dir, err)
	}
	home, err := os.MkdirTemp("", "historyfixture-home-")
	if err != nil {
		return History{}, fmt.Errorf("create a git home: %w", err)
	}
	defer os.RemoveAll(home)

	cases := Cases()
	h := History{Cases: cases}
	h.Upstream.Dir = filepath.Join(dir, Upstream)
	h.Downstream.Dir = filepath.Join(dir, Downstream)
	env := Environ(home)
	shas := map[string]string{}

	up := &builder{dir: h.Upstream.Dir, env: env}
	if err := up.initRepo(); err != nil {
		return History{}, err
	}
	for i := range h.Cases {
		c := &h.Cases[i]
		if c.Repo != Upstream {
			continue
		}
		if err := up.apply(c, shas); err != nil {
			return History{}, fmt.Errorf("case %s: %w", c.ID, err)
		}
		shas[c.ID] = c.SHA
		if c.ID == cloneAfter {
			if _, err := (&builder{dir: dir, env: env}).git("clone", "--quiet", h.Upstream.Dir, h.Downstream.Dir); err != nil {
				return History{}, err
			}
		}
	}
	if h.Upstream.Head, err = up.head(); err != nil {
		return History{}, err
	}

	down := &builder{dir: h.Downstream.Dir, env: env}
	if err := down.configure(); err != nil {
		return History{}, err
	}
	for i := range h.Cases {
		c := &h.Cases[i]
		if c.Repo != Downstream {
			continue
		}
		if err := down.apply(c, shas); err != nil {
			return History{}, fmt.Errorf("case %s: %w", c.ID, err)
		}
		shas[c.ID] = c.SHA
	}
	// Dropping the remote leaves main as the only ref downstream.
	if _, err := down.git("remote", "remove", "origin"); err != nil {
		return History{}, err
	}
	if h.Downstream.Head, err = down.head(); err != nil {
		return History{}, err
	}
	return h, nil
}

func requireEmpty(dir string) error {
	entries, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("read %s: %w", dir, err)
	case len(entries) > 0:
		return fmt.Errorf("%s is not empty", dir)
	}
	return nil
}

type builder struct {
	dir     string
	env     []string
	current string
	created map[string]bool
}

func (b *builder) initRepo() error {
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", b.dir, err)
	}
	if _, err := b.git("init", "--quiet", "--initial-branch="+Branch); err != nil {
		return err
	}
	return b.configure()
}

// configure pins the repository's configuration; a fresh repository or clone
// is on main.
func (b *builder) configure() error {
	b.current = Branch
	for _, args := range [][]string{
		{"config", "commit.gpgsign", "false"},
		{"config", "core.autocrlf", "false"},
		{"config", "tag.gpgsign", "false"},
	} {
		if _, err := b.git(args...); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) head() (string, error) {
	out, err := b.git("rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// switchTo checks out branch, creating a side branch on first use.
func (b *builder) switchTo(branch string) error {
	if b.current == branch {
		return nil
	}
	args := []string{"checkout", "--quiet", branch}
	if branch != Branch && !b.created[branch] {
		args = []string{"checkout", "--quiet", "-b", branch}
		if b.created == nil {
			b.created = map[string]bool{}
		}
		b.created[branch] = true
	}
	if _, err := b.git(args...); err != nil {
		return err
	}
	b.current = branch
	return nil
}

// apply enters the case into the repository and records its SHA.
func (b *builder) apply(c *Case, shas map[string]string) error {
	stamp := fmt.Sprintf("%d +0000", c.Date.Unix())
	committer := []string{
		"GIT_COMMITTER_NAME=Fixture Committer",
		"GIT_COMMITTER_EMAIL=committer@example.invalid",
		"GIT_COMMITTER_DATE=" + stamp,
	}
	author := []string{
		"GIT_AUTHOR_NAME=Fixture Author",
		"GIT_AUTHOR_EMAIL=author@example.invalid",
		"GIT_AUTHOR_DATE=" + stamp,
	}
	withIdentity := func(extra ...[]string) []string {
		env := append([]string{}, b.env...)
		for _, e := range extra {
			env = append(env, e...)
		}
		return env
	}

	var err error
	switch c.Kind {
	case KindCommit:
		err = b.commit(c, withIdentity(author, committer))
	case KindMerge:
		if err = b.switchTo(Branch); err == nil {
			_, err = b.gitEnv(withIdentity(author, committer), "", "merge", "--quiet", "--no-ff", "-m", c.Subject, c.Branch)
		}
		if err == nil {
			_, err = b.git("branch", "--quiet", "-d", c.Branch)
		}
	case KindRevert:
		_, err = b.gitEnv(withIdentity(author, committer), "", "revert", "--no-edit", shas[c.Of])
	case KindCherryPick:
		// The author is the picked commit's; only the committer is ours.
		if _, err = b.git("fetch", "--quiet", "origin"); err == nil {
			_, err = b.gitEnv(withIdentity(committer), "", "cherry-pick", shas[c.Of])
		}
	default:
		err = fmt.Errorf("unknown kind %d", c.Kind)
	}
	if err != nil {
		return err
	}
	c.SHA, err = b.head()
	return err
}

func (b *builder) commit(c *Case, env []string) error {
	branch := c.Branch
	if branch == "" {
		branch = Branch
	}
	if err := b.switchTo(branch); err != nil {
		return err
	}
	for _, f := range c.Files {
		path := filepath.Join(b.dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(f.Path), err)
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.Path, err)
		}
		if _, err := b.git("add", "--", f.Path); err != nil {
			return err
		}
	}
	message := c.Subject + "\n"
	if c.Body != "" {
		message += "\n" + c.Body + "\n"
	}
	_, err := b.gitEnv(env, message, "commit", "--quiet", "--cleanup=verbatim", "-F", "-")
	return err
}

func (b *builder) git(args ...string) (string, error) {
	return b.gitEnv(b.env, "", args...)
}

// gitEnv runs git in the builder's directory. Diagnostics name the git
// subcommand and git's own output, never the environment.
func (b *builder) gitEnv(env []string, stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", b.dir}, args...)...)
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
