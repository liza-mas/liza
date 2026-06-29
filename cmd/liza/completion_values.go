package main

import (
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/toolchain"
	"github.com/spf13/cobra"
)

func completeValues(values ...string) cobra.CompletionFunc {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return filterCompletionValues(values, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeCLINames(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterCompletionValues(agent.ValidCLIs(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func toolIDs() []string {
	var ids []string
	for _, tool := range toolchain.Catalog() {
		ids = append(ids, tool.ID)
	}
	return ids
}

func completeToolIDs(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filterCompletionValues(toolIDs(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeToolIDsOrAll(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ids := append(toolIDs(), "all")
	return filterCompletionValues(ids, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completePipelineTransitions(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	projectRoot, ok := completionProjectRoot(cmd)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	transitions := pipeline.NewResolver(cfg).AllTransitions()
	names := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		names = append(names, transition.Name)
	}
	return filterCompletionValues(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeAgentRoles(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	roleNames := completionProjectRoles(cmd)
	if len(roleNames) == 0 {
		roleNames = roles.All()
	}
	return filterCompletionValues(roleNames, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeAgentArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeAgentRoles(cmd, args, toComplete)
	case 1:
		return completeTaskIDs(cmd, args, toComplete)
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

func completeTaskIDs(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	state, ok := completionState(cmd)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterCompletionValues(taskIDsFromState(state), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeAgentIDs(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	state, ok := completionState(cmd)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return filterCompletionValues(agentIDsFromState(state), toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeTaskIDArgs(maxArgs int) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTaskIDs(cmd, args, toComplete)
	}
}

func completeAgentIDArgs(maxArgs int) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeAgentIDs(cmd, args, toComplete)
	}
}

func completeGetQueries(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	values := []string{
		"config.mode",
		"sprint.status",
		"sprint.elapsed",
		"sprint.metrics.tasks_done",
		"tasks",
		"agents",
		"metrics",
		"anomalies",
	}
	if state, ok := completionState(cmd); ok {
		values = append(values, taskIDsFromState(state)...)
		values = append(values, agentIDsFromState(state)...)
	}
	return filterCompletionValues(values, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completionState(cmd *cobra.Command) (*models.State, bool) {
	projectRoot, ok := completionProjectRoot(cmd)
	if !ok {
		return nil, false
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).Read()
	if err != nil {
		return nil, false
	}
	return state, true
}

func completionProjectRoles(cmd *cobra.Command) []string {
	projectRoot, ok := completionProjectRoot(cmd)
	if !ok {
		return nil
	}
	cfg, err := pipeline.LoadFrozen(projectRoot)
	if err != nil {
		return nil
	}
	return pipeline.NewResolver(cfg).AllRoleNames()
}

func completionProjectRoot(cmd *cobra.Command) (string, bool) {
	root := ""
	if cmd != nil {
		root, _ = cmd.Root().PersistentFlags().GetString("project-root")
	}
	if root != "" {
		projectRoot, err := paths.GetProjectRootFromDir(root)
		return projectRoot, err == nil
	}
	projectRoot, err := paths.GetProjectRoot()
	return projectRoot, err == nil
}

func taskIDsFromState(state *models.State) []string {
	if state == nil {
		return nil
	}
	ids := make([]string, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if task.ID != "" {
			ids = append(ids, task.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func agentIDsFromState(state *models.State) []string {
	if state == nil {
		return nil
	}
	ids := make([]string, 0, len(state.Agents))
	for id := range state.Agents {
		if id != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func filterCompletionValues(values []string, prefix string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || !strings.HasPrefix(value, prefix) {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func registerCompletion(cmd *cobra.Command, flagName string, fn cobra.CompletionFunc) {
	if err := cmd.RegisterFlagCompletionFunc(flagName, fn); err != nil {
		panic(err)
	}
}
