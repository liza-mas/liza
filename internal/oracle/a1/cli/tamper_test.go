package cli

import (
	"bytes"
	"context"
	"encoding/json"
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

const packagePath = "./internal/oracle/a1/cli/..."
const packageDir = "internal/oracle/a1/cli"
const startingCommit = "96c9ad69aee983ce2b929dd681b6ba43bf920535"

func TestOracleCLITamper(t *testing.T) {
	if os.Getenv("ORACLE_CLI_TAMPER") != "1" {
		t.Skip("opt-in per-golden sensitivity proof")
	}
	root := t.TempDir()
	home, tmp := filepath.Join(root, "home"), filepath.Join(root, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: tmp, Go: true})
	if err != nil {
		t.Fatal(err)
	}
	assertEnvironment(t, env, false, true)
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
			t.Fatal("tamper proof child failed to launch")
		}
		if ctx.Err() != nil {
			t.Fatal("tamper proof child timed out")
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
			t.Fatalf("tamper proof setup/control failed (%s, exit %d): %s", filepath.Base(name), code, out)
		}
		return out
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Clean(filepath.Join(wd, "../../../.."))
	sha := strings.TrimSpace(require(source, "git", "rev-parse", "HEAD"))
	clone := filepath.Join(root, "candidate")
	require(root, "git", "clone", "--no-hardlinks", "--no-checkout", source, clone)
	require(clone, "git", "checkout", "--detach", sha)
	ledger := readFile(t, filepath.Join(clone, "internal", "oracle", "PROVENANCE"))
	if strings.TrimSpace(string(ledger)) != "start "+startingCommit {
		t.Fatal("unexpected provenance baseline")
	}
	require(clone, "git", "cat-file", "-e", startingCommit+"^{commit}")
	require(clone, "go", "run", "./internal/brandrender/cmd/sync-embedded", "--repo-root", ".")
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	live, regen := filepath.Join(root, "cli-tests"+ext), filepath.Join(root, "regen"+ext)
	// Cache executables only; every invocation still runs the candidate's real
	// CLI drivers, or materializes/builds the actual starting production tree.
	require(clone, "go", "test", "-c", "-o", live, "./internal/oracle/a1/cli")
	require(clone, "go", "build", "-o", regen, "./internal/testhelpers/oracle/cmd/regen")
	testDir := filepath.Join(clone, filepath.FromSlash(packageDir))
	liveArgs := []string{"-test.count=1", "-test.run=^TestOracle(Validate|ValidatePayload|PipelineRBAC|UsageReport|AnalysisReport|CLIComparison|CLIBoundary)$"}
	regenArgs := []string{"-repo", clone, "-mode", "check", "-pkg", packagePath}
	require(testDir, live, liveArgs...)
	control := require(clone, regen, regenArgs...)
	for _, path := range inventory {
		if !strings.Contains(control, "ok "+packageDir+"/testdata/golden/"+path+" H1") {
			t.Fatalf("baseline control omitted %s", path)
		}
	}
	for _, path := range inventory {
		t.Run(path, func(t *testing.T) {
			file := filepath.Join(testDir, "testdata", "golden", filepath.FromSlash(path))
			original := readFile(t, file)
			defer func() {
				if err := os.WriteFile(file, original, 0644); err != nil {
					t.Error("cannot restore tampered golden")
				}
			}()
			altered := tamperContent(t, path, original)
			if bytes.Equal(original, altered) {
				t.Fatal("tamper did not change asserted content")
			}
			if err := os.WriteFile(file, altered, 0644); err != nil {
				t.Fatal(err)
			}
			out, code := run(testDir, live, liveArgs...)
			if code != 1 || !strings.Contains(out, "golden mismatch: testdata/golden/"+path) {
				t.Fatalf("live proof failed to detect %s by comparison (exit %d): %s", path, code, out)
			}
			out, code = run(clone, regen, regenArgs...)
			if code != 1 || !strings.Contains(out, "unexplained "+packageDir+"/testdata/golden/"+path+": H1 reproduction differs") {
				t.Fatalf("baseline proof failed to detect %s by comparison (exit %d): %s", path, code, out)
			}
		})
	}
}

func tamperContent(t *testing.T, path string, original []byte) []byte {
	t.Helper()
	if strings.HasPrefix(path, "json/") {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(original, &value); err != nil {
			t.Fatal(err)
		}
		// A valid JSON scalar mutation, not malformed syntax or whitespace.
		value["exit_code"] = json.RawMessage("97")
		b, err := canonicalJSON(value)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if strings.HasPrefix(path, "state/") {
		needle := []byte("description: A1 CLI oracle")
		if !bytes.Contains(original, needle) {
			t.Fatal("state tamper anchor missing")
		}
		return bytes.Replace(original, needle, []byte("description: TAMPERED A1 CLI oracle"), 1)
	}
	// Human content mutation preserves layout and all framing delimiters.
	return bytes.Replace(original, []byte("exit_code: "), []byte("exit_code: 9"), 1)
}
