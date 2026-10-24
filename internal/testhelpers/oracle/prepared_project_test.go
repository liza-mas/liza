package oracle

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const preparedProbePkg = "./internal/testhelpers/oracle/testdata/preparedprobe"

// The caller, rather than the runner, materialises and owns each case.
func preparedCase(t *testing.T) (string, EnvBase) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal("resolve disposable root")
	}
	project := filepath.Join(root, "project")
	base := EnvBase{Home: filepath.Join(root, "home"), Path: "", Tmp: filepath.Join(root, "tmp")}
	for _, dir := range []string{project, base.Home, base.Tmp} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal("create disposable directory")
		}
	}
	preparedWrite(t, filepath.Join(project, "caller.txt"), "caller fixture\n")
	return project, base
}

func preparedWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal("write disposable fixture")
	}
}

func preparedRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read disposable fixture")
	}
	return data
}

func preparedAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("unexpected fixture effect or inaccessible fixture")
	}
}

func preparedOK(t *testing.T, res Result) {
	t.Helper()
	if res.ExitCode != 0 || len(res.Stderr) != 0 {
		t.Fatalf("fixture success=%t, stderr empty=%t, exit code=%d", res.ExitCode == 0, len(res.Stderr) == 0, res.ExitCode)
	}
}

func TestPreparedProjectSequence(t *testing.T) {
	bin := BuildCLI(t, preparedProbePkg)
	project, base := preparedCase(t)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git executable unavailable")
	}
	preparedOK(t, RunInProject(t, git, []string{"init", "--quiet"}, project, base, RunOptions{}))
	for _, step := range []string{"prepare-1", "prepare-2"} {
		preparedOK(t, RunInProject(t, bin, []string{step}, project, base, RunOptions{}))
	}
	before := preparedRead(t, filepath.Join(project, "state.txt"))
	res := RunInProject(t, bin, []string{"tested"}, project, base, RunOptions{})
	after := preparedRead(t, filepath.Join(project, "state.txt"))
	preparedOK(t, res)
	if string(before) != "ready\n" || string(after) != "complete\n" {
		t.Error("tested step must change the prepared state to the final state")
	}
	if res.Project != project || res.Home != base.Home || res.Tmp != base.Tmp {
		t.Error("result must retain the caller directories")
	}
	for _, check := range []struct{ path, token string }{
		{project, TokenProject}, {base.Home, TokenHome}, {base.Tmp, TokenTmp},
	} {
		if string(preparedRead(t, filepath.Join(check.path, "step-1"))) != "first\n" {
			t.Error("preparation did not persist in the caller directory")
		}
		if res.Norm.Text(check.path) != check.token {
			t.Error("result normaliser did not register a caller directory")
		}
	}
	if string(preparedRead(t, filepath.Join(project, "step-2"))) != "second\n" ||
		string(preparedRead(t, filepath.Join(project, "result"))) != "final\n" ||
		string(preparedRead(t, filepath.Join(project, "caller.txt"))) != "caller fixture\n" {
		t.Error("caller preparation or final files changed unexpectedly")
	}
	if info, err := os.Stat(filepath.Join(project, ".git")); err != nil || !info.IsDir() {
		t.Error("caller Git repository must survive the sequence")
	}

	t.Run("failed preparation stops driver", func(t *testing.T) {
		project, base := preparedCase(t)
		var finalAttempted bool
		var result Result
		// Sequencing belongs to the A5-style driver. RunInProject returns the
		// preparation's ordinary exit code; the driver refuses to continue.
		for _, step := range []string{"fail", "tested"} {
			if step == "tested" {
				finalAttempted = true
			}
			result = RunInProject(t, bin, []string{step}, project, base, RunOptions{})
			if result.ExitCode != 0 {
				break
			}
		}
		if result.ExitCode != 7 || finalAttempted {
			t.Error("failed preparation must retain its exit code and stop the driver")
		}
		preparedAbsent(t, filepath.Join(project, "tested-invoked"))
	})
}

func TestPreparedProjectIsolation(t *testing.T) {
	bin := BuildCLI(t, preparedProbePkg)
	first, firstBase := preparedCase(t)
	second, secondBase := preparedCase(t)
	preparedOK(t, RunInProject(t, bin, []string{"prepare-1"}, first, firstBase, RunOptions{}))
	// Independent observers read both cases concurrently; no fixture is mutated.
	for _, tc := range []struct {
		name, project string
		base          EnvBase
		want          string
	}{
		{"earlier effect visible", first, firstBase, "sentinel=true\n"},
		{"independent case empty", second, secondBase, "sentinel=false\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := RunInProject(t, bin, []string{"observe"}, tc.project, tc.base, RunOptions{})
			preparedOK(t, res)
			if string(res.Stdout) != tc.want {
				t.Error("case observer reported the wrong sentinel presence")
			}
		})
	}
}

