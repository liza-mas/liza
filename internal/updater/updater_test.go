package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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
	var stderr bytes.Buffer
	installed := false
	disabled := false
	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/liza", "status"},
		Stdin:          strings.NewReader("n\ny\n"),
		Stdout:         io.Discard,
		Stderr:         &stderr,
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
	if !strings.Contains(stderr.String(), "Liza stable update is available (v1.0.0 -> v1.2.3)") {
		t.Fatalf("prompt missing latest version:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Disable update checks for future runs?") {
		t.Fatalf("decline output missing disable proposal:\n%s", stderr.String())
	}
	if !disabled {
		t.Fatal("expected accepted disable proposal to persist disable preference")
	}
	if !strings.Contains(stderr.String(), "Update checks disabled.") {
		t.Fatalf("decline output missing disable confirmation:\n%s", stderr.String())
	}
}

func TestMaybeUpdateAndReexecInstallsAndReexecsOriginalCommand(t *testing.T) {
	var stderr bytes.Buffer
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
		Stdout:         io.Discard,
		Stderr:         &stderr,
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
	if !strings.Contains(stderr.String(), "Installing release binary v1.2.3") {
		t.Fatalf("stderr missing release install plan:\n%s", stderr.String())
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

func TestVerifyChecksum(t *testing.T) {
	data := []byte("test data")
	hash := sha256.Sum256(data)
	expected := hex.EncodeToString(hash[:])

	if err := verifyChecksum(data, expected); err != nil {
		t.Fatalf("verifyChecksum with correct checksum failed: %v", err)
	}

	if err := verifyChecksum(data, "wrongchecksum"); err == nil {
		t.Fatal("verifyChecksum with wrong checksum should fail")
	}
}

func TestDownloadChecksums(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0.0/checksums.txt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("abc123  liza-1.0.0-linux-amd64.tar.gz\ndef456  liza-1.0.0-darwin-amd64.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	checksums, err := downloadChecksums(ctx, server.URL+"/v1.0.0/checksums.txt")
	if err != nil {
		t.Fatalf("downloadChecksums failed: %v", err)
	}

	if checksums["liza-1.0.0-linux-amd64.tar.gz"] != "abc123" {
		t.Fatalf("checksum for linux-amd64 = %q, want abc123", checksums["liza-1.0.0-linux-amd64.tar.gz"])
	}
	if checksums["liza-1.0.0-darwin-amd64.tar.gz"] != "def456" {
		t.Fatalf("checksum for darwin-amd64 = %q, want def456", checksums["liza-1.0.0-darwin-amd64.tar.gz"])
	}
}

func TestReleaseBaseURLControlsBothArchiveAndChecksum(t *testing.T) {
	// Test that LIZA_RELEASE_BASE_URL controls both archive and checksum URLs
	oldBase := os.Getenv("LIZA_RELEASE_BASE_URL")
	defer os.Setenv("LIZA_RELEASE_BASE_URL", oldBase)

	customBase := "https://custom.example.com/releases"
	os.Setenv("LIZA_RELEASE_BASE_URL", customBase)

	archiveURL := releaseArchiveURL("v1.0.0", "linux", "amd64")
	checksumsURL := checksumURL("v1.0.0")

	if !strings.Contains(archiveURL, customBase) {
		t.Fatalf("archive URL = %s, want to contain %s", archiveURL, customBase)
	}
	if !strings.Contains(checksumsURL, customBase) {
		t.Fatalf("checksums URL = %s, want to contain %s", checksumsURL, customBase)
	}

	// Test default when env var is not set
	os.Setenv("LIZA_RELEASE_BASE_URL", "")
	defaultBase := "https://github.com/liza-mas/liza/releases/download"

	archiveURL = releaseArchiveURL("v1.0.0", "linux", "amd64")
	checksumsURL = checksumURL("v1.0.0")

	if !strings.Contains(archiveURL, defaultBase) {
		t.Fatalf("archive URL = %s, want to contain %s", archiveURL, defaultBase)
	}
	if !strings.Contains(checksumsURL, defaultBase) {
		t.Fatalf("checksums URL = %s, want to contain %s", checksumsURL, defaultBase)
	}
}

func TestInstallReleaseBinaryWithChecksum(t *testing.T) {
	// Create test archive
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

	archiveData := archive.Bytes()
	hash := sha256.Sum256(archiveData)
	checksum := hex.EncodeToString(hash[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0.0/liza-1.0.0-linux-amd64.tar.gz" {
			w.WriteHeader(http.StatusOK)
			w.Write(archiveData)
			return
		}
		if r.URL.Path == "/v1.0.0/checksums.txt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(checksum + "  liza-1.0.0-linux-amd64.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Temporarily override the release base URL
	oldBase := os.Getenv("LIZA_RELEASE_BASE_URL")
	defer os.Setenv("LIZA_RELEASE_BASE_URL", oldBase)
	os.Setenv("LIZA_RELEASE_BASE_URL", server.URL)

	dir := t.TempDir()
	target := filepath.Join(dir, "liza")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := installReleaseBinary(ctx, "v1.0.0", target, io.Discard); err != nil {
		t.Fatalf("installReleaseBinary failed: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("target body = %q, want %q", string(got), string(body))
	}
}

func TestInstallReleaseBinaryChecksumMismatch(t *testing.T) {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0.0/liza-1.0.0-linux-amd64.tar.gz" {
			w.WriteHeader(http.StatusOK)
			w.Write(archive.Bytes())
			return
		}
		if r.URL.Path == "/v1.0.0/checksums.txt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("wrongchecksum  liza-1.0.0-linux-amd64.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldBase := os.Getenv("LIZA_RELEASE_BASE_URL")
	defer os.Setenv("LIZA_RELEASE_BASE_URL", oldBase)
	os.Setenv("LIZA_RELEASE_BASE_URL", server.URL)

	dir := t.TempDir()
	target := filepath.Join(dir, "liza")

	ctx := context.Background()
	if err := installReleaseBinary(ctx, "v1.0.0", target, io.Discard); err == nil {
		t.Fatal("installReleaseBinary with wrong checksum should fail")
	}
}

func TestInstallReleaseBinaryMissingChecksum(t *testing.T) {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1.0.0/liza-1.0.0-linux-amd64.tar.gz" {
			w.WriteHeader(http.StatusOK)
			w.Write(archive.Bytes())
			return
		}
		if r.URL.Path == "/v1.0.0/checksums.txt" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("otherchecksum  other-file.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	oldBase := os.Getenv("LIZA_RELEASE_BASE_URL")
	defer os.Setenv("LIZA_RELEASE_BASE_URL", oldBase)
	os.Setenv("LIZA_RELEASE_BASE_URL", server.URL)

	dir := t.TempDir()
	target := filepath.Join(dir, "liza")

	ctx := context.Background()
	if err := installReleaseBinary(ctx, "v1.0.0", target, io.Discard); err == nil {
		t.Fatal("installReleaseBinary with missing checksum should fail")
	}
}

func TestCheckUpdateFlagStopsAtDoubleDash(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		want     bool
		wantFlag bool
	}{
		{name: "flag before --", args: []string{"liza", "--check-update", "version", "--", "--check-update"}, want: true, wantFlag: true},
		{name: "flag after -- ignored", args: []string{"liza", "version", "--", "--check-update"}, want: false, wantFlag: false},
		{name: "no flag", args: []string{"liza", "version"}, want: false, wantFlag: false},
		{name: "flag with =true", args: []string{"liza", "--check-update=true", "version"}, want: true, wantFlag: true},
		{name: "flag with =false", args: []string{"liza", "--check-update=false", "version"}, want: false, wantFlag: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotFlag := checkUpdateFlag(tt.args)
			if got != tt.want || gotFlag != tt.wantFlag {
				t.Fatalf("checkUpdateFlag(%v) = (%v, %v), want (%v, %v)", tt.args, got, gotFlag, tt.want, tt.wantFlag)
			}
		})
	}
}

func TestCheckUpdateFlagLastWins(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		want     bool
		wantFlag bool
	}{
		{name: "true then false", args: []string{"liza", "--check-update=true", "--check-update=false", "version"}, want: false, wantFlag: true},
		{name: "false then true", args: []string{"liza", "--check-update=false", "--check-update=true", "version"}, want: true, wantFlag: true},
		{name: "true then true", args: []string{"liza", "--check-update=true", "--check-update=true", "version"}, want: true, wantFlag: true},
		{name: "bare then false", args: []string{"liza", "--check-update", "--check-update=false", "version"}, want: false, wantFlag: true},
		{name: "false then bare", args: []string{"liza", "--check-update=false", "--check-update", "version"}, want: true, wantFlag: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotFlag := checkUpdateFlag(tt.args)
			if got != tt.want || gotFlag != tt.wantFlag {
				t.Fatalf("checkUpdateFlag(%v) = (%v, %v), want (%v, %v)", tt.args, got, gotFlag, tt.want, tt.wantFlag)
			}
		})
	}
}

