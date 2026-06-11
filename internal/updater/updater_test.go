package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseLatestVersion(t *testing.T) {
	got, err := parseLatestVersion([]byte(`{"Path":"github.com/liza-mas/liza","Version":"v1.2.3"}`))
	if err != nil {
		t.Fatalf("parseLatestVersion returned error: %v", err)
	}
	if got != "v1.2.3" {
		t.Fatalf("version = %q, want v1.2.3", got)
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "newer", current: "0.4.0", latest: "v0.5.0", want: true},
		{name: "same", current: "v0.5.0", latest: "v0.5.0", want: false},
		{name: "older latest", current: "v0.6.0", latest: "v0.5.0", want: false},
		{name: "dev skips", current: "dev", latest: "v0.5.0", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewerVersion(tt.current, tt.latest); got != tt.want {
				t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestMaybeUpdateAndReexecSkipsDevBuild(t *testing.T) {
	called := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "dev",
		CheckUpdate:    true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			called = true
			return "v1.2.3", nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if called {
		t.Fatal("expected dev build to skip latest lookup")
	}
}

func TestMaybeUpdateAndReexecSkipsWhenCheckUpdateOff(t *testing.T) {
	called := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			called = true
			return "v1.2.3", nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if called {
		t.Fatal("expected default check-update off to skip latest lookup")
	}
}

func TestMaybeUpdateAndReexecSavedDisableSuppressesEnvCheck(t *testing.T) {
	called := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		Env:            []string{"LIZA_CHECK_UPDATE=1"},
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		CheckDisabled:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			called = true
			return "v1.2.3", nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if called {
		t.Fatal("expected saved disable to suppress env-driven update check")
	}
}

func TestMaybeUpdateAndReexecExplicitFlagOverridesSavedDisable(t *testing.T) {
	called := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		Args:           []string{"liza", "--check-update", "version"},
		Stdin:          strings.NewReader("n\nn\n"),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		CheckDisabled:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			called = true
			return "v1.2.3", nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if !called {
		t.Fatal("expected explicit --check-update to run despite saved disable")
	}
}

func TestMaybeUpdateAndReexecSkipsNonInteractive(t *testing.T) {
	called := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return false },
		LookupLatest: func(context.Context) (string, error) {
			called = true
			return "v1.2.3", nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if called {
		t.Fatal("expected non-interactive run to skip latest lookup")
	}
}

func TestMaybeUpdateAndReexecDeclineRunsOriginalVersion(t *testing.T) {
	var stdout bytes.Buffer
	installed := false
	disabled := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/liza", "status"},
		Stdin:          strings.NewReader("n\ny\n"),
		Stdout:         &stdout,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		LookupLatest:   func(context.Context) (string, error) { return "v1.2.3", nil },
		Install: func(context.Context, candidate, string, io.Writer, io.Writer) error {
			installed = true
			return nil
		},
		DisableCheck: func() error {
			disabled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec returned error: %v", err)
	}
	if installed {
		t.Fatal("expected declined update not to install")
	}
	if !strings.Contains(stdout.String(), "Liza stable update is available (v1.0.0 -> v1.2.3)") {
		t.Fatalf("prompt missing latest version:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Disable update checks for future runs?") {
		t.Fatalf("decline output missing disable proposal:\n%s", stdout.String())
	}
	if !disabled {
		t.Fatal("expected accepted disable proposal to persist disable preference")
	}
	if !strings.Contains(stdout.String(), "Update checks disabled.") {
		t.Fatalf("decline output missing disable confirmation:\n%s", stdout.String())
	}
}

func TestMaybeUpdateAndReexecInstallsAndReexecsOriginalCommand(t *testing.T) {
	var stdout bytes.Buffer
	var installedVersion string
	var execPath string
	var execArgs []string
	var execEnv []string
	reexecErr := errors.New("reexec sentinel")

	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/old-liza", "agent", "orchestrator", "--agent-id", "orchestrator-1"},
		Env:            []string{"PATH=/bin"},
		Stdin:          strings.NewReader("yes\n"),
		Stdout:         &stdout,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		LookupLatest:   func(context.Context) (string, error) { return "v1.2.3", nil },
		Install: func(_ context.Context, next candidate, _ string, _ io.Writer, _ io.Writer) error {
			installedVersion = next.Ref
			return nil
		},
		InstallTarget: func() (string, error) {
			return "/home/user/go/bin/liza", nil
		},
		Reexec: func(path string, args []string, env []string) error {
			execPath = path
			execArgs = append([]string(nil), args...)
			execEnv = append([]string(nil), env...)
			return reexecErr
		},
	})
	if !errors.Is(err, reexecErr) {
		t.Fatalf("MaybeUpdateAndReexec error = %v, want reexec sentinel", err)
	}
	if installedVersion != "v1.2.3" {
		t.Fatalf("installed version = %q, want v1.2.3", installedVersion)
	}
	if execPath != "/home/user/go/bin/liza" {
		t.Fatalf("exec path = %q, want installed liza path", execPath)
	}
	wantArgs := []string{"liza", "agent", "orchestrator", "--agent-id", "orchestrator-1"}
	if !slices.Equal(execArgs, wantArgs) {
		t.Fatalf("exec args = %v, want %v", execArgs, wantArgs)
	}
	if !slices.Contains(execEnv, "LIZA_SKIP_AUTO_UPDATE=1") {
		t.Fatalf("exec env missing skip marker: %v", execEnv)
	}
	if !strings.Contains(stdout.String(), "Installing release binary v1.2.3") {
		t.Fatalf("stdout missing release install plan:\n%s", stdout.String())
	}
}

func TestMaybeUpdateAndReexecMainChannelInstallsCommitRef(t *testing.T) {
	var installedRef string
	reexecErr := errors.New("reexec sentinel")
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.2.3",
		CurrentCommit:  "111111111111",
		CheckUpdate:    true,
		Channel:        "main",
		Args:           []string{"/tmp/old-liza", "version"},
		Stdin:          strings.NewReader("y\n"),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			t.Fatal("stable lookup should not run for main channel")
			return "", nil
		},
		LookupMain: func(context.Context) (string, error) {
			return "222222222222abcdef", nil
		},
		Install: func(_ context.Context, next candidate, _ string, _ io.Writer, _ io.Writer) error {
			installedRef = next.Ref
			return nil
		},
		InstallTarget: func() (string, error) {
			return "/home/user/go/bin/liza", nil
		},
		Reexec: func(string, []string, []string) error {
			return reexecErr
		},
	})
	if !errors.Is(err, reexecErr) {
		t.Fatalf("MaybeUpdateAndReexec error = %v, want reexec sentinel", err)
	}
	if installedRef != "222222222222abcdef" {
		t.Fatalf("installed ref = %q, want main commit hash", installedRef)
	}
}