func TestPreparedProjectEnvironment(t *testing.T) {
	t.Setenv("ORACLE_PREPARED_HOST_ONLY", "host sentinel")
	bin := BuildCLI(t, preparedProbePkg)
	project, base := preparedCase(t)
	opts := RunOptions{Env: []string{"ORACLE_PREPARED_EXTRA=fixture"}, Stdin: []byte("fixture input\n")}
	res := RunInProject(t, bin, []string{"environment"}, project, base, opts)
	preparedOK(t, res)
	wantNames := []string{"HOME", "PATH", "TMPDIR", "ORACLE_PREPARED_EXTRA"}
	if runtime.GOOS == "windows" {
		wantNames = append(wantNames, "USERPROFILE", "TMP", "TEMP", "SYSTEMROOT")
	}
	sort.Strings(wantNames)
	want := "extra=true\nstdin=true\npath-empty=true\nhost-absent=true\nnames=" + strings.Join(wantNames, ",") + "\n"
	if string(res.Stdout) != want {
		t.Error("child environment names or fixture input booleans differ")
	}
	failed := RunInProject(t, bin, []string{"fail"}, project, base, RunOptions{})
	if failed.ExitCode != 7 || string(failed.Stdout) != "stdout=true\n" || string(failed.Stderr) != "stderr=true\n" {
		t.Error("stdout, stderr and nonzero exit must remain distinct")
	}
	policyNames := []string{"HOME", "PATH", "TMPDIR"}
	if runtime.GOOS == "windows" {
		policyNames = append(policyNames, "USERPROFILE", "TMP", "TEMP", "SYSTEMROOT", "home")
	}
	for _, name := range policyNames {
		t.Run(name, func(t *testing.T) {
			msg := expectFailure(t, func(tb testing.TB) {
				RunInProject(tb, bin, []string{"touch"}, project, base, RunOptions{Env: []string{name + "=forbidden-fixture"}})
			})
			if !strings.Contains(msg, name) || strings.Contains(msg, "forbidden-fixture") {
				t.Error("policy diagnostic must name the variable and omit its value")
			}
			preparedAbsent(t, filepath.Join(project, "executed"))
		})
	}
}

func TestPreparedProjectInvalidDirectories(t *testing.T) {
	bin := BuildCLI(t, preparedProbePkg)
	for _, field := range []string{"project", "HOME", "TMPDIR"} {
		for _, shape := range []string{"empty", "relative", "missing", "file"} {
			t.Run(field+"/"+shape, func(t *testing.T) {
				project, base := preparedCase(t)
				validProject := project
				file := filepath.Join(project, "not-a-directory")
				preparedWrite(t, file, "keep\n")
				bad := map[string]string{"empty": "", "relative": ".", "missing": filepath.Join(project, "missing"), "file": file}[shape]
				switch field {
				case "project":
					project = bad
				case "HOME":
					base.Home = bad
				case "TMPDIR":
					base.Tmp = bad
				}
				msg := expectFailure(t, func(tb testing.TB) {
					// The absolute marker remains reachable even for an invalid cwd.
					RunInProject(tb, bin, []string{"touch", filepath.Join(validProject, "executed")}, project, base, RunOptions{})
				})
				if msg != "oracle: "+field+" must be an existing absolute directory" {
					t.Error("directory diagnostic must name the field and omit its value")
				}
				preparedAbsent(t, filepath.Join(validProject, "executed"))
				if string(preparedRead(t, file)) != "keep\n" || string(preparedRead(t, filepath.Join(validProject, "caller.txt"))) != "caller fixture\n" {
					t.Error("invalid input must not clear or replace a valid fixture")
				}
			})
		}
	}
}

func TestProviderCorrectionA5Start(t *testing.T) {
	runCorrectionAtStart(t, "^(TestPreparedProjectSequence|TestPreparedProjectIsolation|TestPreparedProjectEnvironment|TestPreparedProjectInvalidDirectories)$")
}

func TestPreparedProjectStartFailure(t *testing.T) {
	project, base := preparedCase(t)
	msg := expectFailure(t, func(tb testing.TB) {
		RunInProject(tb, filepath.Join(project, "missing-binary"), nil, project, base, RunOptions{})
	})
	if !strings.Contains(msg, "failed to start") {
		t.Error("a spawn failure must remain fatal")
	}
	if !bytes.Equal(preparedRead(t, filepath.Join(project, "caller.txt")), []byte("caller fixture\n")) {
		t.Error("spawn failure changed caller fixture")
	}
}
