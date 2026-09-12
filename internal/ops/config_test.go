package ops

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSetPostWorktreeCmd(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	initial := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, statePath, initial)
	command := "make setup"
	for _, tc := range []struct {
		name    string
		input   SetPostWorktreeCmdInput
		outcome string
		err     string
	}{
		{"set", SetPostWorktreeCmdInput{Command: command}, "set", ""},
		{"repeat", SetPostWorktreeCmdInput{Command: command}, "unchanged", ""},
		{"conflict", SetPostWorktreeCmdInput{Command: "make other"}, "", "already set"},
		{"replace without reason", SetPostWorktreeCmdInput{Command: "make other", Replace: true}, "", "--reason"},
		{"replace", SetPostWorktreeCmdInput{Command: "make other", Replace: true, Reason: "Correct setup"}, "replaced", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := readStateForTest(t, statePath)
			result, err := SetPostWorktreeCmd(root, tc.input)
			after := readStateForTest(t, statePath)
			if tc.err != "" {
				var precondition *PreconditionError
				if !errors.As(err, &precondition) || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error = %v, want precondition containing %q", err, tc.err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("rejected write changed state")
				}
				return
			}
			if err != nil || result.Key != PostWorktreeConfigKey || result.Outcome != tc.outcome {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if after.Config.PostWorktreeCmd == nil || *after.Config.PostWorktreeCmd != tc.input.Command {
				t.Fatal("command was not persisted")
			}
			after.Config.PostWorktreeCmd = before.Config.PostWorktreeCmd
			if !reflect.DeepEqual(before, after) {
				t.Fatal("write changed unrelated state")
			}
		})
	}
}

func TestSetPostWorktreeCmdRejectsMalformedInput(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	for _, command := range []string{"", " ", " setup", "setup ", "setup\nnext", "setup\rnext", "setup\x00", string([]byte{0xff})} {
		if _, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: command}); err == nil {
			t.Fatal("malformed command accepted")
		}
	}
	if readStateForTest(t, statePath).Config.PostWorktreeCmd != nil {
		t.Fatal("invalid input changed config")
	}
}

func TestSetPostWorktreeCmdDoesNotExecute(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	if _, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: "echo unexpected > config-set-executed"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "config-set-executed")); !os.IsNotExist(err) {
		t.Fatalf("setting config executed the command: %v", err)
	}
}

func TestSetPostWorktreeCmdAuthorityCheckedInsideTransaction(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = models.Agent{Generation: "old"}
	testhelpers.WriteInitialState(t, statePath, state)
	bb := db.For(statePath)
	t.Cleanup(setLifecycleMutationTestHook(bb, func() {
		if err := bb.Modify(func(s *models.State) error {
			a := s.Agents["orchestrator-1"]
			a.Generation = "new"
			s.Agents["orchestrator-1"] = a
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}))
	input := SetPostWorktreeCmdInput{Command: "make setup", Authority: &models.AgentAuthority{ID: "orchestrator-1", Generation: "old"}}
	if _, err := SetPostWorktreeCmd(root, input); !IsAgentAuthorityError(err) {
		t.Fatalf("stale authority accepted: %v", err)
	}
	if readStateForTest(t, statePath).Config.PostWorktreeCmd != nil {
		t.Fatal("stale authority wrote config")
	}
	input.Authority.Generation = "new"
	if _, err := SetPostWorktreeCmd(root, input); err != nil {
		t.Fatalf("current authority rejected: %v", err)
	}
}

func TestSetPostWorktreeCmdConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	start := make(chan struct{})
	type outcome struct {
		command string
		err     error
	}
	results := make(chan outcome, 2)
	var writers sync.WaitGroup
	for _, command := range []string{"make first", "make second"} {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			_, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: command})
			results <- outcome{command, err}
		}()
	}
	close(start)
	writers.Wait()
	close(results)
	winner, conflicts := "", 0
	for result := range results {
		if result.err == nil {
			if winner != "" {
				t.Fatal("both writers succeeded")
			}
			winner = result.command
		} else {
			var conflict *PreconditionError
			if !errors.As(result.err, &conflict) || conflict.Details["conflict"] != "existing_value" {
				t.Fatal(result.err)
			}
			conflicts++
		}
	}
	state := readStateForTest(t, statePath)
	if conflicts != 1 || state.Config.PostWorktreeCmd == nil || *state.Config.PostWorktreeCmd != winner {
		t.Fatal("concurrent comparison lost a committed value")
	}
}

