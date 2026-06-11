package toolchain

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type InstallOptions struct {
	Profile    Profile
	Include    []string
	Exclude    []string
	InstallDir string
	DryRun     bool
	Runner     Runner
	GOOS       string
}

type InstallStatus string

const (
	InstallPlanned     InstallStatus = "planned"
	InstallSkipped     InstallStatus = "skipped"
	InstallInstalled   InstallStatus = "installed"
	InstallUnsupported InstallStatus = "unsupported"
	InstallFailed      InstallStatus = "failed"
)

type InstallStep struct {
	ToolID  string        `json:"tool_id"`
	Status  InstallStatus `json:"status"`
	Message string        `json:"message,omitempty"`
	Command Command       `json:"command,omitempty"`
	Output  CommandOutput `json:"output,omitempty"`
}

type InstallResult struct {
	Profile    Profile       `json:"profile"`
	InstallDir string        `json:"install_dir"`
	Steps      []InstallStep `json:"steps"`
}

func Install(opts InstallOptions) (InstallResult, error) {
	runner := runnerOrDefault(opts.Runner)
	installDir, err := resolveInstallDir(opts.InstallDir)
	if err != nil {
		return InstallResult{}, err
	}
	selection, err := ResolveSelection(opts.Profile, opts.Include, opts.Exclude)
	if err != nil {
		return InstallResult{}, err
	}
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	result := InstallResult{Profile: selection.Profile, InstallDir: installDir}
	for _, tool := range selection.Tools {
		step := installOne(tool, installDir, goos, opts.DryRun, runner)
		result.Steps = append(result.Steps, step)
	}
	return result, nil
}

func installOne(tool Tool, installDir, goos string, dryRun bool, runner Runner) InstallStep {
	if tool.InstallKind == InstallManualOnly {
		return InstallStep{ToolID: tool.ID, Status: InstallSkipped, Message: tool.ManualNote}
	}
	if goos == "windows" {
		return InstallStep{ToolID: tool.ID, Status: InstallUnsupported, Message: "native Windows support is doctor-only; run this installer under WSL2"}
	}
	if tool.Binary != "" {
		if path, err := runner.LookPath(tool.Binary); err == nil && path != "" {
			return InstallStep{ToolID: tool.ID, Status: InstallSkipped, Message: "already installed at " + path}
		}
	}

	command, err := installCommand(tool, installDir, runner)
	if err != nil {
		return InstallStep{ToolID: tool.ID, Status: InstallFailed, Message: err.Error()}
	}
	if dryRun {
		return InstallStep{ToolID: tool.ID, Status: InstallPlanned, Command: command}
	}

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return InstallStep{ToolID: tool.ID, Status: InstallFailed, Message: fmt.Sprintf("create install dir: %v", err), Command: command}
	}
	output, err := runner.Run(command)
	if err != nil {
		fallback, fallbackErr := sourceFallbackCommand(tool, installDir)
		if fallbackErr != nil {
			return InstallStep{ToolID: tool.ID, Status: InstallFailed, Message: err.Error(), Command: command, Output: output}
		}
		fallbackOutput, fallbackRunErr := runner.Run(fallback)
		if fallbackRunErr != nil {
			return InstallStep{
				ToolID:  tool.ID,
				Status:  InstallFailed,
				Message: fmt.Sprintf("%v; source fallback failed: %v", err, fallbackRunErr),
				Command: fallback,
				Output:  fallbackOutput,
			}
		}
		return InstallStep{
			ToolID:  tool.ID,
			Status:  InstallInstalled,
			Message: "installed from source after primary installer failed",
			Command: fallback,
			Output:  fallbackOutput,
		}
	}
	return InstallStep{ToolID: tool.ID, Status: InstallInstalled, Command: command, Output: output}
}

