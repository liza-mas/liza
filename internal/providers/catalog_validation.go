package providers

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

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
		Backend:     BackendACPX,
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
	if p.Backend != BackendCLI && p.Backend != BackendACPX {
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
	if rt.PromptTransport != "" && rt.PromptTransport != PromptTransportStdin && rt.PromptTransport != PromptTransportArg && rt.PromptTransport != PromptTransportFile {
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
	return value != "." && !paths.IsAbsAnyPlatform(value) && !strings.HasPrefix(value, "..") && !strings.ContainsAny(value, "\x00\r\n")
}
