package a5

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const (
	tamperEnv      = "ORACLE_A5_TAMPER"
	startingCommit = "96c9ad69aee983ce2b929dd681b6ba43bf920535"
	packageDir     = "internal/oracle/a5"
	tamperedGolden = "json/SR1.json"
)

// TestA5GoldenSensitivity proves the A5 checks are sensitive to a single golden
// byte. In a scratch clone that retains the starting history it first requires
// the intact live and regeneration checks to pass, alters one content byte of
// one committed golden, then requires both checks to fail naming that golden.
// The source worktree and live runtime state are never touched.
func TestA5GoldenSensitivity(t *testing.T) {
	if os.Getenv(tamperEnv) != "1" {
		t.Skip("opt-in single-byte golden sensitivity proof; set " + tamperEnv + "=1")
	}
	if os.Getenv(oracle.WriteEnv) == "1" {
		t.Skip("sensitivity proof compares committed goldens; it does not run in write mode")
	}
	root := t.TempDir()
	home, tmp := filepath.Join(root, "home"), filepath.Join(root, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A child environment never carries the opt-in flag, so a nested run of
	// this package cannot recurse into the proof.
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: tmp, Go: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, kv := range env {
		if name, _, _ := strings.Cut(kv, "="); name == tamperEnv {
			t.Fatal("child environment carries the opt-in flag")
		}
	}
	run := func(dir, name string, args ...string) (string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		code := 0
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else if err != nil {
			t.Fatalf("sensitivity child %s failed to launch", filepath.Base(name))
		}
		if ctx.Err() != nil {
			t.Fatalf("sensitivity child %s timed out", filepath.Base(name))
		}
		// Never print policy environment values, even if a tool emits them.
		detail := string(out)
		for _, kv := range env {
			name, value, _ := strings.Cut(kv, "=")
			if value != "" && name != "GOFLAGS" {
				detail = strings.ReplaceAll(detail, value, "<"+name+">")
			}
		}
		return detail, code
	}
	require := func(dir, name string, args ...string) string {
		t.Helper()
		out, code := run(dir, name, args...)
		if code != 0 {
			t.Fatalf("sensitivity setup or control failed (%s, exit %d): %s", filepath.Base(name), code, out)
		}
		return out
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Clean(filepath.Join(wd, "../../.."))
	sha := strings.TrimSpace(require(source, "git", "rev-parse", "HEAD"))
	clone := filepath.Join(root, "candidate")
	require(root, "git", "clone", "--no-hardlinks", "--no-checkout", source, clone)
	require(clone, "git", "checkout", "--detach", sha)
	require(clone, "git", "cat-file", "-e", startingCommit+"^{commit}")
	require(clone, "go", "run", "./internal/brandrender/cmd/sync-embedded", "--repo-root", ".")

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	live, regen := filepath.Join(root, "a5-tests"+ext), filepath.Join(root, "regen"+ext)
	require(clone, "go", "test", "-c", "-o", live, "./"+packageDir)
	require(clone, "go", "build", "-o", regen, "./internal/testhelpers/oracle/cmd/regen")
	testDir := filepath.Join(clone, filepath.FromSlash(packageDir))
	// The live selection excludes this test, so the nested run cannot recurse.
	liveArgs := []string{"-test.count=1", "-test.run=^TestOracleA5$"}
	regenArgs := []string{"-repo", clone, "-mode", "check", "-pkg", "./" + packageDir + "/..."}

	// Control: the intact tree passes both checks and the regeneration check
	// reports the golden by name, so a later failure cannot be a setup error.
	require(testDir, live, liveArgs...)
	control := require(clone, regen, regenArgs...)
	if !strings.Contains(control, "ok "+packageDir+"/testdata/golden/"+tamperedGolden+" H1") {
		t.Fatalf("regeneration control omitted %s", tamperedGolden)
	}

	file := filepath.Join(testDir, "testdata", "golden", filepath.FromSlash(tamperedGolden))
	original := read(t, file)
	defer func() {
		if err := os.WriteFile(file, original, 0o644); err != nil {
			t.Error("cannot restore the tampered golden")
		}
	}()
	needle := []byte(`"exit_code": 0`)
	if !bytes.Contains(original, needle) {
		t.Fatal("tamper anchor missing")
	}
	// One content byte, a valid JSON scalar: not syntax damage or whitespace.
	altered := bytes.Replace(original, needle, []byte(`"exit_code": 1`), 1)
	if len(altered) != len(original) || bytes.Equal(altered, original) {
		t.Fatal("tamper did not change exactly one byte")
	}
	if err := os.WriteFile(file, altered, 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(testDir, live, liveArgs...)
	if code != 1 || !strings.Contains(out, "testdata/golden/"+tamperedGolden) || !strings.Contains(out, "differs at line") {
		t.Fatalf("live check did not report the altered golden by comparison (exit %d): %s", code, out)
	}
	out, code = run(clone, regen, regenArgs...)
	if code != 1 || !strings.Contains(out, "unexplained "+packageDir+"/testdata/golden/"+tamperedGolden+": H1 reproduction differs") {
		t.Fatalf("regeneration check did not report the altered golden by comparison (exit %d): %s", code, out)
	}
}
