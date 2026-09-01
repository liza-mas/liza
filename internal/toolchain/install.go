package toolchain

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// toolEnvName names an environment variable the generated install commands set
// and their scripts read. The pair is closed - nothing outside this package
// writes or reads these, so no legacy alias is needed - but the names appear in
// every command the CLI prints, so they carry the configured brand rather than
// a hardcoded one.
func toolEnvName(suffix string) string {
	return brand.EnvName("TOOL_" + suffix)
}

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
	InstallPlanned   InstallStatus = "planned"
	InstallSkipped   InstallStatus = "skipped"
	InstallInstalled InstallStatus = "installed"
	InstallFailed    InstallStatus = "failed"
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
	if err := installResultError(result.Steps); err != nil {
		return result, err
	}
	return result, nil
}

func installOne(tool Tool, installDir, goos string, dryRun bool, runner Runner) InstallStep {
	if tool.InstallKind == InstallManualOnly {
		return InstallStep{ToolID: tool.ID, Status: InstallSkipped, Message: tool.ManualNote}
	}
	if tool.Binary != "" {
		if path, err := runner.LookPath(tool.Binary); err == nil && path != "" {
			return InstallStep{ToolID: tool.ID, Status: InstallSkipped, Message: "already installed at " + path}
		}
	}

	command, err := installCommand(tool, installDir, runner, goos)
	if errors.Is(err, errNoPackagePath) {
		// Nothing automated can install it here. Say what would, and let doctor
		// keep reporting it missing until someone does.
		message := "no package manager on this host carries " + tool.ID
		if tool.ManualInstallNote != "" {
			message += "; " + tool.ManualInstallNote
		}
		return InstallStep{ToolID: tool.ID, Status: InstallSkipped, Message: message}
	}
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
		return verifyInstalled(InstallStep{
			ToolID:  tool.ID,
			Status:  InstallInstalled,
			Message: "installed from source after primary installer failed",
			Command: fallback,
			Output:  fallbackOutput,
		}, tool, installDir, runner, goos)
	}
	return verifyInstalled(InstallStep{ToolID: tool.ID, Status: InstallInstalled, Command: command, Output: output}, tool, installDir, runner, goos)
}

// verifyInstalled confirms the tool is reachable now that its installer claims
// success, and repairs the one case where it reliably is not.
//
// An installer that builds from source with `go build -o <dir>/<name>` writes
// exactly that name, and Go honours it — so on Windows the result carries no
// extension and nothing resolves it through PATHEXT: not exec.LookPath, not
// cmd, not PowerShell. The install reports success and every later invocation
// fails, including toolchain doctor, which reports the tool missing seconds
// after it was installed. Renaming it is safe: the file sits in the directory
// this command was told to manage.
//
// Anything still unresolved afterwards is reported as a failure rather than
// left to be discovered later.
func verifyInstalled(step InstallStep, tool Tool, installDir string, runner Runner, goos string) InstallStep {
	if tool.Binary == "" {
		return step
	}

	// A package manager installs into its own prefix and extends PATH for
	// processes started afterwards, so neither the managed directory nor this
	// process's PATH can show the result: the environment was captured before
	// the install ran. Its exit code is the only evidence available here, and a
	// package manager is a trustworthy reporter.
	if tool.InstallKind == InstallPackage {
		step.Message = strings.TrimSpace(step.Message + " (installed by a package manager; start a new shell for PATH to include it)")
		return step
	}

	if goos == "windows" && !hasExecutableForm(tool, installDir) {
		// Only when nothing runnable exists. npm, for one, writes three files per
		// package — a POSIX shell wrapper with no extension plus .cmd and .ps1
		// shims — and the extensionless one is a script, not a binary. Renaming
		// it would put a shell script where PATHEXT looks for an executable
		// first, shadowing the .cmd that actually works.
		plain := filepath.Join(installDir, tool.Binary)
		if info, statErr := os.Stat(plain); statErr == nil && !info.IsDir() {
			suffixed := plain + ".exe"
			if renameErr := os.Rename(plain, suffixed); renameErr != nil {
				step.Status = InstallFailed
				step.Message = fmt.Sprintf("%s was installed as %s, which Windows cannot resolve through PATH, and renaming it failed: %v", tool.ID, plain, renameErr)
				return step
			}
			step.Message = strings.TrimSpace(step.Message + " (renamed to " + filepath.Base(suffixed) + " so PATH can resolve it)")
		}
	}

	// The binary is accounted for if it landed in the directory this command
	// manages, or if it is resolvable anywhere on PATH — a package manager puts
	// it under its own prefix, not here. Being absent from PATH is not a
	// failure: the install directory is wired in by `toolchain configure`, which
	// may not have run yet.
	if binaryInInstallDir(tool, installDir, goos) {
		return step
	}
	if path, err := runner.LookPath(tool.Binary); err == nil && path != "" {
		return step
	}
	step.Status = InstallFailed
	step.Message = strings.TrimSpace(fmt.Sprintf("%s installer reported success but produced no %s in %s and none is on PATH. %s",
		tool.ID, tool.Binary, installDir, step.Message))
	return step
}