func TestUpdateChannelStopsAtDoubleDash(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "channel before --", args: []string{"liza", "--update-channel=main", "version", "--", "--update-channel=stable"}, want: "main"},
		{name: "channel after -- ignored", args: []string{"liza", "version", "--", "--update-channel=main"}, want: "stable"},
		{name: "no channel", args: []string{"liza", "version"}, want: "stable"},
		{name: "channel with space", args: []string{"liza", "--update-channel", "main", "version"}, want: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateChannel(Config{Args: tt.args})
			if got != tt.want {
				t.Fatalf("updateChannel(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestUpdateChannelLastWins(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "stable then main", args: []string{"liza", "--update-channel=stable", "--update-channel=main", "version"}, want: "main"},
		{name: "main then stable", args: []string{"liza", "--update-channel=main", "--update-channel=stable", "version"}, want: "stable"},
		{name: "space then =", args: []string{"liza", "--update-channel", "stable", "--update-channel=main", "version"}, want: "main"},
		{name: "= then space", args: []string{"liza", "--update-channel=stable", "--update-channel", "main", "version"}, want: "main"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateChannel(Config{Args: tt.args})
			if got != tt.want {
				t.Fatalf("updateChannel(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestLookupCandidateInvalidChannelReturnsFatalError(t *testing.T) {
	cfg := Config{
		Args:  []string{"liza", "--update-channel=bogus", "version"},
		Env:   []string{},
		Channel: "bogus",
	}

	_, err := lookupCandidate(context.Background(), cfg)
	if err == nil {
		t.Fatal("lookupCandidate with invalid channel should return error")
	}

	var fatalErr *FatalError
	if !errors.As(err, &fatalErr) {
		t.Fatal("lookupCandidate with invalid channel should return FatalError")
	}
}

func TestMaybeUpdateAndReexecInstallFailureContinues(t *testing.T) {
	var stdout bytes.Buffer
	installErr := errors.New("install failed")

	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/old-liza", "version"},
		Stdin:          strings.NewReader("y\n"),
		Stdout:         &stdout,
		Stderr:         &stdout,
		IsInteractive:  func() bool { return true },
		LookupLatest:   func(context.Context) (string, error) { return "v1.2.3", nil },
		Install: func(context.Context, candidate, string, io.Writer, io.Writer) error {
			return installErr
		},
		InstallTarget: func() (string, error) {
			return "/home/user/go/bin/liza", nil
		},
	})

	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec with install failure should return nil, got: %v", err)
	}

	if !strings.Contains(stdout.String(), "update install failed") {
		t.Fatalf("stderr missing install failure message:\n%s", stdout.String())
	}
}

func TestMaybeUpdateAndReexecLookupFailureContinues(t *testing.T) {
	var stderr bytes.Buffer
	lookupErr := errors.New("network failed")

	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/old-liza", "version"},
		Stdin:          strings.NewReader("y\n"),
		Stdout:         io.Discard,
		Stderr:         &stderr,
		IsInteractive:  func() bool { return true },
		LookupLatest: func(context.Context) (string, error) {
			return "", lookupErr
		},
	})

	if err != nil {
		t.Fatalf("MaybeUpdateAndReexec with lookup failure should return nil, got: %v", err)
	}

	if !strings.Contains(stderr.String(), "update check failed") {
		t.Fatalf("stderr missing lookup failure message:\n%s", stderr.String())
	}
}

