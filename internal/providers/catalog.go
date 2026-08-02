package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

var (
	errCatalogNotModified = errors.New("provider catalog not modified")
	// ErrUnstableGlobalRoot identifies environment roots whose meaning changes
	// with the provider process's working directory.
	ErrUnstableGlobalRoot = errors.New("provider global root is unstable across working directories")
)

const (
	DefaultCatalogURL       = "https://raw.githubusercontent.com/liza-mas/liza/main/provider-catalog.yaml"
	defaultCatalogTTL       = time.Hour
	defaultCatalogTimeout   = 1500 * time.Millisecond
	envCatalogURLSuffix     = "PROVIDER_CATALOG_URL"
	envCatalogTTLSuffix     = "PROVIDER_CATALOG_TTL"
	envCatalogTimeoutSuffix = "PROVIDER_CATALOG_TIMEOUT"
)

var (
	EnvCatalogURL     = brand.EnvName(envCatalogURLSuffix)
	EnvCatalogTTL     = brand.EnvName(envCatalogTTLSuffix)
	EnvCatalogTimeout = brand.EnvName(envCatalogTimeoutSuffix)
)

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
	ClaudeIgnore        bool `yaml:"claude_ignore,omitempty"`
	MistralPromptConfig bool `yaml:"mistral_prompt_config,omitempty"`
	BashPolicyClaude    bool `yaml:"bash_policy_claude,omitempty"`
	BashPolicyCodex     bool `yaml:"bash_policy_codex,omitempty"`
}

