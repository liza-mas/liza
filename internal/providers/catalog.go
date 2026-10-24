package providers

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrUnstableGlobalRoot identifies environment roots whose meaning changes
// with the provider process's working directory.
var ErrUnstableGlobalRoot = errors.New("provider global root is unstable across working directories")

type Catalog struct {
	Version   int        `yaml:"version"`
	Providers []Provider `yaml:"providers"`

	byID    map[string]Provider
	aliases map[string]string
}

type Provider struct {
	ID          string   `yaml:"id"`
	DisplayName string   `yaml:"display_name"`
	Aliases     []string `yaml:"aliases,omitempty"`
	Backend     string   `yaml:"backend"`
	// Disabled is informational: it marks providers that are not yet
	// fully supported (e.g. missing stable CLI or ACP integration).
	// Disabled providers remain resolvable and detectable; the flag is
	// surfaced in `providers list` so users can see which providers are
	// experimental. Enforcement (skipping detection/setup) may be added
	// later.
	Disabled   bool      `yaml:"disabled,omitempty"`
	Detection  Detection `yaml:"detection,omitempty"`
	Setup      Setup     `yaml:"setup,omitempty"`
	Runtime    Runtime   `yaml:"runtime"`
	ACPRuntime *Runtime  `yaml:"acp_runtime,omitempty"`
}

type Detection struct {
	Binaries    []string `yaml:"binaries,omitempty"`
	VersionArgs []string `yaml:"version_args,omitempty"`
}

type Setup struct {
	ConfigDir        string           `yaml:"config_dir,omitempty"`
	SkillsDir        string           `yaml:"skills_dir,omitempty"`
	ExtraDirs        []string         `yaml:"extra_dirs,omitempty"`
	Symlinks         []Symlink        `yaml:"symlinks,omitempty"`
	Contract         ContractLinks    `yaml:"contract,omitempty"`
	ActivationAssets ActivationAssets `yaml:"activation_assets,omitempty"`
}

type Symlink struct {
	Source string `yaml:"source"`
	Target string `yaml:"target"`
}

type ContractLinks struct {
	RepoFile                    string `yaml:"repo_file,omitempty"`
	GlobalFallback              string `yaml:"global_fallback,omitempty"`
	GlobalFallbackEnv           string `yaml:"global_fallback_env,omitempty"`
	GlobalFallbackEnvSuffix     string `yaml:"global_fallback_env_suffix,omitempty"`
	GlobalFallbackEnvExpandHome bool   `yaml:"global_fallback_env_expand_home,omitempty"`
	LocalFallback               string `yaml:"local_fallback,omitempty"`
	// PreferGlobal selects the global managed link when both contract locations
	// exist. A nil value allows stale catalogs to inherit the embedded default.
	PreferGlobal *bool `yaml:"prefer_global,omitempty"`
}

func (c ContractLinks) PrefersGlobal() bool {
	return c.PreferGlobal != nil && *c.PreferGlobal
}

