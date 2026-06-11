package toolchain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
)

type ConfigureOptions struct {
	Profile           Profile
	Include           []string
	Exclude           []string
	GlobalDir         string
	InstallDir        string
	AgentToolsMode    string
	WriteShellProfile bool
	HomeDir           string
	Shell             string
}

type ConfigureResult struct {
	Profile           Profile  `json:"profile"`
	ToolchainDir      string   `json:"toolchain_dir"`
	ProfilePath       string   `json:"profile_path"`
	EnvPath           string   `json:"env_path"`
	InstallDir        string   `json:"install_dir"`
	AgentToolsPath    string   `json:"agent_tools_path,omitempty"`
	ShellProfilePath  string   `json:"shell_profile_path,omitempty"`
	ShellProfilePaths []string `json:"shell_profile_paths,omitempty"`
	SelectedTools     []string `json:"selected_tools"`
	ActivationEnv     []string `json:"activation_env"`
}

type profileFile struct {
	Profile       Profile  `json:"profile"`
	SelectedTools []string `json:"selected_tools"`
	ActivationEnv []string `json:"activation_env"`
	GeneratedAt   string   `json:"generated_at"`
	InstallDir    string   `json:"install_dir"`
}

func Configure(opts ConfigureOptions) (ConfigureResult, error) {
	selection, err := ResolveSelection(opts.Profile, opts.Include, opts.Exclude)
	if err != nil {
		return ConfigureResult{}, err
	}
	agentToolsMode, err := normalizeAgentToolsMode(opts.AgentToolsMode)
	if err != nil {
		return ConfigureResult{}, err
	}
	globalDir, err := resolveGlobalDir(opts.GlobalDir)
	if err != nil {
		return ConfigureResult{}, err
	}
	installDir, err := resolveInstallDir(opts.InstallDir)
	if err != nil {
		return ConfigureResult{}, err
	}
	toolchainDir := filepath.Join(globalDir, "toolchain")
	if err := os.MkdirAll(toolchainDir, 0o755); err != nil {
		return ConfigureResult{}, fmt.Errorf("create toolchain dir: %w", err)
	}

	selectedIDs := selectedToolIDs(selection.Tools)
	activation := activationEnv(selection.Tools)
	profilePath := filepath.Join(toolchainDir, "profile.json")
	envPath := filepath.Join(toolchainDir, "env.sh")

	profilePayload, err := json.MarshalIndent(profileFile{
		Profile:       selection.Profile,
		SelectedTools: selectedIDs,
		ActivationEnv: activation,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		InstallDir:    installDir,
	}, "", "  ")
	if err != nil {
		return ConfigureResult{}, fmt.Errorf("render profile: %w", err)
	}
	if err := os.WriteFile(profilePath, append(profilePayload, '\n'), 0o644); err != nil {
		return ConfigureResult{}, fmt.Errorf("write profile: %w", err)
	}
	if err := os.WriteFile(envPath, []byte(renderEnvFile(installDir, activation)), 0o644); err != nil {
		return ConfigureResult{}, fmt.Errorf("write env: %w", err)
	}

	agentToolsPath, err := configureAgentTools(globalDir, agentToolsMode)
	if err != nil {
		return ConfigureResult{}, err
	}

	result := ConfigureResult{
		Profile:        selection.Profile,
		ToolchainDir:   toolchainDir,
		ProfilePath:    profilePath,
		EnvPath:        envPath,
		InstallDir:     installDir,
		AgentToolsPath: agentToolsPath,
		SelectedTools:  selectedIDs,
		ActivationEnv:  activation,
	}
	if opts.WriteShellProfile {
		paths, err := appendShellProfileSources(opts.HomeDir, opts.Shell, envPath)
		if err != nil {
			return ConfigureResult{}, err
		}
		if len(paths) > 0 {
			result.ShellProfilePath = paths[0]
		}
		result.ShellProfilePaths = paths
	}
	return result, nil
}

