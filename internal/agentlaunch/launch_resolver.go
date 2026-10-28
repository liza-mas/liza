// Package agentlaunch resolves how a provider CLI is launched: the effective
// tool registry (catalog merged with agent_tools), CLI, profile and model
// selection, role-model validation, launch-plan rendering, executable
// prerequisites and the default-role validation policy. It starts no process,
// registers nothing and touches no worktree, so the agent runtime, process
// spawning and presentation share it without importing one another.
package agentlaunch

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/roles"
)

// Template variable names the launch templates may reference as {{name}}. The
// names below are the ones the resolver itself reads or writes.
const (
	templateVarAgentID     = "agentID"
	templateVarTaskID      = "taskID"
	templateVarGlobalDir   = "globalDir"
	templateVarModel       = "model"
	templateVarACPXAgent   = "acpxAgent"
	templateVarSessionName = "sessionName"
)

// Claude CLI argument literals of the subagent denial: it is inserted before the
// first of the logging flags.
const (
	claudeDisallowedToolsFlag = "--disallowedTools"
	claudeDeniedTaskTool      = "Task"
	claudeVerboseFlag         = "--verbose"
	claudeOutputFormatFlag    = "--output-format"
)

// Delimiters of a template expression; templateExprRE matches the same pair.
const (
	templateExprOpen  = "{{"
	templateExprClose = "}}"
)

var templateExprRE = regexp.MustCompile(`\{\{([^{}]+)\}\}`)

var (
	runtimeCatalogOnce  sync.Once
	runtimeCatalog      providers.Catalog
	embeddedCatalogOnce sync.Once
	embeddedCatalog     providers.Catalog
)

type ResolvedProfile struct {
	Name string
	Vars map[string]string
}

type LaunchPlan struct {
	ToolName             string
	ProfileName          string
	Model                string
	Backend              providers.Backend
	Executable           string
	Args                 []string
	PromptTransport      providers.PromptTransport
	PromptFile           string
	EnvFiles             []string
	OptionalEnvFiles     bool
	ValidationExecution  string
	Environment          []string
	Directory            string
	RequiredExecutables  []string
	ContractKey          string
	UsesStdin            bool
	UsesPromptFile       bool
	RequiresCodexWrapper bool
	ProviderKey          string
	ACPXAgent            string
	ACPXSessionName      string
	ACPXShowArgs         []string
	ACPXEnsureArgs       []string
	ACPXSetModeArgs      []string
	ACPXPromptArgs       []string
	ACPXEventMode        string
}

type LaunchPlanRequest struct {
	ToolName         string
	ProfileName      string
	ProfileVars      map[string]string
	Model            string // first-class model selection; empty keeps the tool default
	Prompt           string
	PromptFile       string
	ProjectRoot      string
	AgentID          string
	TaskID           string
	SessionID        string
	SessionScope     string
	OutputsDir       string
	RuntimeConfig    models.Config
	DisableSubagents bool
	Interactive      bool
}

// UseRuntimeCatalog replaces the memoised runtime catalog and returns the
// function that restores it. Tests of callers use it to pin the catalog they
// assert against, so the result does not depend on the on-disk cache or the
// network; production code never calls it.
func UseRuntimeCatalog(catalog providers.Catalog) (restore func()) {
	runtimeCatalogOnce.Do(func() {})
	previous := runtimeCatalog
	runtimeCatalog = catalog
	return func() { runtimeCatalog = previous }
}

func BuiltInAgentTools() map[string]models.AgentToolConfig {
	embeddedCatalogOnce.Do(func() {
		embeddedCatalog = providers.EmbeddedCatalog()
	})
	runtimeCatalogOnce.Do(func() {
		cat, _ := providers.Load(context.Background(), providers.LoadOptions{})
		runtimeCatalog = cat
	})
	return agentToolsFromCatalogs(embeddedCatalog, runtimeCatalog)
}

func agentToolsFromCatalogs(embedded, loaded providers.Catalog) map[string]models.AgentToolConfig {
	registry := embedded.RuntimeTools()
	for name, tool := range loaded.RuntimeTools() {
		embeddedTool, builtIn := registry[name]
		merged := mergeAgentToolConfig(name, embeddedTool, tool)
		if builtIn {
			// Embedded session templates are rendered for this build's brand.
			// Loaded catalogs may be stale or default-branded, so merge their
			// operational fields without replacing built-in session identity.
			merged.ACPXSessionName = embeddedTool.ACPXSessionName
		}
		registry[name] = merged
	}
	return registry
}

