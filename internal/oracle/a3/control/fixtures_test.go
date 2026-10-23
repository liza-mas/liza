package control

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestFixtureStatesValidate(t *testing.T) {
	linuxOnly(t)
	files, err := filepath.Glob("testdata/fixtures/state/*.yaml")
	if err != nil || len(files) != 9 {
		t.Fatal("expected nine fixture states")
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".yaml")
		t.Run(name, func(t *testing.T) {
			p := newProject(t, name, false, name == "no-live-agent", false)
			res := p.run(t, "validate")
			if res.code != 0 || strings.TrimSpace(res.stdout) != "VALID" {
				t.Fatalf("fixture %s validate: exit %d; %s", name, res.code, p.normalise(t, "validation", []byte(res.stdout+res.stderr)))
			}
		})
	}
}

func envNames(env []string) []string {
	names := make([]string, 0, len(env))
	for _, pair := range env {
		name, _, _ := strings.Cut(pair, "=")
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestChildEnvNames(t *testing.T) {
	linuxOnly(t)
	t.Setenv("ORACLE_HOST_ONLY", "must-not-reach-child")
	for _, agent := range []bool{false, true} {
		p := newProject(t, "base", agent, false, true)
		cmd, cancel := p.command(filepath.Join(p.bin, "oracle-stub"), "0")
		defer cancel()
		base, err := oracle.ChildEnv(oracle.EnvBase{Home: p.home, Path: p.bin, Tmp: p.tmp})
		if err != nil {
			t.Fatal(err)
		}
		want := envNames(base)
		want = append(want, envNames(childExtras)...)
		if agent {
			want = append(want, "LIZA_AGENT_ID", "LIZA_AGENT_GENERATION")
		}
		sort.Strings(want)
		if got := envNames(cmd.Env); !reflect.DeepEqual(got, want) {
			t.Fatalf("child variable names: got %v, want %v", got, want)
		}
		// Observe the actual child environment as names, not just our constructor.
		probe := "#!/bin/sh\nfor name in HOME PATH TMPDIR TZ LIZA_SKIP_AUTO_UPDATE LIZA_PROVIDER_CATALOG_URL GIT_CONFIG_NOSYSTEM GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL GIT_AUTHOR_DATE GIT_COMMITTER_NAME GIT_COMMITTER_EMAIL GIT_COMMITTER_DATE LIZA_AGENT_ID LIZA_AGENT_GENERATION ORACLE_HOST_ONLY; do eval 'present=${'\"$name\"'+x}'; if [ \"$present\" = x ]; then echo \"$name\"; fi; done\n"
		mustWrite(t, filepath.Join(p.bin, "env-names"), []byte(probe), 0o755)
		cmd, cancel = p.command(filepath.Join(p.bin, "env-names"))
		out, err := cmd.Output()
		cancel()
		if err != nil {
			t.Fatal("environment names child failed")
		}
		got := strings.Fields(string(out))
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("observed variable names: got %v, want %v", got, want)
		}
		if cmd.Dir != p.dir {
			t.Fatal("child working directory escapes disposable project")
		}
		entries, err := os.ReadDir(p.bin)
		if err != nil {
			t.Fatal("read disposable PATH")
		}
		var bins []string
		for _, e := range entries {
			bins = append(bins, e.Name())
		}
		if !reflect.DeepEqual(bins, []string{"env-names", "git", "oracle-stub"}) {
			t.Fatalf("PATH entries: %v", bins)
		}
	}
}