// GlobalPath resolves the contract path that the provider actually reads.
// The catalog default is relative to the user's home directory. Providers
// with a documented config-root override can replace it through an environment
// variable while keeping the instruction filename catalog-owned.
func (c ContractLinks) GlobalPath(homeDir string) (string, error) {
	if c.GlobalFallback == "" {
		return "", nil
	}
	if c.GlobalFallbackEnv != "" {
		if root := strings.TrimSpace(os.Getenv(c.GlobalFallbackEnv)); root != "" {
			// The XDG base-directory specification requires relative values to
			// be ignored, so use the catalog's home-relative default instead.
			if c.GlobalFallbackEnv == "XDG_CONFIG_HOME" && !filepath.IsAbs(root) {
				return filepath.Join(homeDir, c.GlobalFallback), nil
			}
			if strings.HasPrefix(root, "~") && !c.GlobalFallbackEnvExpandHome {
				return "", fmt.Errorf("%s must contain an absolute path", c.GlobalFallbackEnv)
			}
			if root == "~" {
				root = homeDir
			} else if strings.HasPrefix(root, "~/") || strings.HasPrefix(root, `~\`) {
				root = filepath.Join(homeDir, root[2:])
			} else if strings.HasPrefix(root, "~") {
				return "", fmt.Errorf("%s contains unsupported home expansion %q", c.GlobalFallbackEnv, root)
			}
			if !filepath.IsAbs(root) {
				return "", fmt.Errorf("%w: %s relative path %q", ErrUnstableGlobalRoot, c.GlobalFallbackEnv, root)
			}
			return filepath.Join(filepath.Clean(root), c.GlobalFallbackEnvSuffix), nil
		}
	}
	return filepath.Join(homeDir, c.GlobalFallback), nil
}

type ActivationAssets struct {
	ClaudeSettings      bool `yaml:"claude_settings,omitempty"`
	CodexConfig         bool `yaml:"codex_config,omitempty"`
	CodexHooks          bool `yaml:"codex_hooks,omitempty"`
	OpenCodeExecTool    bool `yaml:"opencode_exec_tool,omitempty"`
	CursorHooks         bool `yaml:"cursor_hooks,omitempty"`
	CopilotHooks        bool `yaml:"copilot_hooks,omitempty"`
	ClaudeIgnore        bool `yaml:"claude_ignore,omitempty"`
	MistralPromptConfig bool `yaml:"mistral_prompt_config,omitempty"`
	BashPolicyClaude    bool `yaml:"bash_policy_claude,omitempty"`
	BashPolicyCodex     bool `yaml:"bash_policy_codex,omitempty"`
	PiExtension         bool `yaml:"pi_extension,omitempty"`
}

type Runtime struct {
	ProviderKey         string   `yaml:"provider_key,omitempty"`
	Executable          string   `yaml:"executable,omitempty"`
	PromptTransport     string   `yaml:"prompt_transport,omitempty"`
	RunArgs             []string `yaml:"run_args,omitempty"`
	LoggedRunArgs       []string `yaml:"logged_run_args,omitempty"`
	InteractiveArgs     []string `yaml:"interactive_args,omitempty"`
	ModelArgs           []string `yaml:"model_args,omitempty"`
	EnvFiles            []string `yaml:"env_files,omitempty"`
	RequiredExecutables []string `yaml:"required_executables,omitempty"`
	ContractKey         string   `yaml:"contract_key,omitempty"`
	ACPXAgent           string   `yaml:"acpx_agent,omitempty"`
	ACPXSessionName     string   `yaml:"acpx_session_name,omitempty"`
	ACPXShowArgs        []string `yaml:"acpx_show_args,omitempty"`
	ACPXEnsureArgs      []string `yaml:"acpx_ensure_args,omitempty"`
	ACPXSetModeArgs     []string `yaml:"acpx_set_mode_args,omitempty"`
	ACPXPromptArgs      []string `yaml:"acpx_prompt_args,omitempty"`
	ACPXEventMode       string   `yaml:"acpx_event_mode,omitempty"`
}

func (c Catalog) ProvidersSorted() []Provider {
	providers := append([]Provider(nil), c.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].ID < providers[j].ID })
	return providers
}

// AllProvidersSorted returns every resolvable provider — declared entries plus
// synthesized ACP variants — sorted by ID. Use this when listing the full
// catalog surface (e.g. `providers list`); use ProvidersSorted for detection,
// which should only iterate declared entries.
func (c Catalog) AllProvidersSorted() []Provider {
	if c.byID == nil {
		_ = c.Validate()
	}
	all := make([]Provider, 0, len(c.byID))
	for _, p := range c.byID {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	return all
}

func (c Catalog) Resolve(id string) (Provider, bool) {
	if c.byID == nil {
		_ = c.Validate()
	}
	id = strings.TrimSpace(id)
	if p, ok := c.byID[id]; ok {
		return p, true
	}
	if resolved, ok := c.aliases[id]; ok {
		p, ok := c.byID[resolved]
		return p, ok
	}
	return Provider{}, false
}

// WithEmbeddedBuiltins returns cat plus every embedded built-in provider it
// does not already define. Listing and detection use it so a provider that
// ships embedded-only (because released binaries could not parse its catalog
// fields) stays visible when a fetched catalog predates it. Providers present
// in cat always win; an embedded provider whose id, synthesized -acp id or
// alias would collide with cat is skipped individually.
func WithEmbeddedBuiltins(cat Catalog) Catalog {
	merged := cat
	for _, p := range EmbeddedCatalog().Providers {
		if _, ok := merged.Resolve(p.ID); ok {
			continue
		}
		candidate := Catalog{Version: merged.Version, Providers: append(append([]Provider(nil), merged.Providers...), p)}
		if err := candidate.Validate(); err != nil {
			continue
		}
		merged = candidate
	}
	return merged
}