func AgentToolRegistry(config models.Config) map[string]models.AgentToolConfig {
	registry := BuiltInAgentTools()
	overlayAgentTools(registry, config)
	return registry
}

// overlayAgentTools applies the project's agent_tools over registry in place.
func overlayAgentTools(registry map[string]models.AgentToolConfig, config models.Config) {
	for name, override := range config.AgentTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		base := registry[name]
		registry[name] = mergeAgentToolConfig(name, base, override)
	}
}

func AvailableCLIs(config models.Config) []string {
	registry := AgentToolRegistry(config)
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ResolveProfileForRole(explicitProfile string, roleType string, config models.Config) (ResolvedProfile, error) {
	name := strings.TrimSpace(explicitProfile)
	if name == "" {
		name = roleSpecificProfile(roleType, config)
	}
	if name == "" {
		name = strings.TrimSpace(config.DefaultProfile)
	}
	if name == "" {
		return ResolvedProfile{}, nil
	}
	profile, ok := config.AgentProfiles[name]
	if !ok {
		return ResolvedProfile{}, fmt.Errorf("unknown agent profile: %s", name)
	}
	return ResolvedProfile{Name: name, Vars: cloneStringMap(profile.Vars)}, nil
}

func ResolveCLIWithProfile(flagChanged bool, flagValue string, profile ResolvedProfile, roleType string, config models.Config) (string, error) {
	if flagChanged {
		return flagValue, nil
	}
	if profile.Name != "" {
		profileConfig := config.AgentProfiles[profile.Name]
		if profileConfig.CLI != "" {
			return profileConfig.CLI, nil
		}
	}
	return ResolveDefaultCLIForRole(roleType, CLIResolutionConfig{
		DefaultCLI:         config.DefaultCLI,
		DefaultDoerCLI:     config.DefaultDoerCLI,
		DefaultReviewerCLI: config.DefaultReviewerCLI,
	}), nil
}

func ResolveLaunchPlan(req LaunchPlanRequest) (LaunchPlan, error) {
	toolName := strings.TrimSpace(req.ToolName)
	if toolName == "" {
		return LaunchPlan{}, fmt.Errorf("missing CLI name")
	}
	tool, ok := AgentToolRegistry(req.RuntimeConfig)[toolName]
	if !ok {
		return LaunchPlan{}, fmt.Errorf("unknown CLI: %s", toolName)
	}
	base, err := resolveLaunchBase(toolName, tool, req.Model)
	if err != nil {
		return LaunchPlan{}, err
	}

	vars := launchTemplateVars(req, toolName)
	acpxAgent, sessionName, err := expandLaunchSession(req, toolName, tool, base.backend, vars)
	if err != nil {
		return LaunchPlan{}, err
	}
	args, err := expandLaunchArgs(req, toolName, tool, base.model, vars)
	if err != nil {
		return LaunchPlan{}, err
	}
	acpx, err := expandACPXArgs(toolName, tool, vars)
	if err != nil {
		return LaunchPlan{}, err
	}

	return LaunchPlan{
		ToolName:             toolName,
		Directory:            req.ProjectRoot,
		ProfileName:          req.ProfileName,
		Model:                base.model,
		Backend:              base.backend,
		Executable:           base.executable,
		Args:                 args,
		PromptTransport:      base.transport,
		PromptFile:           req.PromptFile,
		EnvFiles:             append([]string(nil), tool.EnvFiles...),
		OptionalEnvFiles:     len(req.RuntimeConfig.AgentTools[toolName].EnvFiles) == 0,
		ValidationExecution:  tool.ValidationExecution,
		RequiredExecutables:  append([]string(nil), tool.RequiredExecutables...),
		ContractKey:          strings.TrimSpace(tool.ContractKey),
		UsesStdin:            base.transport == providers.PromptTransportStdin,
		UsesPromptFile:       base.transport == providers.PromptTransportFile,
		RequiresCodexWrapper: toolName == "codex",
		ProviderKey:          strings.TrimSpace(tool.ProviderKey),
		ACPXAgent:            acpxAgent,
		ACPXSessionName:      sessionName,
		ACPXShowArgs:         acpx.show,
		ACPXEnsureArgs:       acpx.ensure,
		ACPXSetModeArgs:      acpx.setMode,
		ACPXPromptArgs:       acpx.prompt,
		ACPXEventMode:        strings.TrimSpace(tool.ACPXEventMode),
	}, nil
}

// launchBase is a tool's validated launch identity with its defaults applied.
type launchBase struct {
	backend           providers.Backend
	transport         providers.PromptTransport
	executable, model string
}

// resolveLaunchBase applies the backend, executable and transport defaults and
// refuses an unsupported transport, backend or model selection, in that order.
func resolveLaunchBase(toolName string, tool models.AgentToolConfig, requestedModel string) (launchBase, error) {
	base := launchBase{
		backend:    providers.Backend(strings.TrimSpace(tool.Backend)),
		executable: strings.TrimSpace(tool.Executable),
		transport:  providers.PromptTransport(strings.TrimSpace(tool.PromptTransport)),
		model:      strings.TrimSpace(requestedModel),
	}
	if base.backend == "" {
		base.backend = providers.BackendCLI
	}
	if base.executable == "" {
		base.executable = toolName
	}
	if base.transport == "" {
		base.transport = providers.PromptTransportStdin
	}
	if err := validatePromptTransport(base.transport); err != nil {
		return launchBase{}, err
	}
	if base.backend != providers.BackendCLI && base.backend != providers.BackendACPX {
		return launchBase{}, fmt.Errorf("unsupported backend for %s: %s", toolName, base.backend)
	}
	if base.model != "" && !SupportsModelSelection(tool) {
		return launchBase{}, fmt.Errorf("%s does not support model selection", toolName)
	}
	return base, nil
}

// expandLaunchSession resolves the ACPX agent name and renders the session
// name, recording both as template variables for the argument expansion.
func expandLaunchSession(req LaunchPlanRequest, toolName string, tool models.AgentToolConfig, backend providers.Backend, vars map[string]string) (acpxAgent, sessionName string, err error) {
	acpxAgent = strings.TrimSpace(tool.ACPXAgent)
	if acpxAgent == "" && backend == providers.BackendACPX {
		acpxAgent = ACPXAgentName(toolName)
	}
	sessionTemplate := strings.TrimSpace(tool.ACPXSessionName)
	if sessionTemplate == "" && backend == providers.BackendACPX {
		sessionTemplate = ACPXSessionName(templateExprOpen + templateVarAgentID + templateExprClose)
	}

	vars[templateVarACPXAgent] = acpxAgent
	if sessionTemplate == "" {
		return acpxAgent, "", nil
	}
	sessionName, err = renderArg(sessionTemplate, vars)
	if err != nil {
		return "", "", fmt.Errorf("%s acpx session name: %w", toolName, err)
	}
	// Scope the session to the task so context cannot accumulate across
	// tasks: a session that spans tasks grows until the provider rejects
	// every prompt, and auto-repair then respawns into the same poisoned
	// session (DEV-667). Templates that already place {{taskID}} keep
	// full control.
	templateIncludesTaskID := false
	for _, match := range templateExprRE.FindAllString(sessionTemplate, -1) {
		name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, templateExprOpen), templateExprClose))
		if name == templateVarTaskID {
			templateIncludesTaskID = true
			break
		}
	}
	if scope := acpxSessionTaskScope(req.TaskID); scope != "" && !templateIncludesTaskID {
		sessionName = sessionName + "-" + scope
	}
	if req.SessionScope != "" {
		sessionName += "-" + req.SessionScope
	}
	vars[templateVarSessionName] = sessionName
	return acpxAgent, sessionName, nil
}

