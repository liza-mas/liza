//go:build linux

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestOracleA2IndexRefresh(t *testing.T) {
	t.Run("activated", func(t *testing.T) {
		f := newFixture(t)
		f.repository()
		for _, name := range []string{"stacklit", "mkdir", "mktemp", "rm", "mv", "cp", "cksum", "awk"} {
			f.stub(name, name)
		}
		f.extra = append(f.extra, "LIZA_ENABLE_SEMBLE=0")
		installIndexActivation(t, f)
		f.check("index-refresh/activated", oracle.Human, f.run(nil, "index-refresh"), 0)
		marker, err := os.ReadFile(filepath.Join(f.project, "index-invocations.txt"))
		must(t, err)
		if !bytes.Contains(marker, []byte("generate-json\n")) {
			t.Fatal("activated refresh did not invoke the index fixture")
		}
	})
	t.Run("no-activation", func(t *testing.T) {
		f := newFixture(t)
		f.repository()
		f.check("index-refresh/no-activation", oracle.Human, f.run(nil, "index-refresh"), 0)
	})
	t.Run("not-repository", func(t *testing.T) {
		f := newFixture(t)
		git, err := exec.LookPath("git")
		must(t, err)
		// The same fixture-only Git link is needed to reach rev-parse's refusal.
		must(t, os.Symlink(git, filepath.Join(f.bin, "git")))
		f.check("index-refresh/not-repository", oracle.Human, f.run(nil, "index-refresh"), 1)
	})
}

func TestOracleA2SembleReady(t *testing.T) {
	for _, name := range []string{"disabled", "ready", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.repository()
			enabled := "1"
			want := 1
			if name == "disabled" {
				enabled = "0"
			}
			if name != "unavailable" {
				f.stub("semble", "semble")
			}
			if name == "ready" {
				want = 0
			}
			f.extra = append(f.extra, "LIZA_ENABLE_SEMBLE="+enabled)
			f.check("semble-ready/"+name, oracle.Human, f.run(nil, "semble-ready", f.project), want)
			_, err := os.Stat(filepath.Join(f.project, "semble-invocations.txt"))
			if name == "ready" {
				must(t, err)
			} else if !os.IsNotExist(err) {
				t.Fatal("unavailable or disabled readiness invoked semble")
			}
		})
	}
}
