package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/jsonout"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// checkSubmissionEnvelope is the --json envelope of check-submission.
type checkSubmissionEnvelope struct {
	OK     bool `json:"ok"`
	Result struct {
		TaskID          string `json:"task_id"`
		AgentID         string `json:"agent_id"`
		CandidateCommit string `json:"candidate_commit"`
		IntegrationHead string `json:"integration_head"`
		Verdict         string `json:"verdict"`
		Checks          []struct {
			Name   string `json:"name"`
			Stage  string `json:"stage"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		} `json:"checks"`
		Refusal *struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"refusal"`
	} `json:"result"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// checkSubmissionRun runs check-submission as an operator naming the doer and
// returns its stdout, its exit status and the parsed envelope when --json.
func checkSubmissionRun(t *testing.T, root string, args ...string) (string, int, checkSubmissionEnvelope) {
	t.Helper()
	return checkSubmissionRunAs(t, root, "", args...)
}

// checkSubmissionRunAs runs it inside an agent session when sessionID is set.
func checkSubmissionRunAs(t *testing.T, root, sessionID string, args ...string) (string, int, checkSubmissionEnvelope) {
	t.Helper()
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	t.Setenv(brand.EnvName("AGENT_ID"), sessionID)
	out, err := executeRootCommandCapture(t, root, append([]string{"check-submission"}, args...)...)
	code := 0
	var exit *toolResultExit
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.code
	default:
		code = lizaerrors.ExitError
	}
	var envelope checkSubmissionEnvelope
	if strings.Contains(strings.Join(args, " "), "--json") {
		if jsonErr := json.Unmarshal([]byte(out), &envelope); jsonErr != nil {
			t.Fatalf("stdout is not one JSON envelope: %v\n%s", jsonErr, out)
		}
	}
	return out, code, envelope
}

func TestCheckSubmissionCLIExitsPassRefusedAndIncomplete(t *testing.T) {
	const sentinel = "check-submission-env-sentinel-7731"
	t.Setenv("CHECK_SUBMISSION_SENTINEL", sentinel)

	t.Run("pass exits 0 with the verdict fields", func(t *testing.T) {
		scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
		before, err := os.ReadFile(paths.New(scenario.Root).StatePath())
		if err != nil {
			t.Fatal(err)
		}
		out, code, envelope := checkSubmissionRun(t, scenario.Root, scenario.TaskID, "--agent-id", scenario.AgentID, "--json")
		if code != 0 || !envelope.OK || envelope.Result.Verdict != "pass" {
			t.Fatalf("exit %d, envelope %+v, want exit 0 and a pass\n%s", code, envelope, out)
		}
		got := envelope.Result
		if got.TaskID != scenario.TaskID || got.AgentID != scenario.AgentID || got.CandidateCommit != scenario.Commit || len(got.IntegrationHead) != 40 || got.Refusal != nil {
			t.Fatalf("result = %+v, want the task, agent, candidate and integration SHAs and no refusal", got)
		}
		names := make([]string, len(got.Checks))
		for i, check := range got.Checks {
			names[i] = check.Name
			if check.Stage == "" || (check.Status != "passed" && check.Status != "not_applicable") {
				t.Fatalf("check %+v on a pass", check)
			}
		}
		if names[0] != "submit_hold" || names[len(names)-1] != "publication_guards" || len(names) < 20 {
			t.Fatalf("checks = %v, want every name in gate order", names)
		}
		if after, _ := os.ReadFile(paths.New(scenario.Root).StatePath()); !bytes.Equal(before, after) {
			t.Fatal("check-submission changed the state file")
		}
		if strings.Contains(out, sentinel) {
			t.Fatal("the output carries an environment value")
		}
	})

	// A refusal after the gate's preparation marker (entries 13-19) reads with
	// the local run's own lifecycle, the gate's reason otherwise.
	t.Run("post-marker refusal exits 5 with the local lifecycle", func(t *testing.T) {
		scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "echo failing-assertion; exit 3\n", 30)
		out, code, envelope := checkSubmissionRun(t, scenario.Root, scenario.TaskID, "--agent-id", scenario.AgentID, "--json")
		if code != exitCheckSubmissionRefused || !envelope.OK || envelope.Result.Verdict != "refused" {
			t.Fatalf("exit %d, envelope %+v, want exit 5 and a refusal\n%s", code, envelope, out)
		}
		refusal := envelope.Result.Refusal
		if refusal == nil || !strings.Contains(refusal.Message, "acceptance.execution") {
			t.Fatalf("refusal = %+v, want the gate's reason", refusal)
		}
		requireRefusalLifecycle(t, refusal.Code, refusal.Message, refusal.Details)
		var refusedAt string
		for _, check := range envelope.Result.Checks {
			if check.Status == "refused" {
				refusedAt = check.Name
			}
		}
		if refusedAt != "canonical_validation" {
			t.Fatalf("refused at %q, want canonical_validation", refusedAt)
		}
		if strings.Contains(out, sentinel) {
			t.Fatal("the output carries an environment value")
		}
	})

	t.Run("pre-marker refusal exits 5 with the gate's envelope", func(t *testing.T) {
		scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
		// The task has no role pair: entry pipeline refuses.
		if err := db.For(paths.New(scenario.Root).StatePath()).Modify(func(state *models.State) error {
			state.FindTask(scenario.TaskID).RolePair = ""
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		out, code, envelope := checkSubmissionRun(t, scenario.Root, scenario.TaskID, "--agent-id", scenario.AgentID, "--json")
		if code != exitCheckSubmissionRefused || !envelope.OK || envelope.Result.Verdict != "refused" {
			t.Fatalf("exit %d, envelope %+v, want exit 5 and a refusal\n%s", code, envelope, out)
		}
		refusal := envelope.Result.Refusal
		if refusal == nil {
			t.Fatalf("no refusal\n%s", out)
		}
		requireRefusalLifecycle(t, refusal.Code, refusal.Message, refusal.Details)
		for _, check := range envelope.Result.Checks {
			if check.Status == "refused" && check.Name != "pipeline" {
				t.Fatalf("refused at %q, want pipeline", check.Name)
			}
		}
	})

	t.Run("incomplete exits 6 and names why", func(t *testing.T) {
		scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
		if err := os.WriteFile(filepath.Join(scenario.Root, "integration-new.txt"), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, scenario.Root, "add", "integration-new.txt")
		testhelpers.MustGit(t, scenario.Root, "commit", "-m", "Advance integration")
		_, code, envelope := checkSubmissionRun(t, scenario.Root, scenario.TaskID, "--agent-id", scenario.AgentID, "--json")
		if code != exitCheckSubmissionIncomplete || envelope.Result.Verdict != "incomplete" {
			t.Fatalf("exit %d, verdict %s, want exit 6 and incomplete", code, envelope.Result.Verdict)
		}
		for _, check := range envelope.Result.Checks {
			if check.Name == "rebase" && (check.Status != "not_run" || check.Reason != "rebase_pending") {
				t.Fatalf("rebase = %+v, want not_run rebase_pending", check)
			}
		}
	})
}

// requireRefusalLifecycle asserts the rendered refusal reads as a correctable
// input error with no effect: what the local run reports after the gate's marker
// and the gate itself reports before it.
func requireRefusalLifecycle(t *testing.T, code, message string, details map[string]any) {
	t.Helper()
	if code != "validation" || details["outcome"] != "INVALID_INPUT" || details["safe_action"] != "correct_input" || details["effects"] != "none" {
		t.Fatalf("refusal code %q, outcome %v, safe_action %v, effects %v, want validation INVALID_INPUT correct_input none",
			code, details["outcome"], details["safe_action"], details["effects"])
	}
	if !strings.Contains(message, "INVALID_INPUT (safe_action=correct_input)") {
		t.Fatalf("refusal message %q does not carry the INVALID_INPUT prefix", message)
	}
	for _, key := range []string{"request_id", "changed", "diagnostics", "completed_transition_id"} {
		if _, present := details[key]; present {
			t.Fatalf("refusal details carry %s", key)
		}
	}
}

func TestCheckSubmissionCLIDoerSessionPassesWithItsOwnAuthority(t *testing.T) {
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
	out, code, envelope := checkSubmissionRunAs(t, scenario.Root, scenario.AgentID, scenario.TaskID, "--json")
	if code != 0 || envelope.Result.Verdict != "pass" || envelope.Result.AgentID != scenario.AgentID {
		t.Fatalf("exit %d, envelope %+v\n%s", code, envelope, out)
	}
}

func TestCheckSubmissionCLIHumanOutputIsOneLinePerCheckThenTheVerdict(t *testing.T) {
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
	out, code, _ := checkSubmissionRun(t, scenario.Root, scenario.TaskID, "--agent-id", scenario.AgentID)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if code != 0 || len(lines) < 21 || lines[len(lines)-1] != "verdict: pass" {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.HasPrefix(lines[0], "submit_hold") || !strings.Contains(lines[len(lines)-2], "publication_guards") {
		t.Fatalf("first and last check lines = %q, %q", lines[0], lines[len(lines)-2])
	}
}

func TestCheckSubmissionCLIUnknownTaskExits3(t *testing.T) {
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
	out, code, envelope := checkSubmissionRun(t, scenario.Root, "no-such-task", "--agent-id", scenario.AgentID, "--json")
	if code != lizaerrors.ExitNotFound || envelope.OK || envelope.Error == nil || envelope.Error.Code == "" {
		t.Fatalf("exit %d, envelope %+v, want exit 3 and ok false with an error\n%s", code, envelope, out)
	}
}

func TestCheckSubmissionCLIRefusesAReviewerAgent(t *testing.T) {
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 0\n", 30)
	before, err := os.ReadFile(paths.New(scenario.Root).StatePath())
	if err != nil {
		t.Fatal(err)
	}
	out, code, envelope := checkSubmissionRunAs(t, scenario.Root, "code-reviewer-1", scenario.TaskID, "--json")
	wantCode, _ := jsonout.ClassifyError(&lizaerrors.PermissionError{})
	if code != lizaerrors.ExitError || envelope.OK || envelope.Error == nil || envelope.Error.Code != wantCode || !strings.Contains(envelope.Error.Message, "doer") {
		t.Fatalf("exit %d, envelope %+v, want exit 1 and the permission refusal %q\n%s", code, envelope, wantCode, out)
	}
	if after, _ := os.ReadFile(paths.New(scenario.Root).StatePath()); !bytes.Equal(before, after) {
		t.Fatal("a refused reviewer changed the state file")
	}
}
