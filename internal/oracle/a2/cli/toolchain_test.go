//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func toolModes(t *testing.T, run func(*testing.T, *fixture, string, oracle.Kind)) {
	t.Helper()
	for _, mode := range []string{"human", "json"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.stub("rtk", "present")
			f.stub("apt-get", "installer")
			f.stub("npm", "installer")
			f.extra = append(f.extra, "ORACLE_STUB_LOG="+filepath.Join(f.root, "installer-ran"))
			kind := oracle.Human
			if mode == "json" {
				kind = oracle.JSON
			}
			run(t, f, mode, kind)
			if _, err := os.Stat(filepath.Join(f.root, "installer-ran")); !os.IsNotExist(err) {
				t.Fatal("a fixture installer executed")
			}
		})
	}
}

func modeArgs(args []string, mode string) []string {
	if mode == "json" {
		return append(args, "--json")
	}
	return args
}

func TestOracleA2ToolchainList(t *testing.T) {
	toolModes(t, func(t *testing.T, f *fixture, mode string, kind oracle.Kind) {
		f.check("toolchain-list/"+mode, kind, f.run(nil, modeArgs([]string{"toolchain", "list"}, mode)...), 0)
	})
}

func TestOracleA2ToolchainDoctor(t *testing.T) {
	toolModes(t, func(t *testing.T, f *fixture, mode string, kind oracle.Kind) {
		f.check("toolchain-doctor/"+mode, kind, f.run(nil, modeArgs([]string{"toolchain", "doctor", "--profile", "lean"}, mode)...), 0)
	})
}

func TestOracleA2ToolchainInstall(t *testing.T) {
	toolModes(t, func(t *testing.T, f *fixture, mode string, kind oracle.Kind) {
		f.check("toolchain-install/dry-run-"+mode, kind, f.run(nil, modeArgs([]string{"toolchain", "install", "--profile", "lean", "--dry-run", "--install-dir", f.install}, mode)...), 0)
		f.fileEffects("toolchain-install/dry-run-"+mode, map[string]string{"install": f.install, "home": f.home})
	})
	t.Run("noninteractive-refusal", func(t *testing.T) {
		f := newFixture(t)
		// No --tool: this reaches the non-interactive confirmation refusal.
		f.check("toolchain-install/refusal", oracle.Human, f.run(nil, "toolchain", "install", "--profile", "lean", "--install-dir", f.install), 1)
		f.fileEffects("toolchain-install/refusal", map[string]string{"install": f.install, "home": f.home})
	})
}

func TestOracleA2ToolchainConfigure(t *testing.T) {
	toolModes(t, func(t *testing.T, f *fixture, mode string, kind oracle.Kind) {
		f.check("toolchain-configure/"+mode, kind, f.run(nil, modeArgs([]string{"toolchain", "configure", "--profile", "lean", "--global-dir", f.global, "--install-dir", f.install}, mode)...), 0)
		f.fileEffects("toolchain-configure/"+mode, map[string]string{"global": f.global, "home": f.home, "install": f.install, "project": f.project})
	})
	t.Run("project-without-agents", func(t *testing.T) {
		f := newFixture(t)
		f.check("toolchain-configure/project-refusal", oracle.Human, f.run(nil, "toolchain", "configure", "--project", f.project, "--global-dir", f.global, "--install-dir", f.install), 1)
		f.fileEffects("toolchain-configure/project-refusal", map[string]string{"global": f.global, "home": f.home, "install": f.install, "project": f.project})
	})
}