func installCommand(tool Tool, installDir string, runner Runner) (Command, error) {
	switch tool.InstallKind {
	case InstallScript:
		if tool.InstallURL == "" {
			return Command{}, fmt.Errorf("%s has no install URL", tool.ID)
		}
		return Command{
			Name: "bash",
			Args: []string{"-c", `curl -fsSL "$LIZA_TOOL_INSTALL_URL" | bash`},
			Env: map[string]string{
				"INSTALL_DIR":           installDir,
				"LIZA_TOOL_INSTALL_URL": tool.InstallURL,
			},
		}, nil
	case InstallGo:
		return Command{
			Name: "go",
			Args: []string{"install", tool.GoPackage},
			Env:  map[string]string{"GOBIN": installDir},
		}, nil
	case InstallNPM:
		return Command{Name: "npm", Args: []string{"install", "-g", tool.NPMPackage}}, nil
	case InstallUVTool:
		return Command{Name: "uv", Args: []string{"tool", "install", tool.UVPackage}}, nil
	case InstallPackage:
		return packageInstallCommand(tool.PackageName, runner)
	default:
		return Command{}, fmt.Errorf("%s has unsupported install kind %q", tool.ID, tool.InstallKind)
	}
}

func sourceFallbackCommand(tool Tool, installDir string) (Command, error) {
	if tool.SourceRepo == "" || tool.SourcePackage == "" {
		return Command{}, fmt.Errorf("%s has no source fallback", tool.ID)
	}
	return Command{
		Name: "bash",
		Args: []string{"-c", strings.Join([]string{
			`set -euo pipefail`,
			`tmp="$(mktemp -d)"`,
			`trap 'rm -rf "$tmp"' EXIT`,
			`git clone --depth 1 "$LIZA_TOOL_SOURCE_REPO" "$tmp/src"`,
			`cd "$tmp/src"`,
			`GOBIN="$INSTALL_DIR" go install "$LIZA_TOOL_SOURCE_PACKAGE"`,
		}, "; ")},
		Env: map[string]string{
			"INSTALL_DIR":              installDir,
			"LIZA_TOOL_SOURCE_REPO":    tool.SourceRepo,
			"LIZA_TOOL_SOURCE_PACKAGE": tool.SourcePackage,
		},
	}, nil
}

func packageInstallCommand(packageName string, runner Runner) (Command, error) {
	if packageName == "" {
		return Command{}, fmt.Errorf("missing package name")
	}
	if strings.HasPrefix(packageName, "http://") || strings.HasPrefix(packageName, "https://") {
		if path, err := runner.LookPath("brew"); err == nil && path != "" {
			return Command{Name: "brew", Args: []string{"install", "--formula", packageName}}, nil
		}
		return Command{}, fmt.Errorf("homebrew is required to install formula %s", packageName)
	}
	packageManagers := []struct {
		binary string
		cmd    Command
	}{
		{"brew", Command{Name: "brew", Args: []string{"install", packageName}}},
		{"apt-get", Command{Name: "sh", Args: []string{"-c", `sudo apt-get update && sudo apt-get install -y "$LIZA_TOOL_PACKAGE"`}, Env: map[string]string{"LIZA_TOOL_PACKAGE": packageName}}},
		{"dnf", Command{Name: "sudo", Args: []string{"dnf", "install", "-y", packageName}}},
		{"yum", Command{Name: "sudo", Args: []string{"yum", "install", "-y", packageName}}},
		{"pacman", Command{Name: "sudo", Args: []string{"pacman", "-Sy", "--needed", packageName}}},
		{"zypper", Command{Name: "sudo", Args: []string{"zypper", "install", "-y", packageName}}},
	}
	for _, candidate := range packageManagers {
		if path, err := runner.LookPath(candidate.binary); err == nil && path != "" {
			return candidate.cmd, nil
		}
	}
	return Command{}, fmt.Errorf("no supported package manager found for %s (checked brew, apt-get, dnf, yum, pacman, zypper)", packageName)
}

func resolveInstallDir(raw string) (string, error) {
	if raw == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		raw = filepath.Join(home, ".local", "bin")
	}
	expanded := raw
	if strings.HasPrefix(expanded, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		expanded = filepath.Join(home, strings.TrimPrefix(expanded, "~/"))
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve install dir: %w", err)
	}
	return abs, nil
}

func SortInstallSteps(steps []InstallStep) {
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].ToolID < steps[j].ToolID
	})
}
