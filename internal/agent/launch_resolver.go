package agent

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/providers"
)

const (
	PromptTransportStdin = "stdin"
	PromptTransportArg   = "arg"
	PromptTransportFile  = "file"

	ToolBackendCLI  = "cli"
	ToolBackendACPX = "acpx"
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
	Backend              string
	Executable           string
	Args                 []string
	PromptTransport      string
	PromptFile           string
	EnvFiles             []string
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
	Prompt           string
	PromptFile       string
	ProjectRoot      string
	AgentID          string
	TaskID           string
	SessionID        string
	OutputsDir       string
	RuntimeConfig    models.Config
	DisableSubagents bool
	Interactive      bool
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
		registry[name] = mergeAgentToolConfig(name, registry[name], tool)
	}
	return registry
}

func AgentToolRegistry(config models.Config) map[string]models.AgentToolConfig {
	registry := BuiltInAgentTools()
	for name, override := range config.AgentTools {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		base := registry[name]
		registry[name] = mergeAgentToolConfig(name, base, override)
	}
	return registry
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

	backend := strings.TrimSpace(tool.Backend)
	if backend == "" {
		backend = ToolBackendCLI
	}
	executable := strings.TrimSpace(tool.Executable)
	if executable == "" {
		executable = toolName
	}
	transport := strings.TrimSpace(tool.PromptTransport)
	if transport == "" {
		transport = PromptTransportStdin
	}
	if err := validatePromptTransport(transport); err != nil {
		return LaunchPlan{}, err
	}
	if backend != ToolBackendCLI && backend != ToolBackendACPX {
		return LaunchPlan{}, fmt.Errorf("unsupported backend for %s: %s", toolName, backend)
	}

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

	acpxAgent := strings.TrimSpace(tool.ACPXAgent)
	if acpxAgent == "" && backend == ToolBackendACPX {
		acpxAgent = acpxAgentNameFromTool(toolName)
	}
	sessionTemplate := strings.TrimSpace(tool.ACPXSessionName)
	if sessionTemplate == "" && backend == ToolBackendACPX {
		sessionTemplate = acpxSessionName("{{agentID}}")
	}

	vars := launchTemplateVars(req, toolName)
	vars["acpxAgent"] = acpxAgent
	renderedSessionName := ""
	if sessionTemplate != "" {
		var err error
		renderedSessionName, err = renderArg(sessionTemplate, vars)
		if err != nil {
			return LaunchPlan{}, fmt.Errorf("%s acpx session name: %w", toolName, err)
		}
		// Scope the session to the task so context cannot accumulate across
		// tasks: a session that spans tasks grows until the provider rejects
		// every prompt, and auto-repair then respawns into the same poisoned
		// session (DEV-667). Templates that already place {{taskID}} keep
		// full control.
		if scope := acpxSessionTaskScope(req.TaskID); scope != "" && !strings.Contains(sessionTemplate, "{{taskID}}") {
			renderedSessionName = renderedSessionName + "-" + scope
		}
		vars["sessionName"] = renderedSessionName
	}

	renderedArgs, err := renderArgs(args, vars)
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("%s args: %w", toolName, err)
	}
	acpxShowArgs, err := renderArgs(tool.ACPXShowArgs, vars)
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("%s acpx show args: %w", toolName, err)
	}
	acpxEnsureArgs, err := renderArgs(tool.ACPXEnsureArgs, vars)
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("%s acpx ensure args: %w", toolName, err)
	}
	acpxSetModeArgs, err := renderArgs(tool.ACPXSetModeArgs, vars)
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("%s acpx set-mode args: %w", toolName, err)
	}
	acpxPromptArgs, err := renderArgs(tool.ACPXPromptArgs, vars)
	if err != nil {
		return LaunchPlan{}, fmt.Errorf("%s acpx prompt args: %w", toolName, err)
	}

	return LaunchPlan{
		ToolName:             toolName,
		ProfileName:          req.ProfileName,
		Backend:              backend,
		Executable:           executable,
		Args:                 renderedArgs,
		PromptTransport:      transport,
		PromptFile:           req.PromptFile,
		EnvFiles:             append([]string(nil), tool.EnvFiles...),
		RequiredExecutables:  append([]string(nil), tool.RequiredExecutables...),
		ContractKey:          strings.TrimSpace(tool.ContractKey),
		UsesStdin:            transport == PromptTransportStdin,
		UsesPromptFile:       transport == PromptTransportFile,
		RequiresCodexWrapper: toolName == "codex",
		ProviderKey:          strings.TrimSpace(tool.ProviderKey),
		ACPXAgent:            acpxAgent,
		ACPXSessionName:      renderedSessionName,
		ACPXShowArgs:         acpxShowArgs,
		ACPXEnsureArgs:       acpxEnsureArgs,
		ACPXSetModeArgs:      acpxSetModeArgs,
		ACPXPromptArgs:       acpxPromptArgs,
		ACPXEventMode:        strings.TrimSpace(tool.ACPXEventMode),
	}, nil
}

func mergeAgentToolConfig(name string, base, override models.AgentToolConfig) models.AgentToolConfig {
	out := base
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
		out.Backend = ToolBackendCLI
	}
	if out.Executable == "" {
		out.Executable = name
	}
	if out.PromptTransport == "" {
		out.PromptTransport = PromptTransportStdin
	}
	if out.ProviderKey == "" {
		out.ProviderKey = name
	}
	return out
}

func roleSpecificProfile(roleType string, config models.Config) string {
	switch roleType {
	case "doer", "orchestrator":
		return strings.TrimSpace(config.DefaultDoerProfile)
	case "reviewer":
		return strings.TrimSpace(config.DefaultReviewerProfile)
	default:
		return ""
	}
}

func validatePromptTransport(value string) error {
	switch value {
	case PromptTransportStdin, PromptTransportArg, PromptTransportFile:
		return nil
	default:
		return fmt.Errorf("unsupported prompt transport: %s", value)
	}
}

// acpxSessionTaskScope converts a task ID into a session-name suffix. Task IDs
// are engine-generated kebab-case, but sanitize defensively so the name stays
// safe as an acpx session key.
func acpxSessionTaskScope(taskID string) string {
	taskID = strings.ToLower(strings.TrimSpace(taskID))
	if taskID == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range taskID {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func launchTemplateVars(req LaunchPlanRequest, toolName string) map[string]string {
	vars := map[string]string{
		"tool":        toolName,
		"cli":         toolName,
		"profile":     req.ProfileName,
		"prompt":      req.Prompt,
		"promptFile":  req.PromptFile,
		"projectRoot": req.ProjectRoot,
		"agentID":     req.AgentID,
		"taskID":      req.TaskID,
		"sessionID":   req.SessionID,
		"outputsDir":  req.OutputsDir,
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
		key := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		value, ok := vars[key]
		if !ok {
			missing = append(missing, key)
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("unknown template variable(s): %s", strings.Join(missing, ", "))
	}
	return rendered, nil
}

func appendDisallowedTaskArg(args []string) []string {
	out := make([]string, 0, len(args)+2)
	out = append(out, args...)
	insert := len(out)
	for i, arg := range out {
		if arg == "--verbose" || arg == "--output-format" {
			insert = i
			break
		}
	}
	// Keep Claude's Task denial before logging flags such as --verbose/--output-format.
	out = append(out, "", "")
	copy(out[insert+2:], out[insert:])
	out[insert] = "--disallowedTools"
	out[insert+1] = "Task"
	return out
}

func acpxAgentNameFromTool(toolName string) string {
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
