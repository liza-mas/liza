package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// prepareDevinToolResultPlan puts the host proxy inside acpx's agent process,
// not on its observational stdout. The command is stable for a task so resumed
// sessions keep the same host and artifact namespace.
func prepareDevinToolResultPlan(req LLMAgentRunRequest, plan LaunchPlan, engine string) (LaunchPlan, error) {
	if plan.ToolName != "devin-acp" {
		return plan, nil
	}
	if !filepath.IsAbs(engine) || !filepath.IsAbs(req.ProjectRoot) || strings.TrimSpace(plan.ACPXAgent) == "" {
		return plan, fmt.Errorf("devin tool-result boundary requires absolute engine/project paths and a native ACP command")
	}
	root := filepath.Join(req.ProjectRoot, paths.ProjectDirName(), "tool-results")
	command := codexToolResultShellQuote("env") + " " + codexToolResultShellQuote("DEVIN_PERMISSION_MODE=bypass") + " " + codexToolResultShellQuote(engine) + " tool-result --root " + codexToolResultShellQuote(root) + " acp-proxy --task-id " + codexToolResultShellQuote(req.TaskID) + " --agent-id " + codexToolResultShellQuote(req.AgentID) + " --session-id " + codexToolResultShellQuote(plan.ACPXSessionName) + " -- " + plan.ACPXAgent
	previousAgent, previousSession := plan.ACPXAgent, plan.ACPXSessionName
	plan.ACPXAgent = command
	// Never reconnect to a previously unfiltered agent process after upgrade.
	plan.ACPXSessionName = previousSession + "-tool-results-v1"
	rewrite := func(args []string) ([]string, error) {
		out := make([]string, 0, len(args)+1)
		found := false
		for i, arg := range args {
			switch arg {
			case previousAgent:
				if i == 0 || args[i-1] != "--agent" {
					out = append(out, "--agent")
				}
				out = append(out, command)
				found = true
			case previousSession:
				out = append(out, plan.ACPXSessionName)
			default:
				out = append(out, arg)
			}
		}
		if len(args) > 0 && !found {
			return nil, fmt.Errorf("%s ACP arguments omit native agent identity", brand.BinaryName)
		}
		return out, nil
	}
	for _, args := range []*[]string{&plan.ACPXShowArgs, &plan.ACPXEnsureArgs, &plan.ACPXSetModeArgs, &plan.ACPXPromptArgs} {
		updated, err := rewrite(*args)
		if err != nil {
			return LaunchPlan{}, err
		}
		*args = updated
	}
	return plan, nil
}