func binaryInInstallDir(tool Tool, installDir, goos string) bool {
	if goos == "windows" {
		return hasExecutableForm(tool, installDir) || regularFileExists(filepath.Join(installDir, tool.Binary))
	}
	return regularFileExists(filepath.Join(installDir, tool.Binary))
}

// hasExecutableForm reports whether the install directory already holds a form
// of the tool that Windows will resolve through PATHEXT.
func hasExecutableForm(tool Tool, installDir string) bool {
	for _, ext := range []string{".exe", ".cmd", ".bat", ".com"} {
		if regularFileExists(filepath.Join(installDir, tool.Binary+ext)) {
			return true
		}
	}
	return false
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func installCommand(tool Tool, installDir string, runner Runner, goos string) (Command, error) {
	switch tool.InstallKind {
	case InstallScript:
		// A tool whose install script rejects Windows may still publish a
		// native build. Prefer that over a script that cannot run.
		if goos == "windows" && tool.WindowsArchiveURL != "" {
			return windowsArchiveCommand(tool, installDir), nil
		}
		if tool.InstallURL == "" {
			return Command{}, fmt.Errorf("%s has no install URL", tool.ID)
		}
		installURLEnv := toolEnvName("INSTALL_URL")
		env := map[string]string{
			installURLEnv: tool.InstallURL,
		}
		for _, name := range installDirEnvNames(tool) {
			env[name] = installDir
		}
		return Command{
			Name: "bash",
			Args: []string{"-c", `curl -fsSL "$` + installURLEnv + `" | bash`},
			Env:  env,
		}, nil
	case InstallGo:
		return Command{
			Name: "go",
			Args: []string{"install", tool.GoPackage},
			Env:  map[string]string{"GOBIN": installDir},
		}, nil
	case InstallNPM:
		prefix, err := npmPrefixForBinDir(installDir, goos)
		if err != nil {
			return Command{}, err
		}
		return Command{Name: "npm", Args: []string{"install", "-g", tool.NPMPackage}, Env: map[string]string{"NPM_CONFIG_PREFIX": prefix}}, nil
	case InstallUVTool:
		// --force because this step is only reached when the binary is missing:
		// uv considers a tool installed from its own registry and would report
		// success without recreating the executable a previous install placed
		// somewhere else.
		return Command{Name: "uv", Args: []string{"tool", "install", "--force", tool.UVPackage}, Env: map[string]string{"UV_TOOL_BIN_DIR": installDir}}, nil
	case InstallPackage:
		return packageInstallCommand(tool, runner)
	default:
		return Command{}, fmt.Errorf("%s has unsupported install kind %q", tool.ID, tool.InstallKind)
	}
}

// windowsArchiveCommand downloads a release archive and unpacks the binary into
// the install directory, through PowerShell rather than curl and tar so it works
// on a host with no Unix tooling at all.
func windowsArchiveCommand(tool Tool, installDir string) Command {
	archiveURLEnv := toolEnvName("ARCHIVE_URL")
	binaryEnv := toolEnvName("BINARY")
	installDirEnv := toolEnvName("INSTALL_DIR")
	script := strings.Join([]string{
		`$ErrorActionPreference = 'Stop'`,
		`$work = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString('N'))`,
		`New-Item -ItemType Directory -Path $work | Out-Null`,
		`try {`,
		`  $archive = Join-Path $work 'archive.zip'`,
		`  Invoke-WebRequest -Uri $env:` + archiveURLEnv + ` -OutFile $archive -UseBasicParsing`,
		`  Expand-Archive -Path $archive -DestinationPath $work -Force`,
		`  $binary = Get-ChildItem -Path $work -Recurse -Filter ($env:` + binaryEnv + ` + '.exe') | Select-Object -First 1`,
		`  if (-not $binary) { throw ('archive does not contain ' + $env:` + binaryEnv + ` + '.exe') }`,
		`  New-Item -ItemType Directory -Path $env:` + installDirEnv + ` -Force | Out-Null`,
		`  Move-Item -Path $binary.FullName -Destination (Join-Path $env:` + installDirEnv + ` ($env:` + binaryEnv + ` + '.exe')) -Force`,
		`} finally { Remove-Item -Path $work -Recurse -Force -ErrorAction SilentlyContinue }`,
	}, "; ")
	return Command{
		Name: "powershell",
		Args: []string{"-NoProfile", "-NonInteractive", "-Command", script},
		Env: map[string]string{
			archiveURLEnv: tool.WindowsArchiveURL,
			binaryEnv:     tool.Binary,
			installDirEnv: installDir,
		},
	}
}

func sourceFallbackCommand(tool Tool, installDir string) (Command, error) {
	if tool.SourceRepo == "" || tool.SourcePackage == "" {
		return Command{}, fmt.Errorf("%s has no source fallback", tool.ID)
	}
	sourceRepoEnv := toolEnvName("SOURCE_REPO")
	sourcePackageEnv := toolEnvName("SOURCE_PACKAGE")
	return Command{
		Name: "bash",
		Args: []string{"-c", strings.Join([]string{
			`set -euo pipefail`,
			`tmp="$(mktemp -d)"`,
			`trap 'rm -rf "$tmp"' EXIT`,
			`git clone --depth 1 "$` + sourceRepoEnv + `" "$tmp/src"`,
			`cd "$tmp/src"`,
			`GOBIN="$INSTALL_DIR" go install "$` + sourcePackageEnv + `"`,
		}, "; ")},
		Env: map[string]string{
			"INSTALL_DIR":    installDir,
			sourceRepoEnv:    tool.SourceRepo,
			sourcePackageEnv: tool.SourcePackage,
		},
	}, nil
}

func installDirEnvNames(tool Tool) []string {
	if len(tool.InstallDirEnv) > 0 {
		return tool.InstallDirEnv
	}
	return []string{"INSTALL_DIR"}
}

func installResultError(steps []InstallStep) error {
	var failed []string
	for _, step := range steps {
		if step.Status == InstallFailed {
			failed = append(failed, fmt.Sprintf("%s:%s", step.ToolID, step.Status))
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("toolchain install incomplete: %s", strings.Join(failed, ", "))
}

// npmPrefixForBinDir maps the directory binaries should land in to the prefix
// npm needs to be given.
//
// npm writes global executables to <prefix>/bin on Unix but to <prefix> itself
// on Windows, with no bin subdirectory. Passing the parent on Windows therefore
// scatters the shims one level above the managed directory, where nothing on
// PATH finds them.
func npmPrefixForBinDir(binDir, goos string) (string, error) {
	if goos == "windows" {
		return binDir, nil
	}
	if filepath.Base(binDir) != "bin" {
		return "", fmt.Errorf("npm global installs require install dir ending in /bin, got %s", binDir)
	}
	return filepath.Dir(binDir), nil
}

// errNoPackagePath reports that no package manager on this host can install the
// tool. It is distinct from a failure: the tool may still be installable by
// hand, and the caller turns it into an actionable message rather than an error.
var errNoPackagePath = errors.New("no package manager path")

func packageInstallCommand(tool Tool, runner Runner) (Command, error) {
	if tool.PackageName == "" {
		return Command{}, fmt.Errorf("missing package name")
	}
	if strings.HasPrefix(tool.PackageName, "http://") || strings.HasPrefix(tool.PackageName, "https://") {
		return Command{}, fmt.Errorf("URL package installs are not supported: %s", tool.PackageName)
	}

	// Windows managers come first so they win on a host that has both, which is
	// the case under Git Bash with scoop or a WSL-adjacent brew on PATH.
	packageManagers := []struct {
		binary string
		build  func(name string) Command
	}{
		{"winget", func(name string) Command {
			return Command{Name: "winget", Args: []string{
				"install", "--exact", "--id", name,
				"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity",
			}}
		}},
		{"scoop", func(name string) Command { return Command{Name: "scoop", Args: []string{"install", name}} }},
		{"choco", func(name string) Command {
			return Command{Name: "choco", Args: []string{"install", name, "-y", "--no-progress"}}
		}},
		{"brew", func(name string) Command { return Command{Name: "brew", Args: []string{"install", name}} }},
		{"apt-get", func(name string) Command {
			packageEnv := toolEnvName("PACKAGE")
			return Command{Name: "sh", Args: []string{"-c", `sudo apt-get update && sudo apt-get install -y "$` + packageEnv + `"`}, Env: map[string]string{packageEnv: name}}
		}},
		{"dnf", func(name string) Command { return Command{Name: "sudo", Args: []string{"dnf", "install", "-y", name}} }},
		{"yum", func(name string) Command { return Command{Name: "sudo", Args: []string{"yum", "install", "-y", name}} }},
		{"pacman", func(name string) Command {
			return Command{Name: "sudo", Args: []string{"pacman", "-Sy", "--needed", name}}
		}},
		{"zypper", func(name string) Command {
			return Command{Name: "sudo", Args: []string{"zypper", "install", "-y", name}}
		}},
	}

	var names []string
	// Set when a manager was present but named no package we know: that is a
	// different failure from finding no manager at all, and the caller reports
	// it differently.
	unknownPackageName := false
	for _, candidate := range packageManagers {
		names = append(names, candidate.binary)
		path, err := runner.LookPath(candidate.binary)
		if err != nil || path == "" {
			continue
		}
		packageName, ok := packageNameFor(tool, candidate.binary)
		if !ok {
			// The manager is present but carries this tool under no name we
			// know. Installing PackageName blindly would install whatever else
			// answers to it, so this manager is skipped rather than guessed at
			// — and the next one still gets its turn, since a catalog entry
			// naming a scoop package but no winget one is ordinary.
			unknownPackageName = true
			continue
		}
		return candidate.build(packageName), nil
	}
	if unknownPackageName {
		return Command{}, errNoPackagePath
	}
	return Command{}, fmt.Errorf("no supported package manager found for %s (checked %s)", tool.PackageName, strings.Join(names, ", "))
}

// packageNameFor resolves the package identifier for one manager.
//
// Unix managers share the plain name. The Windows managers identify packages by
// publisher-qualified IDs, so they are only used when the catalog states the
// identifier explicitly.
func packageNameFor(tool Tool, manager string) (string, bool) {
	if name, ok := tool.PackageNamesByManager[manager]; ok && name != "" {
		return name, true
	}
	switch manager {
	case "winget", "scoop", "choco":
		return "", false
	default:
		return tool.PackageName, true
	}
}

func resolveInstallDir(raw string) (string, error) {
	abs, err := resolveHomeDir(raw, filepath.Join(".local", "bin"))
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
