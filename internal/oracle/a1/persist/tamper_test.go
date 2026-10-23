package persist

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const persistencePackage = "./internal/oracle/a1/persist/..."
const surfaceTests = "^(TestOracleStateRoundTrip|TestOracleAlertLog|TestOracleUsageRecordAppend)$"

var goldenInventory = []string{
	"alerts-log.txt", "alerts-once.txt", "alerts-readback.txt",
	"alerts-refusal-keyed.txt", "alerts-refusal-plain.txt",
	"state-refusal.txt", "state-roundtrip.txt", "usage-days.txt", "usage-refusal.txt",
}

func TestOraclePersistenceTamper(t *testing.T) {
	if os.Getenv("ORACLE_VERIFY_TAMPER") != "1" || os.Getenv(oracle.WriteEnv) == "1" {
		t.Skip("opt-in committed-candidate sensitivity proof")
	}
	root := moduleRoot(t)
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	base := oracle.EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: tmp}
	gitEnv := childEnvironment(t, base)
	base.Go = true
	goEnv := childEnvironment(t, base)
	sha := strings.TrimSpace(string(runChildOK(t, root, gitEnv, "git", "rev-parse", "HEAD")))
	clone := filepath.Join(tmp, "candidate")
	runChildOK(t, tmp, gitEnv, "git", "clone", "--quiet", "--no-hardlinks", "--no-checkout", root, clone)
	runChildOK(t, clone, gitEnv, "git", "checkout", "--quiet", "--detach", sha)
	actualSHA := strings.TrimSpace(string(runChildOK(t, clone, gitEnv, "git", "rev-parse", "HEAD")))
	if actualSHA != sha {
		t.Fatal("disposable checkout differs from captured candidate")
	}
	runChildOK(t, clone, goEnv, "go", "run", "./internal/brandrender/cmd/sync-embedded", "--repo-root", ".")
	goldenDir := filepath.Join(clone, "internal", "oracle", "a1", "persist", "testdata", "golden", "state")
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Fatal("read committed golden inventory")
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatal("unexpected golden subdirectory")
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, goldenInventory) {
		t.Fatalf("golden inventory differs: %v", names)
	}

	// Independently prove the unmodified candidate and its starting-tree reproduction.
	liveArgs := []string{"test", "-count=1", "-run", surfaceTests, persistencePackage}
	checkArgs := []string{"run", "./internal/testhelpers/oracle/cmd/regen", "-mode", "check", "-pkg", persistencePackage}
	runChildOK(t, clone, goEnv, "go", liveArgs...)
	runChildOK(t, clone, goEnv, "go", checkArgs...)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(goldenDir, name)
			original := readBytes(t, path)
			t.Cleanup(func() { writeBytes(t, path, original) })
			writeBytes(t, path, append(bytes.Clone(original), []byte("\nORACLE INTENTIONAL TAMPER\n")...))
			rel := "internal/oracle/a1/persist/testdata/golden/state/" + name
			// Live test messages use package-relative paths; regen names repo-relative paths.
			requireTamperFailure(t, clone, goEnv, "testdata/golden/state/"+name, liveArgs)
			requireTamperFailure(t, clone, goEnv, rel, checkArgs)
		})
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal("locate package")
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("module root not found")
		}
		dir = parent
	}
}

func childEnvironment(t *testing.T, base oracle.EnvBase) []string {
	t.Helper()
	env, err := oracle.ChildEnv(base)
	if err != nil {
		t.Fatal("construct child environment")
	}
	want := []string{"HOME", "PATH", "TMPDIR"}
	if runtime.GOOS == "windows" {
		want = append(want, "USERPROFILE", "TMP", "TEMP", "SYSTEMROOT")
	}
	if base.Go {
		want = append(want, "GOCACHE", "GOMODCACHE", "GOFLAGS")
	}
	names := []string{}
	for _, pair := range env {
		name, _, _ := strings.Cut(pair, "=")
		names = append(names, strings.ToUpper(name))
	}
	sort.Strings(names)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("child environment names differ: %v", names)
	}
	// Exact-name equality excludes the enable flag, write mode and agent identity.
	return env
}

func runChild(dir string, env []string, program string, args ...string) ([]byte, error) {
	cmd := exec.Command(program, args...)
	cmd.Dir, cmd.Env = dir, env
	return cmd.CombinedOutput()
}

func runChildOK(t *testing.T, dir string, env []string, program string, args ...string) []byte {
	t.Helper()
	out, err := runChild(dir, env, program, args...)
	if err != nil {
		t.Fatalf("child %s failed: %v\n%.4000s", program, err, out)
	}
	return out
}

func requireTamperFailure(t *testing.T, dir string, env []string, path string, args []string) {
	t.Helper()
	out, err := runChild(dir, env, "go", args...)
	if err == nil {
		t.Fatalf("tampered %s passed %s", path, args[0])
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("tamper child did not execute: %v", err)
	}
	if !strings.Contains(strings.ReplaceAll(string(out), "\\", "/"), path) {
		t.Fatalf("tamper failure does not name %s (%s):\n%.4000s", path, args[0], out)
	}
}
