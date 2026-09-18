package prompts

import (
	"strings"
	"testing"
)

// The operator skill's retired recovery advice must not return: supersede-first
// recovery, dependency holds classified as healthy by default, and the
// recover-task/recover-agent conflation. The replacements are pinned too.
func TestBlockedTaskChainsOperatorSkill(t *testing.T) {
	skill := readContractFixture(t, "../../skills/liza-operator/SKILL.md")
	for _, retired := range []string{
		"supersede→redo",
		"is NOT stuck",
		"removes the task worktree and branch even without",
	} {
		if strings.Contains(skill, retired) {
			t.Errorf("operator skill still carries retired advice %q", retired)
		}
	}
	for _, required := range []string{
		"Intentional dependency metering is a",
		"may be metered — verify, do not assume",
		"preserves by default",
		"Governance, not a second review",
		"The role system is not converging",
		"Blocking chain:",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("operator skill missing %q", required)
		}
	}
}
