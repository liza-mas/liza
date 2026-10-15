package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-188: doers run submit-for-review with --json, which discards the logger.
// Progress must still reach stderr while stdout carries one JSON result.
func TestSubmitForReviewJSONKeepsStdoutAndReportsProgressOnStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("canonical command uses a POSIX shell script")
	}
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "sleep 0.3\n", 30)

	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = stderrW
	var stderr bytes.Buffer
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		_, _ = io.Copy(&stderr, stderrR)
	}()
	stdout, cmdErr := executeRootCommandCapture(t, scenario.Root,
		"submit-for-review", scenario.TaskID, scenario.Commit, "--agent-id", scenario.AgentID, "--json")
	os.Stderr = oldStderr
	_ = stderrW.Close()
	<-copied
	_ = stderrR.Close()

	if cmdErr != nil {
		t.Fatalf("submit-for-review --json failed: %v\nstdout: %s\nstderr: %s", cmdErr, stdout, stderr.String())
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Outcome string `json:"outcome"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(strings.NewReader(stdout))
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("stdout is not a JSON result: %v\n%s", err, stdout)
	}
	if decoder.More() {
		t.Fatalf("stdout holds more than one JSON document:\n%s", stdout)
	}
	if !envelope.OK || envelope.Result.Outcome != models.LifecycleCompleted {
		t.Fatalf("result = %+v, want ok COMPLETED\n%s", envelope, stdout)
	}
	if !strings.Contains(stderr.String(), "running canonical validation, batch budget 30s") {
		t.Fatalf("stderr has no batch progress under --json:\n%s", stderr.String())
	}
	if strings.Contains(stdout, "running canonical validation") {
		t.Fatalf("progress leaked into stdout:\n%s", stdout)
	}
}
