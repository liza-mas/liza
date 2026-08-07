// Package toolchain installs and validates optional local tools that make Liza
// cheaper and easier to operate.
package toolchain

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

type Profile string

const (
	ProfileBalanced Profile = "balanced"
	ProfileLean     Profile = "lean"
	ProfileFull     Profile = "full"
)

type Category string

const (
	CategoryIndexing    Category = "indexing"
	CategoryCompression Category = "compression"
	CategoryNavigation  Category = "navigation"
	CategoryStructured  Category = "structured-data"
	CategoryQuality     Category = "quality"
	CategoryRepository  Category = "repository"
	CategoryCost        Category = "cost"
	CategoryMCP         Category = "mcp"
)

type InstallKind string

const (
	InstallScript     InstallKind = "script"
	InstallGo         InstallKind = "go"
	InstallNPM        InstallKind = "npm"
	InstallUVTool     InstallKind = "uv-tool"
	InstallPackage    InstallKind = "package"
	InstallManualOnly InstallKind = "manual-only"
)

type Tool struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Binary      string      `json:"binary,omitempty"`
	Category    Category    `json:"category"`
	Purpose     string      `json:"purpose"`
	InstallKind InstallKind `json:"install_kind"`
	PackageName string      `json:"package_name,omitempty"`
	// PackageNamesByManager overrides PackageName for a specific package
	// manager, keyed by its binary name. Windows managers identify packages by
	// publisher-qualified IDs ("BurntSushi.ripgrep.MSVC"), which never match the
	// plain name the Unix managers use, so a manager with no entry here is
	// treated as unable to install the tool rather than guessing.
	PackageNamesByManager map[string]string `json:"package_names_by_manager,omitempty"`
	// WindowsArchiveURL is a release archive to unpack into the install
	// directory on Windows, for tools whose install script does not run there.
	WindowsArchiveURL string `json:"windows_archive_url,omitempty"`
	// ManualInstallNote is surfaced when no automated path exists on the current
	// platform, so the step reports what to run instead of a bare failure.
	ManualInstallNote string   `json:"manual_install_note,omitempty"`
	InstallURL        string   `json:"install_url,omitempty"`
	InstallDirEnv     []string `json:"install_dir_env,omitempty"`
	SourceRepo        string   `json:"source_repo,omitempty"`
	SourcePackage     string   `json:"source_package,omitempty"`
	GoPackage         string   `json:"go_package,omitempty"`
	NPMPackage        string   `json:"npm_package,omitempty"`
	UVPackage         string   `json:"uv_package,omitempty"`
	VersionArgs       []string `json:"version_args,omitempty"`
	ActivationEnv     []string `json:"activation_env,omitempty"`
	BalancedDefault   bool     `json:"balanced_default"`
	LeanDefault       bool     `json:"lean_default"`
	FullDefault       bool     `json:"full_default"`
	ManualNote        string   `json:"manual_note,omitempty"`
}

type Selection struct {
	Profile Profile `json:"profile"`
	Tools   []Tool  `json:"tools"`
}

var validProfiles = []Profile{ProfileBalanced, ProfileLean, ProfileFull}

