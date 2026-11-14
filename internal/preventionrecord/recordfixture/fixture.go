// Package recordfixture builds the deterministic sample-record repository the
// prevention record document's queries are proved against: one linear
// repository whose commits are the cases of Cases, tagged case/<id>. It runs
// only the git executable, with an environment it constructs. It is imported
// by tests and by its own materializer command, never by production code.
package recordfixture

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Branch is the branch every case is committed on.
const Branch = "main"

// Repo is a built sample-record repository.
type Repo struct {
	// Dir is the repository's working tree.
	Dir string
	// Head is the SHA of the last case.
	Head string
	// Cases is the case table with SHA filled in, in commit order.
	Cases []Case
}

// Case returns the case with the given id.
func (r Repo) Case(id string) (Case, bool) {
	for _, c := range r.Cases {
		if c.ID == id {
			return c, true
		}
	}
	return Case{}, false
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

// Build creates the sample-record repository in dir, which must be absent or
// empty, and returns it. Two builds yield identical commit SHAs.
func Build(dir string) (Repo, error) {
	if err := requireEmpty(dir); err != nil {
		return Repo{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Repo{}, fmt.Errorf("create %s: %w", dir, err)
	}
	home, err := os.MkdirTemp("", "recordfixture-home-")
	if err != nil {
		return Repo{}, fmt.Errorf("create a git home: %w", err)
	}
	defer os.RemoveAll(home)

	b := builder{dir: dir, env: Environ(home)}
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch=" + Branch},
		{"config", "commit.gpgsign", "false"},
		{"config", "core.autocrlf", "false"},
		{"config", "tag.gpgsign", "false"},
	} {
		if _, err := b.git(nil, args...); err != nil {
			return Repo{}, err
		}
	}

	cases := Cases()
	shas := map[string]string{}
	for i := range cases {
		sha, err := b.commit(cases[i], shas)
		if err != nil {
			return Repo{}, fmt.Errorf("case %s: %w", cases[i].ID, err)
		}
		cases[i].SHA = sha
		shas[cases[i].ID] = sha
		if _, err := b.git(nil, "tag", cases[i].Tag); err != nil {
			return Repo{}, fmt.Errorf("case %s: %w", cases[i].ID, err)
		}
	}
	return Repo{Dir: dir, Head: cases[len(cases)-1].SHA, Cases: cases}, nil
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
	dir string
	env []string
}

func (b builder) commit(c Case, shas map[string]string) (string, error) {
	path := filepath.Join(b.dir, filepath.FromSlash(c.Path))
	if c.Delete {
		if _, err := b.git(nil, "rm", "--quiet", "--", c.Path); err != nil {
			return "", err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", fmt.Errorf("create %s: %w", filepath.Dir(c.Path), err)
		}
		if err := os.WriteFile(path, []byte(c.ID+"\n"), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", c.Path, err)
		}
		if _, err := b.git(nil, "add", "--", c.Path); err != nil {
			return "", err
		}
	}

	message := c.Subject + "\n"
	for _, paragraph := range c.Paragraphs {
		message += "\n" + paragraph + "\n"
	}
	for id, sha := range shas {
		message = strings.ReplaceAll(message, "{sha:"+id+"}", sha)
	}

	stamp := fmt.Sprintf("%d +0000", c.Date.Unix())
	env := append(append([]string{}, b.env...),
		"GIT_AUTHOR_NAME=Fixture Author",
		"GIT_AUTHOR_EMAIL=author@example.invalid",
		"GIT_AUTHOR_DATE="+stamp,
		"GIT_COMMITTER_NAME=Fixture Committer",
		"GIT_COMMITTER_EMAIL=committer@example.invalid",
		"GIT_COMMITTER_DATE="+stamp,
	)
	if _, err := b.gitEnv(env, strings.NewReader(message), "commit", "--quiet", "--cleanup=verbatim", "-F", "-"); err != nil {
		return "", err
	}
	out, err := b.git(nil, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (b builder) git(stdin *strings.Reader, args ...string) (string, error) {
	return b.gitEnv(b.env, stdin, args...)
}

// gitEnv runs git in the repository. Diagnostics name the git subcommand and
// git's own output, never the environment.
func (b builder) gitEnv(env []string, stdin *strings.Reader, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", b.dir}, args...)...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
