package ops

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const deniedHostName = "HOST_ONLY_KEY"

func setAgentEnvDenyForTest(t *testing.T, bb *db.Blackboard, names ...string) {
	t.Helper()
	if err := bb.Modify(func(state *models.State) error {
		state.Config.AgentEnvDeny = names
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A gate command loses an ambient denied name. A runtime input that delivers
// a denied name still reaches its declared command, with the broker value
// only: the deny list controls host inheritance, not explicit delivery.
func TestRuntimeInputGateWithholdsAgentEnvDenyButKeepsBrokerDelivery(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	setAgentEnvDenyForTest(t, f.bb, deniedHostName, "MEMBER_CREDENTIAL")
	grant, err := f.gate().acquire(f.state(t).FindTask(f.taskID))
	if err != nil {
		t.Fatal(err)
	}
	env := grant.commandEnvironment([]string{"PATH=/bin", deniedHostName + "=ambient", "MEMBER_CREDENTIAL=ambient"}, runtimeInputCommand)
	var credentials []string
	for _, entry := range env {
		if strings.HasPrefix(entry, deniedHostName+"=") {
			t.Fatalf("gate env kept the ambient %s", deniedHostName)
		}
		if value, ok := strings.CutPrefix(entry, "MEMBER_CREDENTIAL="); ok {
			credentials = append(credentials, value)
		}
	}
	if !slices.Equal(credentials, []string{runtimeInputSecret}) {
		t.Fatalf("MEMBER_CREDENTIAL: %d values, broker value only=%v; want exactly the broker value", len(credentials), slices.Equal(credentials, []string{runtimeInputSecret}))
	}
}

func TestRunLiveWithholdsAgentEnvDeny(t *testing.T) {
	f := setupRuntimeInputScenario(t, 0)
	setAgentEnvDenyForTest(t, f.bb, deniedHostName)
	t.Setenv(deniedHostName, "ambient-leak")
	wt := git.New(f.root).GetWorktreePath(f.taskID)
	result, err := RunLive(f.root, RunLiveInput{TaskID: f.taskID, Dir: wt,
		Argv: []string{"sh", "-c", `printf '%s' "${` + deniedHostName + `:-absent}"`}})
	if err != nil {
		t.Fatalf("RunLive: %v", err)
	}
	if result.Output != "absent" {
		t.Fatalf("run-live output = %q, want the denied name withheld", result.Output)
	}
}

// A root env file mentioning a denied name never reaches a worktree.
func TestProvisionWorktreeEnvFilesRefusesAgentEnvDenyMention(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	commitEnvIgnoreForWorktreeTest(t, tmpDir)
	statePath, _ := testhelpers.SetupLizaDir(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.AgentEnvDeny = []string{deniedHostName}
	testhelpers.WriteInitialState(t, statePath, state)
	writeRootFileForWorktreeTest(t, tmpDir, ".env", "OTHER=kept\n"+deniedHostName+"=host-secret\n")
	if _, err := git.New(tmpDir).CreateWorktree("task-1", "integration"); err != nil {
		t.Fatalf("CreateWorktree() setup error: %v", err)
	}
	worktreeDir := filepath.Join(tmpDir, paths.WorktreesDirName, "task-1")

	warnings := ProvisionWorktreeEnvFiles(tmpDir, worktreeDir)
	if len(warnings) != 1 || !strings.Contains(warnings[0], deniedHostName) || strings.Contains(warnings[0], "host-secret") {
		t.Fatalf("warnings = %v, want one warning naming the denied variable without its value", warnings)
	}
	assertWorktreeFileMissing(t, worktreeDir, ".env")
}

// The supervisor and deferred-runner boundary: denied names leave this
// process's environment, so every child it starts inherits a clean one.
func TestWithholdDeniedEnvironmentUnsetsConfiguredNames(t *testing.T) {
	root := t.TempDir()
	t.Setenv(deniedHostName, "host-value")
	t.Setenv("UNRELATED_KEY", "kept")
	if removed, err := WithholdDeniedEnvironment(root); err != nil || removed != nil {
		t.Fatalf("without state: removed %v, err %v; want a no-op", removed, err)
	}
	if os.Getenv(deniedHostName) != "host-value" {
		t.Fatal("no-op withhold changed the environment")
	}

	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AgentEnvDeny = []string{deniedHostName}
	testhelpers.WriteInitialState(t, statePath, state)
	removed, err := WithholdDeniedEnvironment(root)
	if err != nil || !slices.Equal(removed, []string{deniedHostName}) {
		t.Fatalf("removed %v, err %v; want the denied name", removed, err)
	}
	if _, ok := os.LookupEnv(deniedHostName); ok || os.Getenv("UNRELATED_KEY") != "kept" {
		t.Fatalf("env after withhold: denied present=%v, unrelated=%q", ok, os.Getenv("UNRELATED_KEY"))
	}

	if err := os.WriteFile(statePath, []byte("not: [valid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WithholdDeniedEnvironment(root); err == nil {
		t.Fatal("unreadable state withheld nothing silently, want fail closed")
	}
}

func TestRunDeferredValidationWithholdsAgentEnvDenyAtEntry(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AgentEnvDeny = []string{deniedHostName}
	testhelpers.WriteInitialState(t, statePath, state)
	t.Setenv(deniedHostName, "host-value")
	_, err := runDeferredValidation(context.Background(), root, RunDeferredValidationOptions{}, deferredValidationHooks{platformSupported: false})
	if err == nil {
		t.Fatal("unsupported platform ran a batch")
	}
	if _, ok := os.LookupEnv(deniedHostName); ok {
		t.Fatal("deferred runner entry kept the denied name")
	}
}

func TestSetAgentEnvDenyFollowsTheConfigRule(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC())
	task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "check", Env: []string{"PREREQUISITE_KEY"}}}
	state.Tasks = append(state.Tasks, task)
	testhelpers.WriteInitialState(t, statePath, state)
	stored := func() []string {
		t.Helper()
		state, err := db.For(statePath).Read()
		if err != nil {
			t.Fatal(err)
		}
		return state.Config.AgentEnvDeny
	}
	set := func(input SetAgentEnvDenyInput) (*ConfigSetResult, error) {
		t.Helper()
		return SetAgentEnvDeny(root, input)
	}

	if result, err := set(SetAgentEnvDenyInput{Names: []string{"OPENAI_API_KEY", deniedHostName}}); err != nil || result.Outcome != "set" {
		t.Fatalf("first set: %+v, %v", result, err)
	}
	if result, err := set(SetAgentEnvDenyInput{Names: []string{"OPENAI_API_KEY", deniedHostName}}); err != nil || result.Outcome != "unchanged" {
		t.Fatalf("repeat: %+v, %v", result, err)
	}
	if _, err := set(SetAgentEnvDenyInput{Names: []string{"OPENAI_API_KEY"}}); err == nil || !strings.Contains(err.Error(), "--replace") {
		t.Fatalf("replacement without --replace: %v", err)
	}
	if _, err := set(SetAgentEnvDenyInput{Names: []string{"PATH"}, Replace: true, Reason: "test"}); err == nil {
		t.Fatal("process variable accepted")
	}
	if _, err := set(SetAgentEnvDenyInput{Names: []string{"PREREQUISITE_KEY"}, Replace: true, Reason: "test"}); err == nil || !strings.Contains(err.Error(), "PREREQUISITE_KEY") {
		t.Fatalf("validation-prerequisite collision: %v", err)
	}
	if got := stored(); !slices.Equal(got, []string{"OPENAI_API_KEY", deniedHostName}) {
		t.Fatalf("stored after refusals = %v", got)
	}
	if result, err := set(SetAgentEnvDenyInput{Names: []string{"OPENAI_API_KEY"}, Replace: true, Reason: "narrow"}); err != nil || result.Outcome != "replaced" {
		t.Fatalf("replace: %+v, %v", result, err)
	}
	if result, err := set(SetAgentEnvDenyInput{Replace: true, Reason: "clear"}); err != nil || result.Outcome != "cleared" || len(stored()) != 0 {
		t.Fatalf("clear: %+v, %v, stored %v", result, err, stored())
	}
}

// The Modify fence refuses a prerequisite that names a denied variable: the
// session could never hold it.
func TestModifyRefusesAPrerequisiteNamingADeniedVariable(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Config.AgentEnvDeny = []string{deniedHostName}
	state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("live", models.TaskStatusReady, time.Now().UTC()))
	testhelpers.WriteInitialState(t, statePath, state)
	err := db.For(statePath).Modify(func(state *models.State) error {
		state.FindTask("live").ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "check", Env: []string{deniedHostName}}}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), deniedHostName) || !strings.Contains(err.Error(), "agent_env_deny") {
		t.Fatalf("Modify err = %v, want the denied prerequisite refused by name", err)
	}
}

// A prepared validation context is bound to the deny list: changing it
// invalidates the preparation like any other launch-relevant config.
func TestValidationPreflightContextChangesWithAgentEnvDeny(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	preflight, err := PrepareValidationPreflight(f.root, "task-1", f.authority.ID, "", f.session(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := preflight.CheckCurrent(f.state(t)); err != nil {
		t.Fatalf("fresh preparation rejected: %v", err)
	}
	setAgentEnvDenyForTest(t, f.bb, "UNRELATED_KEY")
	if err := preflight.CheckCurrent(f.state(t)); err == nil || !strings.Contains(err.Error(), "context_changed") {
		t.Fatalf("CheckCurrent after a deny-list change = %v, want context_changed", err)
	}
}