func Catalog() []Tool {
	return []Tool{
		{
			ID: "rtk", Name: "RTK", Binary: "rtk", Category: CategoryCompression,
			Purpose:     "Compresses shell command output before it reaches agent context.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh",
			InstallDirEnv: []string{"RTK_INSTALL_DIR"},
			// The install script accepts Linux and Darwin only, and RTK has no Go
			// source to fall back to. Upstream publishes a native Windows build
			// and, since v0.37.2, a native hook that needs no Unix shell.
			WindowsArchiveURL: "https://github.com/rtk-ai/rtk/releases/latest/download/rtk-x86_64-pc-windows-msvc.zip",
			VersionArgs:       []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "stacklit", Name: "Stacklit", Binary: "stacklit", Category: CategoryIndexing,
			Purpose:     "Generates compact repository module and dependency maps.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/liza-mas/stacklit-cli/main/install.sh",
			VersionArgs: []string{"--version"}, ActivationEnv: []string{brand.EnvName("ENABLE_STACKLIT") + "=1"},
			BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "scip-search", Name: "SCIP Search", Binary: "scip-search", Category: CategoryIndexing,
			Purpose:     "Queries SCIP indexes for precise symbol, reference, and graph navigation.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/liza-mas/scip-search/main/install.sh",
			SourceRepo: "https://github.com/liza-mas/scip-search", SourcePackage: "./cmd/scip-search",
			VersionArgs: []string{"--version"}, ActivationEnv: []string{brand.EnvName("ENABLE_SCIP_SEARCH") + "=1"},
			BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "scip-go", Name: "SCIP Go indexer", Binary: "scip-go", Category: CategoryIndexing,
			Purpose:     "Generates SCIP indexes for Go projects.",
			InstallKind: InstallGo, GoPackage: "github.com/scip-code/scip-go/cmd/scip-go@latest",
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "scip-typescript", Name: "SCIP TypeScript indexer", Binary: "scip-typescript", Category: CategoryIndexing,
			Purpose:     "Generates SCIP indexes for TypeScript and JavaScript projects.",
			InstallKind: InstallNPM, NPMPackage: "@sourcegraph/scip-typescript",
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "scip-python", Name: "SCIP Python indexer", Binary: "scip-python", Category: CategoryIndexing,
			Purpose:     "Generates SCIP indexes for Python projects.",
			InstallKind: InstallNPM, NPMPackage: "@sourcegraph/scip-python",
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "semble", Name: "Semble", Binary: "semble", Category: CategoryNavigation,
			Purpose:     "Provides local semantic repository discovery.",
			InstallKind: InstallUVTool, UVPackage: "semble",
			VersionArgs: []string{"--help"}, ActivationEnv: []string{brand.EnvName("ENABLE_SEMBLE") + "=1"},
			BalancedDefault: true, FullDefault: true,
		},
		{
			ID: "rg", Name: "ripgrep", Binary: "rg", Category: CategoryNavigation,
			Purpose:     "Fast exact text search and file discovery.",
			InstallKind: InstallPackage, PackageName: "ripgrep",
			PackageNamesByManager: map[string]string{
				"winget": "BurntSushi.ripgrep.MSVC", "scoop": "ripgrep", "choco": "ripgrep",
			},
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "ast-grep", Name: "ast-grep", Binary: "ast-grep", Category: CategoryNavigation,
			Purpose:     "AST-aware structural search and rewrite.",
			InstallKind: InstallPackage, PackageName: "ast-grep",
			PackageNamesByManager: map[string]string{
				"winget": "ast-grep.ast-grep", "scoop": "ast-grep", "choco": "ast-grep",
			},
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "mdtoc", Name: "mdtoc", Binary: "mdtoc", Category: CategoryNavigation,
			Purpose:     "Prints Markdown section ranges and mdq selectors.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/liza-mas/mdtoc/main/install.sh",
			SourceRepo: "https://github.com/liza-mas/mdtoc", SourcePackage: "./cmd/mdtoc",
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "mdq", Name: "mdq", Binary: "mdq", Category: CategoryNavigation,
			Purpose:     "Queries Markdown documents by structure.",
			InstallKind: InstallPackage, PackageName: "mdq",
			// No Windows manager carries this mdq. The winget entry of that name
			// is a different tool.
			ManualInstallNote: "install with: cargo install mdq",
			VersionArgs:       []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "jq", Name: "jq", Binary: "jq", Category: CategoryStructured,
			Purpose:     "Queries JSON without reading entire files into context.",
			InstallKind: InstallPackage, PackageName: "jq",
			PackageNamesByManager: map[string]string{
				"winget": "jqlang.jq", "scoop": "jq", "choco": "jq",
			},
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "yq", Name: "yq", Binary: "yq", Category: CategoryStructured,
			Purpose:     "Queries YAML, TOML, XML, CSV, and related structured files.",
			InstallKind: InstallPackage, PackageName: "yq",
			PackageNamesByManager: map[string]string{
				"winget": "MikeFarah.yq", "scoop": "yq", "choco": "yq",
			},
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "gh", Name: "GitHub CLI", Binary: "gh", Category: CategoryRepository,
			Purpose:     "Uses authenticated GitHub workflows from shell commands.",
			InstallKind: InstallPackage, PackageName: "gh",
			PackageNamesByManager: map[string]string{
				"winget": "GitHub.cli", "scoop": "gh", "choco": "gh",
			},
			VersionArgs: []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "pre-commit", Name: "pre-commit", Binary: "pre-commit", Category: CategoryQuality,
			Purpose:     "Runs repository quality gates before delivery.",
			InstallKind: InstallPackage, PackageName: "pre-commit",
			// A Python tool: the Windows package managers do not carry it.
			ManualInstallNote: "install with: uv tool install pre-commit",
			VersionArgs:       []string{"--version"}, BalancedDefault: true, LeanDefault: true, FullDefault: true,
		},
		{
			ID: "functional-clusters", Name: "Functional Clusters", Binary: "functional-clusters", Category: CategoryIndexing,
			Purpose:     "Builds advisory capability clusters from SCIP graph and Stacklit exports.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/liza-mas/functional-clusters/main/install.sh",
			SourceRepo: "https://github.com/liza-mas/functional-clusters", SourcePackage: "./cmd/functional-clusters",
			VersionArgs: []string{"--version"}, FullDefault: true,
			ActivationEnv: []string{brand.EnvName("ENABLE_FUNCTIONAL_CLUSTERS") + "=1"},
		},
		{
			ID: "bash-policy", Name: "Bash Policy", Binary: "bash-policy", Category: CategoryQuality,
			Purpose:     "Installs provider-aware bash command policy hooks for Claude and Codex.",
			InstallKind: InstallScript, InstallURL: "https://raw.githubusercontent.com/liza-mas/bash-policy/main/install.sh",
			SourceRepo: "https://github.com/liza-mas/bash-policy", SourcePackage: "./cmd/bash-policy",
			VersionArgs: []string{"--version"}, BalancedDefault: true, FullDefault: true,
			ActivationEnv: []string{brand.EnvName("ENABLE_BASH_POLICY") + "=1"},
		},
		manualTool("filesystem-mcp", "filesystem MCP", "Batch local filesystem reads through provider MCP configuration."),
		manualTool("context7", "context7 MCP", "Structured current library documentation lookup through MCP."),
		manualTool("ref", "Ref MCP", "Broad documentation and guide lookup through MCP."),
		manualTool("fetch-mcp", "fetch MCP", "Exact web page retrieval through MCP."),
		manualTool("perplexity", "Perplexity MCP", "Current-info web search with synthesis through MCP."),
		manualTool("deepwiki", "DeepWiki MCP", "Repository architecture lookup through MCP."),
		manualTool("morph-mcp", "Morph MCP", "Semantic code search and fast-apply editing through MCP."),
		manualTool("postgres-mcp", "postgres MCP", "Read-only SQL exploration through MCP."),
	}
}

