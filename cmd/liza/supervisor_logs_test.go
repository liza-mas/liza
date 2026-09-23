package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/subprocess"
	"github.com/spf13/cobra"
)

func TestSupervisorLogsAreChildOwnedMaskedAndPersistErrors(t *testing.T) {
	secret := "supervisor-secret-value"
	t.Setenv("OPENAI_API_KEY", secret)
	stdoutPath := filepath.Join(t.TempDir(), "supervisor.stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "supervisor.stderr.log")

	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorStdoutLogFlag, stdoutPath, "")
	cmd.Flags().String(agent.SupervisorStderrLogFlag, stderrPath, "")
	previousLogger := agent.GetLogger()
	logs, err := openSupervisorLogs(cmd)
	if err != nil {
		t.Fatalf("openSupervisorLogs() error = %v", err)
	}

	agent.GetLogger().Info("supervisor lifecycle", "detail", "before "+secret+" after")
	if _, err := logs.stderrWriter.Write([]byte("stderr before " + secret[:12])); err != nil {
		t.Fatalf("write stderr prefix: %v", err)
	}
	if _, err := logs.stderrWriter.Write([]byte(secret[12:] + " after\n")); err != nil {
		t.Fatalf("write stderr suffix: %v", err)
	}
	runErr := errors.New("supervisor failed with " + secret)
	if gotErr := finishSupervisorLogs(logs, runErr); !errors.Is(gotErr, runErr) {
		t.Fatalf("finishSupervisorLogs() error = %v, want wrapped run error", gotErr)
	}
	if agent.GetLogger() != previousLogger {
		t.Fatal("agent logger was not restored after supervisor logs closed")
	}

	assertMaskedSupervisorLog(t, stdoutPath, secret, "supervisor lifecycle", "before *** after")
	assertMaskedSupervisorLog(t, stderrPath, secret, "stderr before *** after", "Error: supervisor failed with ***")
}

func TestOpenSupervisorLogsRequiresBothPaths(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorStdoutLogFlag, filepath.Join(t.TempDir(), "stdout.log"), "")
	cmd.Flags().String(agent.SupervisorStderrLogFlag, "", "")

	_, err := openSupervisorLogs(cmd)
	if err == nil || !strings.Contains(err.Error(), "must be supplied together") {
		t.Fatalf("openSupervisorLogs() error = %v, want paired-path validation", err)
	}
}

func TestWriteSupervisorBootstrapErrorMasksSecret(t *testing.T) {
	secret := "bootstrap-secret-value"
	t.Setenv("OPENAI_API_KEY", secret)
	readyPath := filepath.Join(t.TempDir(), "supervisor.ready")
	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorReadyFileFlag, readyPath, "")

	if err := writeSupervisorBootstrapError(cmd, errors.New("cannot open logs with "+secret)); err != nil {
		t.Fatalf("writeSupervisorBootstrapError() error = %v", err)
	}
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("read bootstrap status: %v", err)
	}
	got := string(data)
	if strings.Contains(got, secret) {
		t.Fatalf("bootstrap status contains unmasked secret: %q", got)
	}
	if want := "error: cannot open logs with ***\n"; got != want {
		t.Fatalf("bootstrap status = %q, want %q", got, want)
	}
}

func TestWriteSupervisorBootstrapStatusDoesNotOverwriteExistingFile(t *testing.T) {
	readyPath := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(readyPath, []byte("keep"), 0600); err != nil {
		t.Fatalf("write existing file: %v", err)
	}
	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorReadyFileFlag, readyPath, "")

	if err := writeSupervisorBootstrapReady(cmd); err == nil {
		t.Fatal("writeSupervisorBootstrapReady() error = nil, want exclusive-create failure")
	}
	data, err := os.ReadFile(readyPath)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if got := string(data); got != "keep" {
		t.Fatalf("existing file = %q, want unchanged content", got)
	}
}

func TestFinishSupervisorRunPersistsMaskedPanic(t *testing.T) {
	secret := "panic-secret-value"
	t.Setenv("OPENAI_API_KEY", secret)
	stdoutPath := filepath.Join(t.TempDir(), "supervisor.stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "supervisor.stderr.log")
	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorStdoutLogFlag, stdoutPath, "")
	cmd.Flags().String(agent.SupervisorStderrLogFlag, stderrPath, "")
	logs, err := openSupervisorLogs(cmd)
	if err != nil {
		t.Fatalf("openSupervisorLogs() error = %v", err)
	}

	err = finishSupervisorRun(logs, nil, "panic with "+secret)
	if err == nil || !strings.Contains(err.Error(), "supervisor panic: panic with "+secret) {
		t.Fatalf("finishSupervisorRun() error = %v, want recovered panic", err)
	}
	assertMaskedSupervisorLog(t, stderrPath, secret, "supervisor panic: panic with ***")
}

