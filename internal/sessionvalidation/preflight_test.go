package sessionvalidation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestPreflightEnvironmentOverlayAndFingerprint(t *testing.T) {
	root := t.TempDir()
	base := []string{"PATH=/old", "SESSION_VALUE=before", "SESSION_VALUE=last"}
	original := append([]string{}, base...)
	if err := os.WriteFile(filepath.Join(root, "runtime.conf"), []byte("# comment\nPATH=/selected\nSESSION_VALUE=after # note\nEMPTY=\n"), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := ResolveEnvironment(base, root, []string{"runtime.conf"}, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"EMPTY=", "PATH=/selected", "SESSION_VALUE=after"}
	if !reflect.DeepEqual(env, want) || !reflect.DeepEqual(base, original) {
		t.Fatalf("overlay or copy semantics incorrect: got %v", env)
	}
	if Fingerprint(env) != Fingerprint([]string{"SESSION_VALUE=after", "EMPTY=", "PATH=/selected"}) {
		t.Fatal("equivalent environments have different fingerprints")
	}
	if Fingerprint(env) == Fingerprint(base) || Domain() == "" {
		t.Fatal("changed environment must differ in a nonempty process domain")
	}
	if _, err := ResolveEnvironment(base, root, []string{"absent.conf"}, true); err != nil {
		t.Fatalf("missing catalog default must be optional: %v", err)
	}
	for _, optional := range []bool{false, true} {
		_, err := ResolveEnvironment(base, root, []string{"."}, optional)
		assertPreflightCode(t, err, "env_file_unavailable")
	}
	_, err = ResolveEnvironment(base, root, []string{"absent.conf"}, false)
	assertPreflightCode(t, err, "env_file_unavailable")
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), "absent.conf") {
		t.Fatal("file diagnostics disclosed configured path")
	}
}

func TestPreflightRequiredEnvironmentRepair(t *testing.T) {
	contract := []models.ValidationPrerequisite{{Command: "validate", Env: []string{"REQUIRED_VALUE"}}}
	for _, env := range [][]string{nil, {"REQUIRED_VALUE="}, {"REQUIRED_VALUE=present", "REQUIRED_VALUE="}} {
		err := Check(context.Background(), []string{"validate"}, contract, t.TempDir(), env)
		assertPreflightCode(t, err, "environment_missing")
		if !strings.Contains(err.Error(), "REQUIRED_VALUE") {
			t.Fatal("missing safe variable name")
		}
	}
	if err := Check(context.Background(), []string{"validate"}, contract, t.TempDir(), []string{"REQUIRED_VALUE=repaired"}); err != nil {
		t.Fatalf("repaired context rejected: %v", err)
	}
}

func TestPreflightUsesSelectedPathAndWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixtures")
	}
	root := t.TempDir()
	for _, dir := range []string{"broken", "ready"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeExecutable(t, filepath.Join(root, "broken", "runtime"), "#!/bin/sh\nexit 1\n")
	writeExecutable(t, filepath.Join(root, "ready", "runtime"), "#!/bin/sh\n[ \"$SESSION_VALUE\" = expected ] && [ -f marker ]\n")
	if err := os.WriteFile(filepath.Join(root, "marker"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(root, "ready"))
	contract := []models.ValidationPrerequisite{{Command: "validate", Executables: []string{"runtime"}, Probes: [][]string{{"runtime", "--import-module"}}}}
	for _, tc := range []struct {
		path string
		code string
	}{
		{"absent", "executable_missing"},
		{"broken", "probe_failed"},
		{"ready", ""},
	} {
		env := []string{"PATH=" + tc.path, "SESSION_VALUE=expected"}
		err := Check(context.Background(), []string{"validate"}, contract, root, env)
		if tc.code == "" {
			if err != nil {
				t.Fatalf("repaired interpreter failed: %v", err)
			}
			resolved, err := LookPath("runtime", root, env)
			if err != nil || resolved != filepath.Join(root, "ready", "runtime") {
				t.Fatalf("relative snapshot PATH resolved incorrectly: %q %v", resolved, err)
			}
		} else {
			assertPreflightCode(t, err, tc.code)
		}
	}
	if _, err := LookPath("runtime", root, nil); err == nil {
		t.Fatal("missing snapshot PATH used ambient PATH")
	}
	writeExecutable(t, filepath.Join(root, "not-executable"), "content")
	if err := os.Chmod(filepath.Join(root, "not-executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LookPath("./not-executable", root, nil); err == nil {
		t.Fatal("non-executable file accepted")
	}
}

func TestPreflightWindowsLookupWithoutPATHEXT(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows executable extension lookup")
	}
	root := t.TempDir()
	executable := filepath.Join(root, "validation-runtime.exe")
	writeExecutable(t, executable, "fixture executable")
	for _, env := range [][]string{{"PATH=" + root}, {"PATH=" + root, "PATHEXT="}} {
		got, err := LookPath("validation-runtime", root, env)
		if err != nil || got != executable {
			t.Fatalf("default executable extension lookup = %q, %v; want %q", got, err, executable)
		}
	}
	if _, err := LookPath("validation-runtime", root, []string{"PATH=" + root, "PATHEXT=.custom"}); err == nil {
		t.Fatal("configured PATHEXT was ignored")
	}
}

func TestPreflightProbeFailureOutputAndTimeout(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contract := []models.ValidationPrerequisite{{Command: "validate", Probes: [][]string{{binary, "-test.run=^TestPreflightProcess$"}}}}
	err = Check(context.Background(), []string{"validate"}, contract, t.TempDir(), []string{"SESSION_PREFLIGHT_HELPER=noisy"})
	assertPreflightCode(t, err, "probe_failed")
	if strings.Contains(err.Error(), "CANARY") || strings.Contains(err.Error(), binary) {
		t.Fatal("probe diagnostics disclosed child output or executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = Check(ctx, []string{"validate"}, contract, t.TempDir(), []string{"SESSION_PREFLIGHT_HELPER=blocked"})
	assertPreflightCode(t, err, "probe_timeout")
	if time.Since(start) > 3*time.Second {
		t.Fatal("probe ignored deadline")
	}
}

func TestPreflightFingerprintProcessDomain(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestPreflightProcess$")
	cmd.Env = []string{"SESSION_PREFLIGHT_HELPER=fingerprint"}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 || fields[0] == Domain() || fields[1] == Fingerprint([]string{"VALUE=canary"}) {
		t.Fatal("fingerprint or comparison domain reused across processes")
	}
}

func TestPreflightCancellationStopsGrandchild(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	contract := []models.ValidationPrerequisite{{Command: "validate", Probes: [][]string{{binary, "-test.run=^TestPreflightProcess$"}}}}
	result := make(chan error, 1)
	root := t.TempDir()
	go func() {
		result <- Check(ctx, []string{"validate"}, contract, root, []string{"SESSION_PREFLIGHT_HELPER=parent", "SESSION_PREFLIGHT_ADDRESS=" + listener.Addr().String()})
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatalf("grandchild did not become ready: %v", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child, findErr := os.FindProcess(pid); findErr == nil {
			_ = child.Kill()
			_ = child.Release()
		}
	})
	cancel()
	select {
	case err := <-result:
		assertPreflightCode(t, err, "probe_timeout")
	case <-time.After(3 * time.Second):
		t.Fatal("probe cancellation did not return")
	}
	// A surviving grandchild answers this release byte. A killed grandchild
	// closes the socket, giving direct termination evidence without polling.
	_, _ = connection.Write([]byte{1})
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("grandchild survived probe cancellation")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("grandchild connection stayed open after probe cancellation")
	}
}

func TestPreflightInvalidContractDoesNotExecute(t *testing.T) {
	err := Check(context.Background(), []string{"changed"}, []models.ValidationPrerequisite{{Command: "old", Env: []string{"VALUE"}}}, t.TempDir(), []string{"VALUE=present"})
	assertPreflightCode(t, err, "invalid_contract")
	if err := Check(context.Background(), []string{"legacy"}, nil, t.TempDir(), nil); err != nil {
		t.Fatalf("legacy task rejected: %v", err)
	}
	unsafe := &Error{Code: "CANARY-SECRET", Variable: "CANARY\nVALUE"}
	if strings.Contains(unsafe.Error(), "CANARY") {
		t.Fatal("untrusted diagnostic fields disclosed")
	}
}

func TestPreflightProcess(t *testing.T) {
	switch os.Getenv("SESSION_PREFLIGHT_HELPER") {
	case "noisy":
		fmt.Fprintln(os.Stdout, "CANARY-STDOUT")
		fmt.Fprintln(os.Stderr, "CANARY-STDERR")
		os.Exit(7)
	case "blocked":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			os.Exit(16)
		}
		defer listener.Close()
		// No client connects: only the preflight deadline can stop this probe.
		_, _ = listener.Accept()
		os.Exit(17)
	case "parent":
		binary, err := os.Executable()
		if err != nil {
			os.Exit(10)
		}
		cmd := exec.Command(binary, "-test.run=^TestPreflightProcess$")
		cmd.Env = []string{"SESSION_PREFLIGHT_HELPER=grandchild", "SESSION_PREFLIGHT_ADDRESS=" + os.Getenv("SESSION_PREFLIGHT_ADDRESS")}
		if err := cmd.Run(); err != nil {
			os.Exit(11)
		}
		os.Exit(0)
	case "grandchild":
		connection, err := net.DialTimeout("tcp", os.Getenv("SESSION_PREFLIGHT_ADDRESS"), 5*time.Second)
		if err != nil {
			os.Exit(12)
		}
		defer connection.Close()
		if err := connection.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			os.Exit(13)
		}
		if _, err := fmt.Fprintln(connection, os.Getpid()); err != nil {
			os.Exit(14)
		}
		var release [1]byte
		if _, err := connection.Read(release[:]); err != nil {
			os.Exit(15)
		}
		_, _ = connection.Write([]byte{1})
		os.Exit(0)
	case "fingerprint":
		fmt.Println(Domain(), Fingerprint([]string{"VALUE=canary"}))
		os.Exit(0)
	}
}

func assertPreflightCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *Error
	if !errors.Is(err, ErrPreflight) || !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("want safe preflight code %s, got %v", code, err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}
