//go:build linux

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type fixture struct {
	t                                                     *testing.T
	root, home, project, tmp, bin, store, install, global string
	cli                                                   string
	norm                                                  *oracle.Normaliser
	extra                                                 []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	cli := oracle.BuildCLI(t, "./cmd/liza")
	parent := t.TempDir()
	// Digest excerpts and context byte counts include the unnormalised store
	// path. Fix its byte length without sharing any directory across cases.
	const rootBytes = 240
	if len(parent)+2 >= rootBytes {
		t.Fatal("fixture parent exceeds fixed path budget")
	}
	f := &fixture{t: t, cli: cli, root: filepath.Join(parent, strings.Repeat("f", rootBytes-len(parent)-1)), norm: oracle.NewNormaliser()}
	f.home, f.project, f.tmp = filepath.Join(f.root, "home"), filepath.Join(f.root, "project"), filepath.Join(f.root, "tmp")
	f.bin, f.store = filepath.Join(f.root, "bin"), filepath.Join(f.root, "store")
	f.install, f.global = filepath.Join(f.root, "install", "bin"), filepath.Join(f.root, "global")
	for _, dir := range []string{f.home, f.project, f.tmp, f.bin, f.store, f.install, f.global} {
		must(t, os.MkdirAll(dir, 0755))
	}
	for _, pair := range [][2]string{{f.root, "<FIXTURE>"}, {f.home, oracle.TokenHome}, {f.project, oracle.TokenProject}, {f.tmp, oracle.TokenTmp}, {f.bin, "<BIN>"}, {f.store, "<STORE>"}, {f.install, "<INSTALL>"}, {f.global, "<GLOBAL>"}} {
		f.norm.Dir(pair[0], pair[1])
	}
	f.extra = []string{"LIZA_SKIP_AUTO_UPDATE=1", "LIZA_PROVIDER_CATALOG_URL=http://127.0.0.1:1/catalog.yaml", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + filepath.Join(f.home, ".gitconfig")}
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func input(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "fixtures", filepath.FromSlash(name)))
	must(t, err)
	return b
}

func (f *fixture) env() []string {
	f.t.Helper()
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: f.bin, Tmp: f.tmp}, f.extra...)
	must(f.t, err)
	// This is a fixture environment, but report names only even on failure.
	names := map[string]bool{}
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		names[name] = true
	}
	for _, name := range []string{"SHELL", "LIZA_AGENT_ID", "OMNI_EE_AGENT_ID", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "GIT_DIR", "GIT_WORK_TREE"} {
		if names[name] {
			f.t.Fatalf("unexpected child variable %s", name)
		}
	}
	return env
}

func (f *fixture) command(args ...string) (*exec.Cmd, context.CancelFunc) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, f.cli, args...)
	cmd.Dir, cmd.Env = f.project, f.env()
	return cmd, cancel
}

func (f *fixture) run(stdin []byte, args ...string) oracle.Result {
	f.t.Helper()
	cmd, cancel := f.command(args...)
	defer cancel()
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else {
		must(f.t, err)
	}
	return oracle.Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: code, Norm: f.norm}
}

func (f *fixture) check(name string, kind oracle.Kind, result oracle.Result, wantExit int) {
	f.t.Helper()
	if result.ExitCode != wantExit {
		f.t.Fatalf("%s: exit %d, want %d; stderr %s", name, result.ExitCode, wantExit, f.norm.Text(string(result.Stderr)))
	}
	ext := ".txt"
	if kind == oracle.JSON {
		ext = ".json"
	}
	f.norm.Check(f.t, kind, name+"-stdout"+ext, result.Stdout)
	f.norm.Check(f.t, oracle.Human, name+"-stderr.txt", result.Stderr)
	f.norm.Check(f.t, oracle.Human, name+"-exit.txt", []byte(strconv.Itoa(result.ExitCode)+"\n"))
}

func (f *fixture) stub(name, source string) {
	f.t.Helper()
	must(f.t, os.WriteFile(filepath.Join(f.bin, name), input(f.t, "bin/"+source), 0755))
}

func (f *fixture) git(args ...string) {
	f.t.Helper()
	cmd := exec.Command(filepath.Join(f.bin, "git"), args...)
	cmd.Dir, cmd.Env = f.project, f.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("fixture git failed: %v: %s", err, f.norm.Text(string(out)))
	}
}

func (f *fixture) repository() {
	f.t.Helper()
	git, err := exec.LookPath("git")
	must(f.t, err)
	must(f.t, os.Symlink(git, filepath.Join(f.bin, "git")))
	must(f.t, os.CopyFS(f.project, os.DirFS(filepath.Join("testdata", "fixtures", "repo"))))
	f.git("init", "--initial-branch=fixture")
}

// fileEffects snapshots the complete inventory and every byte. Only declared
// timestamp, disposable path, loopback port and event-id volatility is replaced;
// the artifact comparator itself remains byte-exact, including whitespace.
func (f *fixture) fileEffects(name string, roots map[string]string) {
	f.t.Helper()
	files := map[string][]byte{}
	for label, root := range roots {
		must(f.t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !d.Type().IsRegular() {
				return fmt.Errorf("non-regular effect in %s", label)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			key := label + "/" + filepath.ToSlash(rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			// Event filenames are 32-hex, outside the harness's generic ID
			// rule. These scenarios record one filter event, so token 1 is
			// unambiguous; fail rather than overwrite any collision.
			if filepath.Base(filepath.Dir(path)) == "events" {
				f.norm.Literal(strings.TrimSuffix(filepath.Base(path), ".json"), "<EVENT-1>")
			}
			key = f.norm.Text(key)
			if _, exists := files[key]; exists {
				return fmt.Errorf("effect inventory collision: %s", key)
			}
			files[key] = data
			return nil
		}))
	}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	inventory := strings.Join(keys, "\n")
	if len(keys) > 0 {
		inventory += "\n"
	}
	f.norm.Check(f.t, oracle.Artifact, name+"/inventory.txt", []byte(inventory))
	for _, key := range keys {
		f.norm.Check(f.t, oracle.Artifact, name+"/"+key, []byte(f.norm.Text(string(files[key]))))
	}
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var value T
	must(t, json.Unmarshal(b, &value))
	return value
}

func (f *fixture) resultArgs(command string, extra ...string) []string {
	return append([]string{"tool-result", "--root", f.store, "--threshold-bytes", "2048", "--digest-bytes", "1024", command}, extra...)
}