// expandLaunchArgs selects the tool's argument template for the launch mode,
// renders it and leads with the model arguments when a model is selected.
func expandLaunchArgs(req LaunchPlanRequest, toolName string, tool models.AgentToolConfig, model string, vars map[string]string) ([]string, error) {
	args := tool.RunArgs
	if req.Interactive {
		args = tool.InteractiveArgs
	} else if req.OutputsDir != "" && len(tool.LoggedRunArgs) > 0 {
		args = tool.LoggedRunArgs
	}
	args = append([]string(nil), args...)
	if toolName == "claude" && req.DisableSubagents {
		args = appendDisallowedTaskArg(args)
	}
	rendered, err := renderArgs(args, vars)
	if err != nil {
		return nil, fmt.Errorf("%s args: %w", toolName, err)
	}
	if model == "" {
		return rendered, nil
	}
	// Model flags lead the argv: the supported CLIs take them as
	// top-level options, before any subcommand such as codex exec.
	modelArgs, err := renderArgs(tool.ModelArgs, map[string]string{templateVarModel: model})
	if err != nil {
		return nil, fmt.Errorf("%s model args: %w", toolName, err)
	}
	return append(modelArgs, rendered...), nil
}

// acpxArgs are the rendered ACPX subcommand argument lists of a launch.
type acpxArgs struct {
	show, ensure, setMode, prompt []string
}