func TestDetachedChildOwnsMaskedLogsAfterSpawnerExit(t *testing.T) {
	secret := "detached-supervisor-secret"
	stdoutPath := filepath.Join(t.TempDir(), "supervisor.stdout.log")
	stderrPath := filepath.Join(t.TempDir(), "supervisor.stderr.log")
	releasePath := filepath.Join(t.TempDir(), "release-child")

	spawner := exec.Command(os.Args[0], "-test.run=^TestSupervisorLogSpawnerHelper$")
	spawner.Env = append(os.Environ(),
		"LIZA_TEST_LOG_SPAWNER=1",
		"LIZA_TEST_LOG_STDOUT="+stdoutPath,
		"LIZA_TEST_LOG_STDERR="+stderrPath,
		"LIZA_TEST_LOG_RELEASE="+releasePath,
		"OPENAI_API_KEY="+secret,
	)
	if output, err := spawner.CombinedOutput(); err != nil {
		t.Fatalf("supervisor log spawner failed: %v\n%s", err, output)
	}
	if err := os.WriteFile(releasePath, []byte("spawner-exited"), 0600); err != nil {
		t.Fatalf("release detached child: %v", err)
	}

	waitForFileContains(t, stdoutPath, "detached child lifecycle")
	waitForFileContains(t, stderrPath, "stderr before *** after")
	assertMaskedSupervisorLog(t, stdoutPath, secret, "detached child lifecycle", "before *** after")
	assertMaskedSupervisorLog(t, stderrPath, secret, "stderr before *** after")
}

func TestSupervisorLogSpawnerHelper(t *testing.T) {
	if os.Getenv("LIZA_TEST_LOG_SPAWNER") != "1" {
		return
	}

	child := exec.Command(os.Args[0], "-test.run=^TestSupervisorLogChildHelper$")
	child.Env = os.Environ()
	subprocess.SetDetachedProcessGroup(child)
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	child.Stdin = devNull
	child.Stdout = devNull
	child.Stderr = devNull
	if err := child.Start(); err != nil {
		devNull.Close()
		t.Fatalf("start detached log child: %v", err)
	}
	if err := devNull.Close(); err != nil {
		t.Fatalf("close spawner devnull: %v", err)
	}
}

func TestSupervisorLogChildHelper(t *testing.T) {
	if os.Getenv("LIZA_TEST_LOG_SPAWNER") != "1" {
		return
	}

	waitForFileContains(t, os.Getenv("LIZA_TEST_LOG_RELEASE"), "spawner-exited")
	cmd := &cobra.Command{}
	cmd.Flags().String(agent.SupervisorStdoutLogFlag, os.Getenv("LIZA_TEST_LOG_STDOUT"), "")
	cmd.Flags().String(agent.SupervisorStderrLogFlag, os.Getenv("LIZA_TEST_LOG_STDERR"), "")
	logs, err := openSupervisorLogs(cmd)
	if err != nil {
		t.Fatalf("openSupervisorLogs() error = %v", err)
	}

	secret := os.Getenv("OPENAI_API_KEY")
	agent.GetLogger().Info("detached child lifecycle", "detail", "before "+secret+" after")
	if _, err := logs.stderrWriter.Write([]byte("stderr before " + secret[:10])); err != nil {
		t.Fatalf("write detached stderr prefix: %v", err)
	}
	if _, err := logs.stderrWriter.Write([]byte(secret[10:] + " after\n")); err != nil {
		t.Fatalf("write detached stderr suffix: %v", err)
	}
	if err := finishSupervisorLogs(logs, nil); err != nil {
		t.Fatalf("finishSupervisorLogs() error = %v", err)
	}
}

func waitForFileContains(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not contain %q before timeout; last content %q", path, want, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertMaskedSupervisorLog(t *testing.T, path, secret string, wants ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got := string(data)
	if strings.Contains(got, secret) {
		t.Fatalf("log %s contains unmasked secret: %q", path, got)
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("log %s = %q, want %q", path, got, want)
		}
	}
}