type Runtime struct {
	ProviderKey         string   `yaml:"provider_key,omitempty"`
	Executable          string   `yaml:"executable,omitempty"`
	PromptTransport     string   `yaml:"prompt_transport,omitempty"`
	RunArgs             []string `yaml:"run_args,omitempty"`
	LoggedRunArgs       []string `yaml:"logged_run_args,omitempty"`
	InteractiveArgs     []string `yaml:"interactive_args,omitempty"`
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

type LoadOptions struct {
	URL      string
	HomeDir  string
	TTL      time.Duration
	Timeout  time.Duration
	Force    bool
	Client   *http.Client
	LookPath func(string) (string, error)
	Now      func() time.Time
}

type CacheMeta struct {
	URL          string    `json:"url"`
	ETag         string    `json:"etag,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	FetchedAt    time.Time `json:"fetched_at"`
}

type DetectionResult struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Installed   bool   `json:"installed"`
	Executable  string `json:"executable,omitempty"`
	Error       string `json:"error,omitempty"`
}

func EmbeddedCatalog() Catalog {
	rendered, err := brandrender.RenderBytes([]byte(embeddedFallbackCatalogYAML), brand.RuntimeValues())
	if err != nil {
		panic(err)
	}
	cat, err := ParseCatalog(rendered)
	if err != nil {
		panic(err)
	}
	return cat
}

func ParseCatalog(data []byte) (Catalog, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cat Catalog
	if err := dec.Decode(&cat); err != nil {
		return Catalog{}, err
	}
	if err := cat.Validate(); err != nil {
		return Catalog{}, err
	}
	return cat, nil
}

func (c *Catalog) Validate() error {
	if c.Version <= 0 {
		return fmt.Errorf("catalog version must be positive")
	}
	if len(c.Providers) == 0 {
		return fmt.Errorf("catalog must define at least one provider")
	}
	byID := make(map[string]Provider, len(c.Providers))
	for _, p := range c.Providers {
		if err := validateProvider(p); err != nil {
			return err
		}
		if _, exists := byID[p.ID]; exists {
			return fmt.Errorf("duplicate provider id: %s", p.ID)
		}
		byID[p.ID] = p
	}
	// Synthesize <id>-acp entries from each provider's acp_runtime so that
	// consumers can resolve ACP variants (e.g. "codex-acp") without separate
	// catalog entries. The synthesized provider inherits setup/detection from
	// the base provider and uses the acp_runtime as its Runtime.
	for _, p := range c.Providers {
		if p.ACPRuntime == nil {
			continue
		}
		acpID := p.ID + "-acp"
		if _, exists := byID[acpID]; exists {
			return fmt.Errorf("duplicate provider id: %s (synthesized from acp_runtime of %s)", acpID, p.ID)
		}
		byID[acpID] = synthesizeACPProvider(p)
	}
	// Aliases are validated against the complete id set so an alias colliding
	// with a provider id declared later in the list is still rejected.
	aliases := make(map[string]string)
	for _, p := range c.Providers {
		for _, alias := range p.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			if _, providerExists := byID[alias]; providerExists {
				return fmt.Errorf("alias %s conflicts with provider id", alias)
			}
			if existing, exists := aliases[alias]; exists && existing != p.ID {
				return fmt.Errorf("alias %s maps to both %s and %s", alias, existing, p.ID)
			}
			aliases[alias] = p.ID
		}
	}
	c.byID = byID
	c.aliases = aliases
	return nil
}

// synthesizeACPProvider builds a virtual <id>-acp Provider from a base
// provider's acp_runtime block. The synthesized provider has backend "acpx",
// its Runtime populated from the acp_runtime config, and inherits setup and
// detection from the base provider.
func synthesizeACPProvider(base Provider) Provider {
	return Provider{
		ID:          base.ID + "-acp",
		DisplayName: base.DisplayName + " ACP",
		Backend:     "acpx",
		Detection:   base.Detection,
		Setup:       base.Setup,
		Runtime:     *base.ACPRuntime,
	}
}

func validateProvider(p Provider) error {
	if !validID(p.ID) {
		return fmt.Errorf("invalid provider id: %q", p.ID)
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return fmt.Errorf("provider %s missing display_name", p.ID)
	}
	if p.Backend != "cli" && p.Backend != "acpx" {
		return fmt.Errorf("provider %s has unsupported backend %q", p.ID, p.Backend)
	}
	for _, value := range p.Detection.Binaries {
		if !validExecutable(value) {
			return fmt.Errorf("provider %s has invalid executable %q", p.ID, value)
		}
	}
	if err := validateRuntime(p.ID, "runtime", p.Runtime); err != nil {
		return err
	}
	for _, arg := range p.Detection.VersionArgs {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("provider %s has invalid version arg", p.ID)
		}
	}
	for _, path := range []string{
		p.Setup.ConfigDir,
		p.Setup.SkillsDir,
		p.Setup.Contract.RepoFile,
		p.Setup.Contract.GlobalFallback,
		p.Setup.Contract.GlobalFallbackEnvSuffix,
		p.Setup.Contract.LocalFallback,
	} {
		if path != "" && !validRelativePath(path) {
			return fmt.Errorf("provider %s has invalid setup path %q", p.ID, path)
		}
	}
	contract := p.Setup.Contract
	if (contract.GlobalFallbackEnv == "") != (contract.GlobalFallbackEnvSuffix == "") {
		return fmt.Errorf("provider %s must define both global_fallback_env and global_fallback_env_suffix", p.ID)
	}
	if contract.GlobalFallbackEnv != "" {
		if contract.GlobalFallback == "" {
			return fmt.Errorf("provider %s defines global_fallback_env without global_fallback", p.ID)
		}
		if !validEnvName(contract.GlobalFallbackEnv) {
			return fmt.Errorf("provider %s has invalid global_fallback_env %q", p.ID, contract.GlobalFallbackEnv)
		}
	}
	if contract.GlobalFallbackEnvExpandHome && contract.GlobalFallbackEnv == "" {
		return fmt.Errorf("provider %s expands a global fallback environment home path without global_fallback_env", p.ID)
	}
	if contract.PrefersGlobal() && contract.GlobalFallback == "" {
		return fmt.Errorf("provider %s prefers global contract without global_fallback", p.ID)
	}
	for _, path := range p.Setup.ExtraDirs {
		if !validRelativePath(path) {
			return fmt.Errorf("provider %s has invalid setup path %q", p.ID, path)
		}
	}
	for _, link := range p.Setup.Symlinks {
		if !validRelativePath(link.Source) || !validRelativePath(link.Target) {
			return fmt.Errorf("provider %s has invalid setup symlink", p.ID)
		}
	}
	if p.ACPRuntime != nil {
		if err := validateRuntime(p.ID, "acp_runtime", *p.ACPRuntime); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntime(providerID, label string, rt Runtime) error {
	if rt.Executable == "" {
		return fmt.Errorf("provider %s missing %s.executable", providerID, label)
	}
	if !validExecutable(rt.Executable) {
		return fmt.Errorf("provider %s has invalid %s.executable %q", providerID, label, rt.Executable)
	}
	if rt.PromptTransport != "" && rt.PromptTransport != "stdin" && rt.PromptTransport != "arg" && rt.PromptTransport != "file" {
		return fmt.Errorf("provider %s has unsupported %s prompt transport %q", providerID, label, rt.PromptTransport)
	}
	for _, value := range rt.RequiredExecutables {
		if !validExecutable(value) {
			return fmt.Errorf("provider %s has invalid %s executable %q", providerID, label, value)
		}
	}
	for _, path := range rt.EnvFiles {
		if !validRelativePath(path) {
			return fmt.Errorf("provider %s has invalid %s env file %q", providerID, label, path)
		}
	}
	return nil
}

func validID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func validEnvName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validExecutable(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && !strings.ContainsAny(value, "/\\\x00\r\n\t ;|&$")
}

func validRelativePath(value string) bool {
	value = filepath.Clean(strings.TrimSpace(value))
	return value != "." && !filepath.IsAbs(value) && !strings.HasPrefix(value, "..") && !strings.ContainsAny(value, "\x00\r\n")
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

func (c Catalog) RuntimeTools() map[string]models.AgentToolConfig {
	out := make(map[string]models.AgentToolConfig, len(c.Providers)*2)
	for _, p := range c.Providers {
		out[p.ID] = p.RuntimeToolConfig()
		if p.ACPRuntime != nil {
			out[p.ID+"-acp"] = p.ACPToolConfig()
		}
	}
	return out
}

func (p Provider) RuntimeToolConfig() models.AgentToolConfig {
	return runtimeToolConfig(p.ID, p.Backend, p.Runtime)
}

// ACPToolConfig returns the AgentToolConfig derived from the provider's
// acp_runtime block. Returns an empty config if the provider has no ACP
// runtime.
func (p Provider) ACPToolConfig() models.AgentToolConfig {
	if p.ACPRuntime == nil {
		return models.AgentToolConfig{}
	}
	return runtimeToolConfig(p.ID+"-acp", "acpx", *p.ACPRuntime)
}

func runtimeToolConfig(id, backend string, rt Runtime) models.AgentToolConfig {
	required := append([]string(nil), rt.RequiredExecutables...)
	if len(required) == 0 && backend == "acpx" {
		required = []string{"acpx"}
	}
	transport := rt.PromptTransport
	if transport == "" {
		transport = "stdin"
	}
	providerKey := rt.ProviderKey
	if providerKey == "" {
		providerKey = id
	}
	return models.AgentToolConfig{
		Backend:             backend,
		ProviderKey:         providerKey,
		Executable:          rt.Executable,
		PromptTransport:     transport,
		RunArgs:             append([]string(nil), rt.RunArgs...),
		LoggedRunArgs:       append([]string(nil), rt.LoggedRunArgs...),
		InteractiveArgs:     append([]string(nil), rt.InteractiveArgs...),
		EnvFiles:            append([]string(nil), rt.EnvFiles...),
		RequiredExecutables: required,
		ContractKey:         rt.ContractKey,
		ACPXAgent:           rt.ACPXAgent,
		ACPXSessionName:     rt.ACPXSessionName,
		ACPXShowArgs:        append([]string(nil), rt.ACPXShowArgs...),
		ACPXEnsureArgs:      append([]string(nil), rt.ACPXEnsureArgs...),
		ACPXSetModeArgs:     append([]string(nil), rt.ACPXSetModeArgs...),
		ACPXPromptArgs:      append([]string(nil), rt.ACPXPromptArgs...),
		ACPXEventMode:       rt.ACPXEventMode,
	}
}

func Detect(cat Catalog, lookPath func(string) (string, error)) []DetectionResult {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	results := make([]DetectionResult, 0, len(cat.Providers))
	for _, p := range cat.ProvidersSorted() {
		binaries := p.Detection.Binaries
		if len(binaries) == 0 {
			binaries = []string{p.Runtime.Executable}
		}
		result := DetectionResult{ID: p.ID, DisplayName: p.DisplayName}
		for _, bin := range binaries {
			path, err := lookPath(bin)
			if err == nil {
				result.Installed = true
				result.Executable = path
				break
			}
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}

func Load(ctx context.Context, opts LoadOptions) (Catalog, error) {
	url := firstNonEmpty(opts.URL, catalogEnvValue(envCatalogURLSuffix), DefaultCatalogURL)
	homeDir := opts.HomeDir
	if homeDir == "" {
		var err error
		homeDir, err = paths.UserHomeDir()
		if err != nil {
			if opts.Force {
				return Catalog{}, err
			}
			return EmbeddedCatalog(), nil
		}
	}
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = durationFromEnv(envCatalogTTLSuffix, defaultCatalogTTL)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = durationFromEnv(envCatalogTimeoutSuffix, defaultCatalogTimeout)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	cachePath, metaPath := CachePaths(homeDir)
	meta, _ := readMeta(metaPath)
	if !opts.Force && meta.URL == url && !meta.FetchedAt.IsZero() && now().Sub(meta.FetchedAt) < ttl {
		if cat, err := readCatalogFile(cachePath); err == nil {
			return cat, nil
		}
	}
	requestMeta := meta
	if requestMeta.URL != url {
		requestMeta = CacheMeta{}
	}
	cat, metaOut, err := fetchCatalog(ctx, opts.Client, url, timeout, requestMeta, now().UTC())
	if err == nil {
		if cacheErr := writeCatalogCache(cachePath, metaPath, cat.raw, metaOut); cacheErr != nil && opts.Force {
			// The fetch itself succeeded and cat.catalog is valid; only the
			// disk persist failed. Return it alongside the error instead of
			// discarding it, so a caller that wants the freshly-fetched data
			// despite a non-durable refresh isn't forced to refetch.
			return cat.catalog, cacheErr
		}
		return cat.catalog, nil
	}
	if errors.Is(err, errCatalogNotModified) {
		if meta.URL != url {
			if opts.Force {
				return Catalog{}, fmt.Errorf("provider catalog not modified but cache belongs to %q", meta.URL)
			}
			return EmbeddedCatalog(), nil
		}
		cached, cacheErr := readCatalogFile(cachePath)
		if cacheErr != nil {
			if opts.Force {
				return Catalog{}, fmt.Errorf("provider catalog not modified but cache is unavailable: %w", cacheErr)
			}
		} else {
			if writeErr := writeMeta(metaPath, metaOut); writeErr != nil && opts.Force {
				return Catalog{}, writeErr
			}
			return cached, nil
		}
	}
	if opts.Force {
		return Catalog{}, err
	}
	if meta.URL == url {
		if cat, cacheErr := readCatalogFile(cachePath); cacheErr == nil {
			return cat, nil
		}
	}
	if cat, cacheErr := readCatalogFile(cachePath); cacheErr == nil && meta.URL == "" {
		return cat, nil
	}
	if cat, legacyPath, legacyErr := readLegacyCatalogCache(homeDir, cachePath); legacyErr == nil {
		fmt.Fprintf(os.Stderr, "Warning: using legacy provider catalog cache %s; refresh providers to migrate it to %s\n", legacyPath, cachePath)
		return cat, nil
	}
	return EmbeddedCatalog(), nil
}

func CachePaths(homeDir string) (catalogPath, metaPath string) {
	cacheDir := filepath.Join(homeDir, paths.GlobalDirName(), "cache")
	return filepath.Join(cacheDir, "provider-catalog.yaml"), filepath.Join(cacheDir, "provider-catalog.meta.json")
}

func readLegacyCatalogCache(homeDir, brandedCachePath string) (Catalog, string, error) {
	if paths.GlobalDirName() == ".liza" {
		return Catalog{}, "", os.ErrNotExist
	}
	if _, err := os.Stat(brandedCachePath); err == nil || !os.IsNotExist(err) {
		return Catalog{}, "", os.ErrNotExist
	}
	// Pre-brand cache location retained for offline upgrades.
	legacyPath := filepath.Join(homeDir, ".liza", "cache", "provider-catalog.yaml")
	cat, err := readCatalogFile(legacyPath)
	return cat, legacyPath, err
}

type fetchedCatalog struct {
	catalog Catalog
	raw     []byte
}

func fetchCatalog(ctx context.Context, client *http.Client, url string, timeout time.Duration, meta CacheMeta, fetchedAt time.Time) (fetchedCatalog, CacheMeta, error) {
	parsed, err := neturl.Parse(url)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("invalid provider catalog URL: %w", err)
	}
	host := parsed.Hostname()
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopback) {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("provider catalog URL must use HTTPS")
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		metaOut := meta
		metaOut.URL = url
		if etag := resp.Header.Get("ETag"); etag != "" {
			metaOut.ETag = etag
		}
		if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
			metaOut.LastModified = lastModified
		}
		metaOut.FetchedAt = fetchedAt
		return fetchedCatalog{}, metaOut, errCatalogNotModified
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fetchedCatalog{}, CacheMeta{}, fmt.Errorf("provider catalog fetch returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	cat, err := ParseCatalog(data)
	if err != nil {
		return fetchedCatalog{}, CacheMeta{}, err
	}
	return fetchedCatalog{catalog: cat, raw: data}, CacheMeta{
		URL:          url,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		FetchedAt:    fetchedAt,
	}, nil
}

func writeCatalogCache(cachePath, metaPath string, data []byte, meta CacheMeta) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return err
	}
	return writeMeta(metaPath, meta)
}

func readCatalogFile(path string) (Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, err
	}
	return ParseCatalog(data)
}

func readMeta(path string) (CacheMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheMeta{}, err
	}
	var meta CacheMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return CacheMeta{}, err
	}
	return meta, nil
}

func writeMeta(path string, meta CacheMeta) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func catalogEnvValue(suffix string) string {
	lookup := brand.LookupEnv(os.Getenv, suffix)
	if lookup.Warning != "" {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", lookup.Warning)
	}
	return lookup.Value
}

func durationFromEnv(suffix string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(catalogEnvValue(suffix))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
