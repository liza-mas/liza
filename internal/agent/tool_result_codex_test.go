package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestCodexToolResultLaunchVersionGate(t *testing.T) {
	for value, want := range map[string]bool{"codex-cli 0.154.0": true, "codex-cli 0.154.1": true, "codex-cli 1.0.0": true, "codex-cli 0.153.9": false, "codex-cli 0.154.0-alpha": false, "codex-cli 0.160.0-alpha.1": true, "codex-cli 0.154.1-beta": true, "garbage": false} {
		if got := codexSupportsToolResultHooks(value); got != want {
			t.Fatalf("%q = %v", value, got)
		}
	}
}

func TestCodexToolResultLaunchWrapperRunsWithoutGlobalWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell wrapper")
	}
	dir := t.TempDir()
	codex := filepath.Join(dir, "fake codex ' binary")
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codex-cli 0.154.0'; exit 0; fi\nprintf '%s\\0' \"$@\"\n"
	if err := os.WriteFile(codex, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	launch, err := prepareCodexToolResultLaunch(dir, filepath.Join(dir, "engine ' executable"), codex)
	if err != nil {
		t.Fatal(err)
	}
	defer launch.Cleanup()
	output, err := exec.Command(launch.Wrapper, "app-server").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "hooks.PreToolUse") || !strings.Contains(string(output), "hooks.PostToolUse") || !strings.HasSuffix(string(output), "app-server\x00") {
		t.Fatalf("bad wrapper args: %q", output)
	}
	info, err := os.Stat(launch.Wrapper)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("wrapper permissions: %v %v", info, err)
	}
	for _, arg := range launch.Args {
		if !strings.Contains(string(output), arg+"\x00") {
			t.Fatalf("missing argument %q", arg)
		}
	}
	if strings.Contains(string(output), "--dangerously-bypass-approvals-and-sandbox\x00") {
		t.Fatal("managed wrapper must not pass a launch-time sandbox bypass; the rendered config keeps workspace-write")
	}
	if !strings.Contains(string(output), "--dangerously-bypass-hook-trust\x00") {
		t.Fatal("managed wrapper must keep hook-trust scoped to the engine's own generated hooks")
	}
	launch.Cleanup()
	if _, err = os.Stat(launch.Wrapper); err != nil {
		t.Fatalf("ACP wrapper did not survive prompt cleanup: %v", err)
	}
	repeated, err := prepareCodexToolResultLaunch(dir, filepath.Join(dir, "engine ' executable"), codex)
	if err != nil || repeated.Wrapper != launch.Wrapper {
		t.Fatalf("wrapper not content addressed: %#v %v", repeated, err)
	}
}

func TestCodexToolResultLaunchConcurrentPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell wrapper")
	}
	dir := t.TempDir()
	codex := filepath.Join(dir, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\necho 'codex-cli 0.154.0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := prepareCodexToolResultLaunch(dir, "/engine", codex); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCodexToolResultLaunchResolvesPathExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell wrapper")
	}
	bin := t.TempDir()
	codex := filepath.Join(bin, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\necho 'codex-cli 0.154.0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	launch, err := prepareCodexToolResultLaunch(t.TempDir(), os.Args[0], "codex")
	if err != nil || !filepath.IsAbs(launch.Wrapper) {
		t.Fatalf("launch=%+v err=%v", launch, err)
	}
}

func TestCodexToolResultLaunchDegradesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only native launch degradation")
	}
	launch, err := prepareCodexToolResultLaunch(`C:\project`, `C:\engine.exe`, `C:\codex.exe`)
	if err != nil {
		t.Fatalf("Windows must degrade without error, got %v", err)
	}
	if launch.Wrapper != `C:\codex.exe` || len(launch.Args) != 0 {
		t.Fatalf("Windows degrade must run Codex directly without a boundary: %#v", launch)
	}
}

// Stub only the native version probe; model execution remains owned by each
// existing fake provider scenario, including its failure and lifecycle checks.
const fakeCodexHookDiscovery = `if [ "$1" = --version ]; then echo 'codex-cli 0.154.0'; exit 0; fi
`