func expandACPXArgs(toolName string, tool models.AgentToolConfig, vars map[string]string) (acpxArgs, error) {
	var out acpxArgs
	for _, list := range []struct {
		label string
		args  []string
		into  *[]string
	}{
		{"show", tool.ACPXShowArgs, &out.show},
		{"ensure", tool.ACPXEnsureArgs, &out.ensure},
		{"set-mode", tool.ACPXSetModeArgs, &out.setMode},
		{"prompt", tool.ACPXPromptArgs, &out.prompt},
	} {
		rendered, err := renderArgs(list.args, vars)
		if err != nil {
			return acpxArgs{}, fmt.Errorf("%s acpx %s args: %w", toolName, list.label, err)
		}
		*list.into = rendered
	}
	return out, nil
}

// SupportsModelSelection reports whether a tool passes a first-class model:
// it declares model_args and runs as a direct CLI. ACPX launches carry no
// argv model, so a model there would be recorded without being used.
func SupportsModelSelection(tool models.AgentToolConfig) bool {
	backend := providers.Backend(strings.TrimSpace(tool.Backend))
	return len(tool.ModelArgs) > 0 && (backend == "" || backend == providers.BackendCLI)
}

func mergeAgentToolConfig(name string, base, override models.AgentToolConfig) models.AgentToolConfig {
	out := base
	if override.ValidationExecution != "" {
		out.ValidationExecution = override.ValidationExecution
	}
	if override.Backend != "" {
		out.Backend = override.Backend
	}
	if override.Executable != "" {
		out.Executable = override.Executable
	}
	if override.PromptTransport != "" {
		out.PromptTransport = override.PromptTransport
	}
	if len(override.RunArgs) > 0 {
		out.RunArgs = append([]string(nil), override.RunArgs...)
	}
	if len(override.LoggedRunArgs) > 0 {
		out.LoggedRunArgs = append([]string(nil), override.LoggedRunArgs...)
	}
	if len(override.InteractiveArgs) > 0 {
		out.InteractiveArgs = append([]string(nil), override.InteractiveArgs...)
	}
	if len(override.ModelArgs) > 0 {
		out.ModelArgs = append([]string(nil), override.ModelArgs...)
	}
	if len(override.EnvFiles) > 0 {
		out.EnvFiles = append([]string(nil), override.EnvFiles...)
	}
	if len(override.RequiredExecutables) > 0 {
		out.RequiredExecutables = append([]string(nil), override.RequiredExecutables...)
	}
	if override.ContractKey != "" {
		out.ContractKey = override.ContractKey
	}
	if override.ProviderKey != "" {
		out.ProviderKey = override.ProviderKey
	}
	if override.ACPXAgent != "" {
		out.ACPXAgent = override.ACPXAgent
	}
	if override.ACPXSessionName != "" {
		out.ACPXSessionName = override.ACPXSessionName
	}
	if len(override.ACPXShowArgs) > 0 {
		out.ACPXShowArgs = append([]string(nil), override.ACPXShowArgs...)
	}
	if len(override.ACPXEnsureArgs) > 0 {
		out.ACPXEnsureArgs = append([]string(nil), override.ACPXEnsureArgs...)
	}
	if len(override.ACPXSetModeArgs) > 0 {
		out.ACPXSetModeArgs = append([]string(nil), override.ACPXSetModeArgs...)
	}
	if len(override.ACPXPromptArgs) > 0 {
		out.ACPXPromptArgs = append([]string(nil), override.ACPXPromptArgs...)
	}
	if override.ACPXEventMode != "" {
		out.ACPXEventMode = override.ACPXEventMode
	}
	if out.Backend == "" {
		out.Backend = string(providers.BackendCLI)
	}
	if out.Executable == "" {
		out.Executable = name
	}
	if out.PromptTransport == "" {
		out.PromptTransport = string(providers.PromptTransportStdin)
	}
	if out.ProviderKey == "" {
		out.ProviderKey = name
	}
	return out
}

