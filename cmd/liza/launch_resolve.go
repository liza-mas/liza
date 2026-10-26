package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/spf13/cobra"
)

// launchBackend names a terminal multiplexer `launch` can drive. Its values are
// the subcommand names and the executables looked up on PATH.
type launchBackend string

const (
	launchBackendWezterm launchBackend = "wezterm"
	launchBackendCmux    launchBackend = "cmux"
)

// masLaunchPreset names a role set `launch <backend> mas --preset` expands.
type masLaunchPreset string

const (
	masPresetTechnicalSpec    masLaunchPreset = "technical-spec"
	masPresetFunctionalSpec   masLaunchPreset = "functional-spec"
	masPresetGeneralObjective masLaunchPreset = "general-objective"
)

// defaultWeztermPromptDelay is the wait before WezTerm panes receive their
// prompts, and the --prompt-delay default.
const defaultWeztermPromptDelay = 2 * time.Second

type weztermLaunchOptions struct {
	Class       string
	Workspace   string
	CWD         string
	DryRun      bool
	PromptDelay time.Duration
}

type cmuxLaunchOptions struct {
	Workspace string
	CWD       string
	DryRun    bool
}

type interactivePane struct {
	CLIName string
	Command []string
	Prompt  string
}

func masLaunchPresets() map[masLaunchPreset][]string {
	return map[masLaunchPreset][]string{
		masPresetTechnicalSpec: {
			"orchestrator",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
		masPresetFunctionalSpec: {
			"orchestrator",
			"architect",
			"architecture-reviewer",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
		masPresetGeneralObjective: {
			"orchestrator",
			"epic-planner",
			"epic-plan-reviewer",
			"us-writer",
			"us-reviewer",
			"architect",
			"architecture-reviewer",
			"code-planner",
			"code-plan-reviewer",
			"coder",
			"code-reviewer",
		},
	}
}

func launchWorkingDir(cmd *cobra.Command, requireLizaProject bool) (string, error) {
	cwd, _ := cmd.Flags().GetString("cwd")
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", cliValidationWrap("resolve --cwd", err)
		}
		if requireLizaProject {
			if err := validateLaunchProjectRoot(abs); err != nil {
				return "", err
			}
		}
		return abs, nil
	}
	if requireLizaProject {
		return requireProjectRoot()
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", cliValidationWrap("get current directory", err)
	}
	return wd, nil
}

func validateLaunchProjectRoot(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return cliValidationWrap("inspect --cwd", err)
	}
	if !info.IsDir() {
		return cliValidationError(fmt.Sprintf("--cwd %q is not a directory", path))
	}
	output, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return cliValidationWrap("resolve --cwd git root", err)
	}
	gitRoot := strings.TrimSpace(string(output))
	canonicalPath, err := canonicalDir(path)
	if err != nil {
		return cliValidationWrap("resolve --cwd", err)
	}
	canonicalRoot, err := canonicalDir(gitRoot)
	if err != nil {
		return cliValidationWrap("resolve --cwd git root", err)
	}
	if canonicalPath != canonicalRoot {
		return cliValidationError(fmt.Sprintf("--cwd must be the %s project root, got %s under git root %s", brand.NameTitle, path, gitRoot))
	}
	if _, err := os.Stat(filepath.Join(path, paths.ProjectDirName(), "state.yaml")); err != nil {
		if os.IsNotExist(err) {
			return cliValidationError(fmt.Sprintf("--cwd %s is not initialized as a %s project", path, brand.NameTitle))
		}
		return cliValidationWrap(fmt.Sprintf("inspect --cwd %s state", brand.NameTitle), err)
	}
	return nil
}

func resolveLaunchPath(cwd, path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Abs(filepath.Join(cwd, path))
}

func weztermOptionsFromFlags(cmd *cobra.Command, cwd, defaultClass string) (weztermLaunchOptions, error) {
	className, _ := cmd.Flags().GetString("class")
	if className == "" {
		className = defaultClass
	}
	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		workspace = className
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	promptDelay := defaultWeztermPromptDelay
	if cmd.Flags().Lookup("prompt-delay") != nil {
		promptDelay, _ = cmd.Flags().GetDuration("prompt-delay")
		if promptDelay < 0 {
			return weztermLaunchOptions{}, cliValidationError("--prompt-delay must not be negative")
		}
	}
	return weztermLaunchOptions{Class: className, Workspace: workspace, CWD: cwd, DryRun: dryRun, PromptDelay: promptDelay}, nil
}

func cmuxOptionsFromFlags(cmd *cobra.Command, cwd, defaultWorkspace string) (cmuxLaunchOptions, error) {
	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		className, _ := cmd.Flags().GetString("class")
		if className != "" {
			workspace = className
		} else {
			workspace = defaultWorkspace
		}
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return cmuxLaunchOptions{Workspace: workspace, CWD: cwd, DryRun: dryRun}, nil
}

func launchAvailableCLIs(projectRoot string) []string {
	if projectRoot == "" {
		return agent.ValidCLIs()
	}
	state, err := db.For(paths.New(projectRoot).StatePath()).ReadSnapshot()
	if err != nil {
		return agent.ValidCLIs()
	}
	return agent.AvailableCLIs(state.Config)
}

func parseReviewerLaunchSpec(spec string) (string, string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", cliValidationError("--reviewer values must not be empty")
	}
	if strings.Contains(spec, "=") {
		parts := strings.SplitN(spec, "=", 2)
		id := strings.TrimPrefix(strings.TrimSpace(parts[0]), "reviewer-")
		cliName := strings.TrimSpace(parts[1])
		if id == "" || cliName == "" {
			return "", "", cliValidationError(fmt.Sprintf("invalid reviewer spec %q; use id=cli or cli", spec))
		}
		return id, cliName, nil
	}
	id := strings.TrimPrefix(spec, "reviewer-")
	return id, id, nil
}