func TestSetPostWorktreeCmdAudit(t *testing.T) {
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	for _, tc := range []struct{ command, detection string }{
		{"make setup", "none"},
		{"npm install", "match"},
		{"make custom", "different"},
	} {
		if tc.detection != "none" {
			if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{}`), 0644); err != nil {
				t.Fatal(err)
			}
		}
		output.Reset()
		_, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: tc.command, Replace: true, Reason: "Operator setup"})
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range []string{"config_set key=" + PostWorktreeConfigKey, "actor=\"operator\"", "detector=" + tc.detection, "command_sha256="} {
			if !strings.Contains(output.String(), required) {
				t.Fatalf("audit missing %q: %s", required, output.String())
			}
		}
	}
	t.Setenv("CONFIG_TEST_API_KEY", "fixture-private-value")
	output.Reset()
	_, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: "make fixture-private-value", Replace: true, Reason: "fixture-private-value"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "fixture-private-value") {
		t.Fatal("audit exposed a credential")
	}
}

func TestSetPostWorktreeCmdAfterScaffoldPreparesNextClaim(t *testing.T) {
	const scaffold = "config-scaffold"
	root, statePath := setupMergeTestRepo(t, scaffold, "coder-1")
	advanceApprovedTaskWithFiles(t, root, statePath, scaffold, map[string]string{
		"bootstrap.sh": "#!/bin/sh\nprintf ready > .build-output\n",
		".gitignore":   ".build-output\n",
	})
	if _, err := MergeWorktree(root, scaffold, "coder-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: "sh bootstrap.sh"}); err != nil {
		t.Fatal(err)
	}
	bb := db.For(statePath)
	if err := bb.Modify(func(s *models.State) error {
		registerClaimTaskTestAgents(s)
		task := testhelpers.BuildTaskByStatus("after-scaffold", models.TaskStatusReady, time.Now().UTC())
		task.DependsOn = []string{scaffold}
		s.Tasks = append(s.Tasks, task)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := ClaimTask(root, "after-scaffold", "coder-1")
	if err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, ".worktrees", result.TaskID)
	data, err := os.ReadFile(filepath.Join(worktree, ".build-output"))
	if err != nil || string(data) != "ready" {
		t.Fatalf("claim lacked prepared artifacts: %v", err)
	}
	if err := RunPostWorktreeCmd("sh bootstrap.sh", worktree); err != nil {
		t.Fatal(err)
	}
	if got := testhelpers.MustGit(t, worktree, "status", "--porcelain"); got != "" {
		t.Fatalf("bootstrap violated clean sync: %s", got)
	}

	if _, err := SetPostWorktreeCmd(root, SetPostWorktreeCmdInput{Command: "exit 9", Replace: true, Reason: "Exercise readiness failure"}); err != nil {
		t.Fatal(err)
	}
	if err := bb.Modify(func(s *models.State) error {
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("setup-fails", models.TaskStatusReady, time.Now().UTC()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err = ClaimTask(root, "setup-fails", "coder-2")
	var setupErr *PostWorktreeSetupError
	if !errors.As(err, &setupErr) {
		t.Fatalf("expected fail-closed setup, got %v", err)
	}
	if task := readStateForTest(t, statePath).FindTask("setup-fails"); task.Status != models.TaskStatusReady || task.AssignedTo != nil {
		t.Fatal("failed setup published claim")
	}
}
