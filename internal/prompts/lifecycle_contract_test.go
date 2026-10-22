package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestLifecycleResultGuidance_AllRoles(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	roleNames := append(resolver.DoerRoleNames(), resolver.ReviewerRoleNames()...)
	roleNames = append(roleNames, "orchestrator", "custom-role")
	for _, role := range roleNames {
		t.Run(role, func(t *testing.T) {
			base, err := BuildBasePrompt(BasePromptConfig{
				Role: role, AgentID: role + "-1", TaskID: "task-1",
				ProjectRoot: "/repo", StatePath: "/repo/.acme-run/state.yaml",
			})
			if err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(base, "=== LIFECYCLE RESULTS ==="); count != 1 {
				t.Fatalf("got %d lifecycle guidance blocks, want exactly one", count)
			}
			for _, required := range []string{
				"result.outcome", "result.safe_action", "including when ok=false",
				"result.task_status", "result.transition_id", "completed_transition_id",
				"- continue:", "- stop:", "- requery:", "- retry:", "- correct_input:",
				"Wait for each lifecycle command to complete before dependent commands",
				"Preserve the original request-id + expected-transition pair",
				"Do not blindly refresh the transition token and retry",
				"never launch parallel retries",
				"capture the current immutable SHA for a fresh submission",
				"acme-cli get <task-id> --json",
			} {
				if !strings.Contains(base, required) {
					t.Errorf("missing lifecycle rule %q", required)
				}
			}
			for _, forbidden := range []string{"liza", "Liza", "LIZA", "{{binaryName}}"} {
				if strings.Contains(base, forbidden) {
					t.Errorf("non-default rendering leaks %q", forbidden)
				}
			}
		})
	}
}

// A structured retry may be state-lock, admission-slot or runtime-input
// contention: its schedule ends in stop, never in a recovery mutation that
// would presume a cause (D-189).
func TestLifecycleRetryScheduleEndsInStop(t *testing.T) {
	for role, roleType := range map[string]string{"coder": "doer", "code-reviewer": "reviewer"} {
		t.Run(roleType, func(t *testing.T) {
			base, err := BuildBasePrompt(BasePromptConfig{
				Role: role, AgentID: role + "-1", TaskID: "task-1",
				ProjectRoot: "/repo", StatePath: "/repo/.liza/state.yaml",
			})
			if err != nil {
				t.Fatal(err)
			}
			recovery, err := BuildRoleContext(role, []string{"cli-failure-recovery"}, &RoleContextData{
				Role: role, RoleType: roleType, AgentID: role + "-1", TaskID: "task-1", ReviewCommit: "abc",
			})
			if err != nil {
				t.Fatal(err)
			}
			output := base + recovery
			var retry string
			for _, line := range strings.Split(output, "\n") {
				if strings.HasPrefix(line, "- retry:") {
					retry = line
				}
			}
			for _, required := range []string{
				"not a CLI failure", "after about 30 s, 1, 2 and 4 min", "never launch parallel retries",
				"If the fourth retry still returns retry, stop and report",
				"do not infer a cause, add --state-lock-timeout or use CLI FAILURE RECOVERY",
			} {
				if !strings.Contains(retry, required) {
					t.Errorf("retry rule lacks %q:\n%s", required, retry)
				}
			}
			for _, required := range []string{
				"Use this fallback only when no lifecycle result is available",
				"Preserve the original request-id + expected-transition pair",
			} {
				if !strings.Contains(output, required) {
					t.Errorf("missing safeguard %q", required)
				}
			}
		})
	}
}

func TestLifecycleSubmissionEmitters_CaptureImmutableCommit(t *testing.T) {
	for _, section := range []string{
		"architect-tools", "code-planner-tools", "coder-tools", "epic-planner-tools",
		"integration-analyst-tools", "us-writer-tools", "integration-fix", "submission-phase",
	} {
		t.Run(section, func(t *testing.T) {
			output, err := BuildRoleContext("coder", []string{section}, &RoleContextData{
				Role: "coder", RoleType: "doer", AgentID: "coder-1", TaskID: "task-1",
				Worktree: "/repo/.worktrees/task-1", IntegrationFix: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := brand.RuntimeValues().BinaryName + " submit-for-review task-1 <submission-sha> --agent-id coder-1 --json"
			if !strings.Contains(output, want) {
				t.Errorf("missing immutable submission command %q", want)
			}
			for _, required := range []string{"git -C /repo/.worktrees/task-1 rev-parse HEAD", "once", "retry"} {
				if !strings.Contains(output, required) {
					t.Errorf("missing captured-commit rule %q", required)
				}
			}
			for _, forbidden := range []string{"submit-for-review task-1 HEAD", "status changed to CODE_TO_REVIEW"} {
				if strings.Contains(output, forbidden) {
					t.Errorf("unsafe submission instruction %q", forbidden)
				}
			}
			if section == "submission-phase" &&
				!strings.Contains(output, "Only after COMPLETED or ALREADY_COMPLETED with safe_action=continue") {
				t.Error("await must be conditional on the completed submission result")
			}
		})
	}
}

func TestLifecycleRoleInstructions_DoNotOverrideSafeActions(t *testing.T) {
	for _, role := range []string{"code-planner", "epic-planner", "us-writer", "architect"} {
		t.Run(role, func(t *testing.T) {
			output, err := BuildRoleContext(role, []string{"implementation-phase", "cli-failure-recovery"}, &RoleContextData{
				Role: role, RoleType: "doer", AgentID: role + "-1", TaskID: "task-1",
				Worktree: "/repo/.worktrees/task-1",
			})
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{"using HEAD", "submit with HEAD"} {
				if strings.Contains(output, forbidden) {
					t.Errorf("role instructions contradict immutable identity: %q", forbidden)
				}
			}
			for _, required := range []string{
				"using the captured full SHA",
				"Structured safe_action takes precedence",
				"no lifecycle result is available",
				"fresh task query confirms your authority",
			} {
				if !strings.Contains(output, required) {
					t.Errorf("missing lifecycle safeguard %q", required)
				}
			}
		})
	}
}