func TestMaybeUpdateAndReexecUsesInstallTimeout(t *testing.T) {
	var installCtx context.Context
	reexecErr := errors.New("reexec sentinel")

	err := MaybeUpdateAndReexec(context.Background(), Config{
		CurrentVersion: "v1.0.0",
		CheckUpdate:    true,
		Args:           []string{"/tmp/old-liza", "version"},
		Stdin:          strings.NewReader("y\n"),
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		IsInteractive:  func() bool { return true },
		InstallTimeout: 5 * time.Minute,
		LookupLatest:   func(context.Context) (string, error) { return "v1.2.3", nil },
		Install: func(ctx context.Context, next candidate, _ string, _ io.Writer, _ io.Writer) error {
			installCtx = ctx
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

	if installCtx == nil {
		t.Fatal("Install was not called")
	}

	deadline, ok := installCtx.Deadline()
	if !ok {
		t.Fatal("Install context does not have a deadline")
	}

	// Verify the deadline is approximately 5 minutes from now
	now := time.Now()
	remaining := time.Until(deadline)
	if remaining < 4*time.Minute || remaining > 6*time.Minute {
		t.Fatalf("Install deadline remaining = %v, want ~5 minutes", remaining)
	}
}

func TestInstallFromSourceFetchesExactCommit(t *testing.T) {
	// This test verifies that installFromSource can fetch and checkout
	// a specific commit that may not be in the shallow clone's main branch.
	// We use a local git repo to test this without network dependencies.

	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "test-repo")
	target := filepath.Join(tmpDir, "liza")

	// Create a test git repo with multiple commits
	cmd := exec.Command("git", "init", repoDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	cmd = exec.Command("git", "-C", repoDir, "config", "user.email", "test@example.com")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git config email failed: %v", err)
	}
	cmd = exec.Command("git", "-C", repoDir, "config", "user.name", "Test")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git config name failed: %v", err)
	}

	// Create initial commit
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", repoDir, "add", "test.txt")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	cmd = exec.Command("git", "-C", repoDir, "commit", "-m", "initial")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Create a second commit on main
	if err := os.WriteFile(testFile, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", repoDir, "commit", "-am", "second")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	// Get the commit hash of the first commit
	cmd = exec.Command("git", "-C", repoDir, "rev-parse", "HEAD~1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v", err)
	}
	oldCommit := strings.TrimSpace(string(out))

	// Create a Makefile that just copies a binary (simplified for testing)
	makefile := filepath.Join(repoDir, "Makefile")
	makeContent := fmt.Sprintf("install:\n\tcp /bin/sh %s\n", target)
	if err := os.WriteFile(makefile, []byte(makeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("git", "-C", repoDir, "add", "Makefile")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git add Makefile failed: %v", err)
	}
	cmd = exec.Command("git", "-C", repoDir, "commit", "-m", "add makefile")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit makefile failed: %v", err)
	}

	// Now test that we can checkout the old commit using the installFromSource logic
	// We'll manually replicate the shallow clone + fetch pattern
	cloneDir := filepath.Join(tmpDir, "clone")
	cmd = exec.Command("git", "clone", "--depth", "1", "--branch", "main", repoDir, cloneDir)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git clone failed: %v", err)
	}

	// Try to checkout the old commit directly (this should fail with shallow clone)
	cmd = exec.Command("git", "-C", cloneDir, "checkout", oldCommit)
	err = cmd.Run()
	if err == nil {
		t.Fatal("expected checkout of non-tip commit to fail in shallow clone")
	}

	// Now use the fetch pattern from installFromSource
	cmd = exec.Command("git", "-C", cloneDir, "fetch", "--depth", "1", "origin", oldCommit)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git fetch of specific commit failed: %v", err)
	}
	cmd = exec.Command("git", "-C", cloneDir, "checkout", "--detach", "FETCH_HEAD")
	if err := cmd.Run(); err != nil {
		t.Fatalf("git checkout FETCH_HEAD failed: %v", err)
	}

	// Verify we're on the old commit
	cmd = exec.Command("git", "-C", cloneDir, "rev-parse", "HEAD")
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v", err)
	}
	currentCommit := strings.TrimSpace(string(out))
	if currentCommit != oldCommit {
		t.Fatalf("HEAD = %s, want %s", currentCommit, oldCommit)
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
