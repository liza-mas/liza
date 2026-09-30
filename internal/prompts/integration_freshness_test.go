package prompts

import (
	"strings"
	"testing"
)

// The coder is told to catch up with integration before work bound to the exact
// HEAD, so a claim that integration overtakes does not redo that work (D95).
func TestImplementationPhaseIntegrationFreshness(t *testing.T) {
	for _, tc := range []struct {
		role string
		want bool
	}{
		{"coder", true},
		{"code-planner", false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			data := RoleContextData{Role: tc.role, Worktree: "/wt/task-1", IntegrationBranch: "trunk"}
			output, err := BuildRoleContext(tc.role, []string{"implementation-phase"}, &data)
			if err != nil {
				t.Fatal(err)
			}
			for _, instruction := range []string{
				"git -C /wt/task-1 log --oneline -1 HEAD..trunk",
				"git -C /wt/task-1 rebase trunk",
			} {
				if got := strings.Contains(output, instruction); got != tc.want {
					t.Errorf("%s prompt contains %q = %v, want %v:\n%s", tc.role, instruction, got, tc.want, output)
				}
			}
		})
	}
}
