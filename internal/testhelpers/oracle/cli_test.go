package oracle

import (
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
)

const probePkg = "./internal/testhelpers/oracle/testdata/envprobe"

func probeNames(res Result) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(res.Stdout)), "\n") {
		if line != "" {
			names = append(names, envNames(runtime.GOOS, []string{line})...)
		}
	}
	sort.Strings(names)
	return names
}

func TestBuildCLIBuildsOncePerTestBinary(t *testing.T) {
	a := BuildCLI(t, probePkg)
	b := BuildCLI(t, probePkg)
	if a != b {
		t.Errorf("second call returned another binary: %s vs %s", a, b)
	}
	if n := builds[probePkg].count; n != 1 {
		t.Errorf("built %d times, want 1", n)
	}
}

func TestRunChildEnvironmentIsExactlyThePolicyPlusSuppliedPairs(t *testing.T) {
	t.Setenv("ORACLE_HOST_ONLY_PROBE", "1")
	bin := BuildCLI(t, probePkg)
	extra := []string{"ORACLE_SUPPLIED=1"}
	res := Run(t, bin, nil, RunOptions{Env: extra})
	if res.ExitCode != 0 {
		t.Fatalf("exit code %d, stderr %q", res.ExitCode, res.Stderr)
	}
	want, err := ChildEnv(EnvBase{Home: res.Home, Path: os.Getenv("PATH"), Tmp: res.Tmp}, extra...)
	if err != nil {
		t.Fatal(err)
	}
	got, wantNames := probeNames(res), envNames(runtime.GOOS, want)
	if strings.Join(got, ",") != strings.Join(wantNames, ",") {
		t.Errorf("child variable names %v, want %v", got, wantNames)
	}
	for _, name := range got {
		if name == "ORACLE_HOST_ONLY_PROBE" {
			t.Error("a host-only variable reached the child")
		}
	}
}

func TestRunUsesFreshDisposableProjectAndNormalisesIt(t *testing.T) {
	bin := BuildCLI(t, probePkg)
	first := Run(t, bin, []string{"cwd"}, RunOptions{})
	second := Run(t, bin, []string{"cwd"}, RunOptions{})
	if first.Project == second.Project {
		t.Error("project directory reused between runs")
	}
	for _, d := range []string{first.Project, first.Home, first.Tmp} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("disposable directory missing: %v", err)
		}
	}
	got := first.Norm.Text(strings.TrimSpace(string(first.Stdout)))
	if got != TokenProject {
		t.Errorf("working directory normalised to %q, want %s", got, TokenProject)
	}
}

func TestRunReportsExitCode(t *testing.T) {
	res := Run(t, BuildCLI(t, probePkg), []string{"fail"}, RunOptions{})
	if res.ExitCode != 3 {
		t.Errorf("exit code %d, want 3", res.ExitCode)
	}
}

func TestRunRejectsPolicyRedefinition(t *testing.T) {
	bin := BuildCLI(t, probePkg)
	msg := expectFailure(t, func(tb testing.TB) { Run(tb, bin, nil, RunOptions{Env: []string{"HOME=secret-value"}}) })
	if !strings.Contains(msg, "HOME") || strings.Contains(msg, "secret-value") {
		t.Errorf("message must name the variable and not carry its value: %q", msg)
	}
}

func TestBuildCLIFailsWithoutRunMain(t *testing.T) {
	saved := runRoot
	runRoot = ""
	defer func() { runRoot = saved }()
	msg := expectFailure(t, func(tb testing.TB) { BuildCLI(tb, probePkg) })
	if !strings.Contains(msg, "RunMain") {
		t.Errorf("unexpected message %q", msg)
	}
}