func roleSpecificProfile(roleType string, config models.Config) string {
	switch roles.RoleType(roleType) {
	case roles.RoleTypeDoer, roles.RoleTypeOrchestrator:
		return strings.TrimSpace(config.DefaultDoerProfile)
	case roles.RoleTypeReviewer:
		return strings.TrimSpace(config.DefaultReviewerProfile)
	default:
		return ""
	}
}

func validatePromptTransport(value providers.PromptTransport) error {
	switch value {
	case providers.PromptTransportStdin, providers.PromptTransportArg, providers.PromptTransportFile:
		return nil
	default:
		return fmt.Errorf("unsupported prompt transport: %s", value)
	}
}

// acpxSessionTaskScope preserves the validated task ID so session names stay
// collision-free across distinct valid task IDs.
func acpxSessionTaskScope(taskID string) string {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return ""
	}
	return taskID
}

func launchTemplateVars(req LaunchPlanRequest, toolName string) map[string]string {
	vars := map[string]string{
		"tool":             toolName,
		"cli":              toolName,
		"profile":          req.ProfileName,
		"prompt":           req.Prompt,
		"promptFile":       req.PromptFile,
		"projectRoot":      req.ProjectRoot,
		templateVarAgentID: req.AgentID,
		templateVarTaskID:  req.TaskID,
		"sessionID":        req.SessionID,
		"outputsDir":       req.OutputsDir,
	}
	// globalDir is left undefined when the home directory cannot be resolved,
	// so a run_arg referencing it fails rendering with an explicit error
	// instead of degrading to a relative path that does not exist.
	if dir, err := paths.GlobalLizaDir(); err == nil && filepath.IsAbs(dir) {
		vars[templateVarGlobalDir] = dir
	}
	for key, value := range req.ProfileVars {
		if key = strings.TrimSpace(key); key != "" {
			vars["profile."+key] = value
		}
	}
	return vars
}

func renderArgs(args []string, vars map[string]string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		rendered, err := renderArg(arg, vars)
		if err != nil {
			return nil, err
		}
		out = append(out, rendered)
	}
	return out, nil
}

func renderArg(arg string, vars map[string]string) (string, error) {
	var missing []string
	rendered := templateExprRE.ReplaceAllStringFunc(arg, func(match string) string {
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, templateExprOpen), templateExprClose))
		value, ok := vars[key]
		if !ok {
			missing = append(missing, key)
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		if slices.Contains(missing, templateVarGlobalDir) {
			return "", fmt.Errorf("unknown template variable(s): %s ("+templateVarGlobalDir+" requires a resolvable home directory)", strings.Join(missing, ", "))
		}
		return "", fmt.Errorf("unknown template variable(s): %s", strings.Join(missing, ", "))
	}
	return rendered, nil
}

func appendDisallowedTaskArg(args []string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, args...)
	insert := len(out)
	for i, arg := range out {
		if arg == claudeVerboseFlag || arg == claudeOutputFormatFlag {
			insert = i
			break
		}
	}
	// Keep Claude's Task denial before logging flags such as --verbose/--output-format.
	out = append(out, "", "")
	copy(out[insert+2:], out[insert:])
	out[insert] = claudeDisallowedToolsFlag
	out[insert+1] = claudeDeniedTaskTool
	return out
}

// ACPXAgentName is the canonical provider name behind a tool name: the ACPX
// agent of an ACP tool, otherwise the tool name itself. Provider signals use it
// to treat a tool and its ACP counterpart as one provider.
func ACPXAgentName(toolName string) string {
	switch toolName {
	case "codex-acp", "acpx-codex":
		return "codex"
	default:
		if strings.HasPrefix(toolName, "acpx-") {
			return strings.TrimPrefix(toolName, "acpx-")
		}
		if strings.HasSuffix(toolName, "-acp") {
			return strings.TrimSuffix(toolName, "-acp")
		}
		return toolName
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