func configureAgentTools(globalDir, mode string) (string, error) {
	mode, err := normalizeAgentToolsMode(mode)
	if err != nil {
		return "", err
	}
	if mode == "skip" {
		return "", nil
	}
	target := filepath.Join(globalDir, "AGENT_TOOLS.md")
	if _, err := os.Stat(target); err == nil && mode == "auto" {
		return target, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect AGENT_TOOLS.md: %w", err)
	}
	if mode == "force" {
		if content, err := os.ReadFile(target); err == nil {
			if err := os.WriteFile(target+".bak", content, 0o644); err != nil {
				return "", fmt.Errorf("backup AGENT_TOOLS.md: %w", err)
			}
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("read AGENT_TOOLS.md for backup: %w", err)
		}
	}

	skip := map[string]bool{}
	for _, planned := range embedded.PlanGlobalFiles(globalDir) {
		if planned != target {
			skip[planned] = true
		}
	}
	if _, err := embedded.WriteGlobalFiles(globalDir, skip); err != nil {
		return "", fmt.Errorf("write AGENT_TOOLS.md: %w", err)
	}
	return target, nil
}

func normalizeAgentToolsMode(mode string) (string, error) {
	if mode == "" {
		mode = "skip"
	}
	if mode != "auto" && mode != "skip" && mode != "force" {
		return "", fmt.Errorf("invalid agent-tools mode %q (must be auto, skip, or force)", mode)
	}
	return mode, nil
}

func selectedToolIDs(tools []Tool) []string {
	ids := make([]string, len(tools))
	for i, tool := range tools {
		ids[i] = tool.ID
	}
	sort.Strings(ids)
	return ids
}

func activationEnv(tools []Tool) []string {
	seen := map[string]bool{}
	var env []string
	for _, tool := range tools {
		for _, entry := range tool.ActivationEnv {
			if seen[entry] {
				continue
			}
			seen[entry] = true
			env = append(env, entry)
		}
	}
	sort.Strings(env)
	return env
}

func renderEnvFile(installDir string, activation []string) string {
	var b strings.Builder
	b.WriteString("# Generated by liza toolchain configure.\n")
	b.WriteString("# Source this file before running liza init or liza agents.\n")
	b.WriteString("export PATH=")
	b.WriteString(shellQuote(installDir))
	b.WriteString(":\"$PATH\"\n")
	for _, entry := range activation {
		name, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		b.WriteString("export ")
		b.WriteString(name)
		b.WriteString("=")
		b.WriteString(shellQuote(value))
		b.WriteString("\n")
	}
	if slices.Contains(activation, "LIZA_ENABLE_SEMBLE=1") {
		b.WriteString("# After Semble has been prewarmed and verified offline, operators may add:\n")
		b.WriteString("# export HF_HUB_OFFLINE=\"1\"\n")
	}
	return b.String()
}

func resolveGlobalDir(raw string) (string, error) {
	return resolveHomeDir(raw, ".liza")
}

func resolveHomeDir(raw, defaultRel string) (string, error) {
	needsHome := raw == "" || strings.HasPrefix(raw, "~/")
	if needsHome {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		if raw == "" {
			raw = filepath.Join(home, defaultRel)
		} else {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
		}
	}
	return filepath.Abs(raw)
}

func appendShellProfileSources(homeDir, shell, envPath string) ([]string, error) {
	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
	}
	var paths []string
	for _, name := range shellProfileNames(shell) {
		paths = append(paths, filepath.Join(homeDir, name))
	}
	for _, profilePath := range paths {
		if err := appendProfileSource(profilePath, envPath); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func shellProfileNames(shell string) []string {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	switch filepath.Base(shell) {
	case "bash":
		return []string{".bashrc", ".profile"}
	case "zsh":
		return []string{".zshrc"}
	default:
		return []string{".profile"}
	}
}

func appendProfileSource(profilePath, envPath string) error {
	quotedEnvPath := shellQuote(envPath)
	line := fmt.Sprintf("\n# Liza toolchain\n[ -f %s ] && . %s\n", quotedEnvPath, quotedEnvPath)
	existing, err := os.ReadFile(profilePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read shell profile: %w", err)
	}
	if strings.Contains(string(existing), envPath) {
		return nil
	}
	if err := os.WriteFile(profilePath, append(existing, []byte(line)...), 0o644); err != nil {
		return fmt.Errorf("write shell profile: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
