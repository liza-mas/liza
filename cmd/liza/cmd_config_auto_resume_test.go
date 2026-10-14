package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func setupAutoResumeCLI(t *testing.T) (string, string) {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	return setupMutationTestProject(t, func(state *models.State) {
		state.Config.Mode = models.SystemModePaused
		state.Sprint.Status = models.SprintStatusCheckpoint
	})
}

func TestConfigAutoResumeCLI(t *testing.T) {
	root, path := setupAutoResumeCLI(t)
	for _, step := range []struct{ value, outcome string }{{"true", "set"}, {"true", "unchanged"}, {"false", "set"}, {"false", "unchanged"}} {
		stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.AutoResumeConfigKey, step.value, "--json")
		if err != nil {
			t.Fatal(err)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != true {
			t.Fatal(stdout)
		}
		result := envelope["result"].(map[string]any)
		if result["key"] != ops.AutoResumeConfigKey || result["value"] != (step.value == "true") || result["outcome"] != step.outcome {
			t.Fatal(stdout)
		}
		state := readState(t, path)
		if state.Config.Mode != models.SystemModePaused || state.Sprint.Status != models.SprintStatusCheckpoint {
			t.Fatal("CLI resumed execution")
		}
		get, err := executeRootCommandCapture(t, root, "config", "get", ops.AutoResumeConfigKey, "--json")
		if err != nil {
			t.Fatal(err)
		}
		if parseEnvelope(t, get)["result"] != (step.value == "true") {
			t.Fatalf("get disagrees with set: %s", get)
		}
	}
}

func TestConfigAutoResumeRejectsInvalidOrIdentifiedWrites(t *testing.T) {
	for _, value := range []string{"TRUE", "False", "1", "0", "yes", "", " true", "false "} {
		t.Run(value, func(t *testing.T) {
			root, path := setupAutoResumeCLI(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.AutoResumeConfigKey, value, "--json")
			if err == nil {
				t.Fatal("invalid boolean accepted")
			}
			assertJSONError(t, stdout, "validation", "exactly true or false")
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("invalid value wrote state")
			}
		})
	}
	for _, identity := range []string{"flag", "environment"} {
		t.Run(identity, func(t *testing.T) {
			root, path := setupAutoResumeCLI(t)
			before, _ := os.ReadFile(path)
			args := []string{"config", "set", ops.AutoResumeConfigKey, "true", "--json"}
			if identity == "flag" {
				args = append(args, "--agent-id", "orchestrator-1")
			} else {
				t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
			}
			stdout, err := executeRootCommandCapture(t, root, args...)
			if err == nil {
				t.Fatal("identified agent wrote operator policy")
			}
			assertJSONError(t, stdout, "validation", "operator-only")
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("identified write changed state")
			}
		})
	}
	for _, flag := range []string{"--replace", "--reason=ignored"} {
		t.Run(flag, func(t *testing.T) {
			root, _ := setupAutoResumeCLI(t)
			stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.AutoResumeConfigKey, "true", flag, "--json")
			if err == nil {
				t.Fatal("irrelevant write flag accepted")
			}
			assertJSONError(t, stdout, "validation", "does not use")
		})
	}
}

func TestConfigAutoResumeAuditAndTextOutput(t *testing.T) {
	root, _ := setupAutoResumeCLI(t)
	var audit bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&audit)
	t.Cleanup(func() { log.SetOutput(previous) })
	stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.AutoResumeConfigKey, "true", "--json")
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("config_set key=config.auto_resume outcome=set actor=%q project=%q previous=false value=true", "operator", root)
	if !strings.Contains(audit.String(), want) {
		t.Fatalf("audit record missing or misshapen:\n got: %s\nwant: %s", audit.String(), want)
	}
	if strings.Contains(stdout, "config_set") {
		t.Fatal("audit leaked into JSON stdout")
	}
	resetRootCmdForTest(t)
	var text bytes.Buffer
	rootCmd.SetOut(&text)
	rootCmd.SetArgs([]string{"-C", root, "config", "set", ops.AutoResumeConfigKey, "true"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "config.auto_resume: true (unchanged)") {
		t.Fatal(text.String())
	}
}

func TestConfigAutoResumeStructuredFailuresAndCompletion(t *testing.T) {
	root, path := setupAutoResumeCLI(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, root, "config", "set", ops.AutoResumeConfigKey, "true", "--json")
	if err == nil || parseEnvelope(t, stdout)["ok"] != false {
		t.Fatal("missing state produced no structured failure", stdout)
	}
	for _, command := range []string{"get", "set"} {
		output := executeShellComplete(t, "config", command, "config.auto")
		if !completionOutputContains(output, ops.AutoResumeConfigKey) {
			t.Fatal("missing key completion", output)
		}
	}
}