func manualTool(id, name, purpose string) Tool {
	return Tool{
		ID: id, Name: name, Category: CategoryMCP, Purpose: purpose,
		InstallKind: InstallManualOnly,
		ManualNote:  fmt.Sprintf("configure this capability in the active provider or MCP host; %s does not install credentials or provider-specific connectors", brand.NameTitle),
	}
}

func ResolveSelection(profile Profile, include, exclude []string) (Selection, error) {
	if profile == "" {
		profile = ProfileBalanced
	}
	if !slices.Contains(validProfiles, profile) {
		return Selection{}, fmt.Errorf("unknown profile %q (must be balanced, lean, or full)", profile)
	}

	catalog := Catalog()
	byID := map[string]Tool{}
	selected := map[string]bool{}
	for _, tool := range catalog {
		byID[tool.ID] = tool
		if profileSelects(profile, tool) {
			selected[tool.ID] = true
		}
	}

	for _, id := range include {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			return Selection{}, fmt.Errorf("unknown tool %q", id)
		}
		selected[id] = true
	}
	for _, id := range exclude {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			return Selection{}, fmt.Errorf("unknown tool %q", id)
		}
		delete(selected, id)
	}

	var tools []Tool
	for id := range selected {
		tools = append(tools, byID[id])
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].ID < tools[j].ID
	})
	return Selection{Profile: profile, Tools: tools}, nil
}

func profileSelects(profile Profile, tool Tool) bool {
	switch profile {
	case ProfileLean:
		return tool.LeanDefault
	case ProfileFull:
		return tool.FullDefault
	default:
		return tool.BalancedDefault
	}
}
