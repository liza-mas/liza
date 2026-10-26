package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/roles"
)

// flagCompletion returns, sorted, the values a flag's registered completion
// offers; the completion filter sorts them itself.
func flagCompletion(t *testing.T, cmd *cobra.Command, flag string) []string {
	t.Helper()
	complete, ok := cmd.GetFlagCompletionFunc(flag)
	if !ok {
		t.Fatalf("%s --%s has no registered completion", cmd.Name(), flag)
	}
	values, _ := complete(cmd, nil, "")
	return slices.Sorted(slices.Values(values))
}

func TestA7CompletionValuesKeepAcceptedSpellings(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
		flag string
		want []string
	}{
		{"get --format", getCmd, "format", []string{"json", "table", "value", "yaml"}},
		{"status --format", statusCmd, "format", []string{"dashboard", "json", "yaml"}},
		{"toolchain configure --agent-tools", toolchainConfigureCmd, "agent-tools", []string{"auto", "force", "skip"}},
		{"init --scip-search", initCmd, "scip-search", []string{"go", "python", "typescript"}},
	} {
		if got := flagCompletion(t, tc.cmd, tc.flag); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s completes %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestA7MASLaunchPresetsKeepRoleNames(t *testing.T) {
	want := map[masLaunchPreset][]string{
		masPresetTechnicalSpec: {"orchestrator", "code-planner", "code-plan-reviewer", "coder", "code-reviewer"},
		masPresetFunctionalSpec: {
			"orchestrator", "architect", "architecture-reviewer", "code-planner", "code-plan-reviewer", "coder", "code-reviewer",
		},
		masPresetGeneralObjective: {
			"orchestrator", "epic-planner", "epic-plan-reviewer", "us-writer", "us-reviewer", "architect",
			"architecture-reviewer", "code-planner", "code-plan-reviewer", "coder", "code-reviewer",
		},
	}
	if got := masLaunchPresets(); !reflect.DeepEqual(got, want) {
		t.Errorf("masLaunchPresets() = %v, want %v", got, want)
	}
}

func TestA7ValidateRoleTypeNamesAllowedTypes(t *testing.T) {
	resolver := setupResolver(t)
	err := validateRoleType(resolver, "coder-1", roles.RoleTypeOrchestrator, roles.RoleTypeReviewer)
	if err == nil {
		t.Fatal("a doer must not pass an orchestrator or reviewer gate")
	}
	for _, want := range []string{"command requires role type [orchestrator, reviewer]", `has type "doer"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
	if err := validateRoleType(resolver, "coder-1", roles.RoleTypeReviewer, roles.RoleTypeDoer); err != nil {
		t.Errorf("a doer passes a gate that lists doer: %v", err)
	}
}

func TestA7PairingLaunchVocabularyKeepsSpellings(t *testing.T) {
	if got, want := pairingSkillPrompt(string(roles.RoleTypeDoer), "board.md", true), "$adversarial-pairing doer board.md yolo"; got != want {
		t.Errorf("doer prompt = %q, want %q", got, want)
	}
	if got, want := pairingSkillPrompt(pairingReviewerIDPrefix+"a", "board.md", false), "$adversarial-pairing reviewer-a board.md"; got != want {
		t.Errorf("reviewer prompt = %q, want %q", got, want)
	}
	for spec, want := range map[string][2]string{"reviewer-a=codex": {"a", "codex"}, "a=codex": {"a", "codex"}, "reviewer-codex": {"codex", "codex"}} {
		id, cli, err := parseReviewerLaunchSpec(spec)
		if err != nil || id != want[0] || cli != want[1] {
			t.Errorf("parseReviewerLaunchSpec(%q) = %q, %q, %v; want %q, %q", spec, id, cli, err, want[0], want[1])
		}
	}
	for _, pair := range [][2]string{
		{weztermSplitRight, "--right"}, {weztermSplitBottom, "--bottom"},
		{cmuxSplitRight, "right"}, {cmuxSplitDown, "down"},
		{headRef, "HEAD"}, {stdinFileArg, "-"}, {boolArgTrue, "true"}, {boolArgFalse, "false"},
	} {
		if pair[0] != pair[1] {
			t.Errorf("constant spells %q, want %q", pair[0], pair[1])
		}
	}
}

func TestA7NamedMagicValuesKeepTheirValues(t *testing.T) {
	for name, pair := range map[string][2]int{
		"auto-assign attempts": {autoAssignAgentIDAttempts, 5}, "short object ID": {shortObjectIDWidth, 8},
		"provision instance ID": {provisionInstanceIDWidth, 16}, "task priority": {defaultTaskPriority, 1},
		"pairing board dir mode": {pairingBoardDirMode, 0o755}, "pairing board file mode": {pairingBoardFileMode, 0o644},
		"supervisor log mode": {supervisorLogFileMode, 0o644}, "bootstrap status mode": {supervisorBootstrapFileMode, 0o600},
		"failure exit code": {lizaerrors.ExitError, 1}, "blocked questions": {maxBlockedQuestions, 3},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s = %d, want %d", name, pair[0], pair[1])
		}
	}
	if retargetRejectedDetailKey != "diagnostic_action" || retargetRejectedAction != "retarget_dependency_rejected" || defaultChangedBy != "human" {
		t.Errorf("named spellings changed: %q %q %q", retargetRejectedDetailKey, retargetRejectedAction, defaultChangedBy)
	}
	if contractKeyNone != "none" {
		t.Errorf("contractKeyNone = %q, want %q", contractKeyNone, "none")
	}
	if got, want := shortObjectID("0123456789abcdef"), "01234567"; got != want {
		t.Errorf("shortObjectID truncates to %q, want %q", got, want)
	}
	if got, want := shortObjectID("short"), "short"; got != want {
		t.Errorf("shortObjectID keeps %q, want %q", got, want)
	}
}