func TestUpdateChannelFlagOverridesEnv(t *testing.T) {
	got := updateChannel(Config{
		Args: []string{"liza", "--update-channel=main", "version"},
		Env:  []string{"LIZA_UPDATE_CHANNEL=stable"},
	})
	if got != "main" {
		t.Fatalf("channel = %q, want main", got)
	}
}

func TestParseMainCommit(t *testing.T) {
	got, err := parseMainCommit([]byte(`{"Origin":{"Hash":"78b087c5d0891685f79ce56103ba592832ca7b64"}}`))
	if err != nil {
		t.Fatalf("parseMainCommit returned error: %v", err)
	}
	if got != "78b087c5d0891685f79ce56103ba592832ca7b64" {
		t.Fatalf("commit = %q", got)
	}
}

func TestInstallBinaryFromTarGzReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "liza")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho new\n")
	if err := tw.WriteHeader(&tar.Header{
		Name: "liza",
		Mode: 0o755,
		Size: int64(len(body)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	if err := installBinaryFromTarGz(bytes.NewReader(archive.Bytes()), target); err != nil {
		t.Fatalf("installBinaryFromTarGz returned error: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("target body = %q, want %q", string(got), string(body))
	}
}

func TestDisableUpdateChecksPersistsGlobalPreference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if UpdateChecksDisabled() {
		t.Fatal("expected update checks not to start disabled")
	}
	if err := DisableUpdateChecks(); err != nil {
		t.Fatalf("DisableUpdateChecks returned error: %v", err)
	}
	if !UpdateChecksDisabled() {
		t.Fatal("expected persisted preference to disable update checks")
	}
}
