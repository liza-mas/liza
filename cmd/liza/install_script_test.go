package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInstallScriptAcceptsDerivedBrandInputsInHelp(t *testing.T) {
	out, err := runInstallScriptHelp(t,
		"BRAND_NAME_LOWER=acme-agent",
		"BRAND_REPO=acme/agent",
	)
	if err != nil {
		t.Fatalf("install.sh --help failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Acme Agent Installation Script",
		"https://raw.githubusercontent.com/acme/agent/main/install.sh",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install.sh --help missing %q:\n%s", want, out)
		}
	}
}

func TestInstallScriptRejectsInvalidBrandInputs(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "lower", env: "BRAND_NAME_LOWER=Acme", want: "BRAND_NAME_LOWER invalid"},
		{name: "repo", env: "BRAND_REPO=https://github.com/acme/agent", want: "BRAND_REPO invalid"},
		{name: "global dir", env: "BRAND_GLOBAL_DIRNAME=../acme", want: "BRAND_GLOBAL_DIRNAME invalid"},
		{name: "source dir", env: "BRAND_SOURCE_DIR_NAME=../acme", want: "BRAND_SOURCE_DIR_NAME invalid"},
		{name: "release url", env: "BRAND_RELEASE_BASE_URL=http://example.com/acme", want: "BRAND_RELEASE_BASE_URL invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := runInstallScriptHelp(t, tt.env)
			if err == nil {
				t.Fatalf("install.sh --help succeeded, want validation failure\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("install.sh output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

// TestInstallScriptRefusesWindowsRelease covers the refusal itself and the fact
// that it is visible. detect_platform's stdout is consumed by the caller's
// command substitution, so a message printed there never reaches the process
// output this test reads.
func TestInstallScriptRefusesWindowsRelease(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uname reports MINGW/MSYS/CYGWIN only when bash runs on Windows")
	}

	out, err := runInstallScript(t, nil)
	if err == nil {
		t.Fatalf("install.sh succeeded, want a refusal:\n%s", out)
	}
	for _, want := range []string{
		"does not install Windows releases",
		"install.ps1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install.sh output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Downloading from") {
		t.Fatalf("install.sh reached the download before refusing:\n%s", out)
	}
}

func TestInstallScriptReportsUnsupportedPlatformFromCommandSubstitution(t *testing.T) {
	tests := []struct {
		name string
		os   string
		arch string
		want string
	}{
		{name: "operating system", os: "Plan9", arch: "amd64", want: "Unsupported operating system: Plan9"},
		{name: "architecture", os: "Linux", arch: "sparc64", want: "Unsupported architecture: sparc64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bashEnv := filepath.Join(t.TempDir(), "bash-env")
			script := "uname() {\n  if [ \"$1\" = \"-s\" ]; then printf '%s\\n' '" + tt.os + "'; else printf '%s\\n' '" + tt.arch + "'; fi\n}\n"
			if err := os.WriteFile(bashEnv, []byte(script), 0o600); err != nil {
				t.Fatalf("write bash environment: %v", err)
			}
			t.Setenv("BASH_ENV", filepath.ToSlash(bashEnv))

			out, err := runInstallScript(t, nil)
			if err == nil {
				t.Fatalf("install.sh succeeded, want platform refusal:\n%s", out)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("install.sh output missing %q:\n%s", tt.want, out)
			}
		})
	}
}

func TestPowerShellInstallerDerivesBinaryNameFromBrandNameLower(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell installer behavior is exercised by Windows CI")
	}
	powerShell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skipf("Windows PowerShell is unavailable: %v", err)
	}

	repoRoot := findRepoRootForInstallScript(t)
	installerPath := filepath.Join(repoRoot, "install.ps1")
	harnessPath := filepath.Join(t.TempDir(), "test-brand-derivation.ps1")
	harness := `param([string]$InstallerPath)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$env:BRAND_NAME_LOWER = 'acme-agent'
$env:BRAND_BINARY_NAME = $null

$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    $InstallerPath,
    [ref]$tokens,
    [ref]$parseErrors
)
if ($parseErrors -and $parseErrors.Count -ne 0) {
    throw "Could not parse installer: $($parseErrors[0].Message)"
}

$wanted = @('NameLower', 'BinaryName')
foreach ($statement in $ast.EndBlock.Statements) {
    if ($statement -isnot [System.Management.Automation.Language.AssignmentStatementAst]) {
        continue
    }
    $name = $statement.Left.VariablePath.UserPath
    if ($wanted -contains $name) {
        Invoke-Expression $statement.Extent.Text
    }
}

if ($BinaryName -ne 'acme-agent') {
    throw "BinaryName was '$BinaryName', want 'acme-agent'."
}
Write-Output $BinaryName
`
	if err := os.WriteFile(harnessPath, []byte(harness), 0o600); err != nil {
		t.Fatalf("write PowerShell test harness: %v", err)
	}

	cmd := exec.Command(
		powerShell,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", harnessPath,
		"-InstallerPath", installerPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exercise PowerShell brand derivation: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "acme-agent") {
		t.Fatalf("PowerShell output missing derived binary name:\n%s", out)
	}
}

func TestPowerShellInstallerHandlesMissingLatestTag(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell installer behavior is exercised by Windows CI")
	}
	powerShell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skipf("Windows PowerShell is unavailable: %v", err)
	}

	repoRoot := findRepoRootForInstallScript(t)
	installerPath := filepath.Join(repoRoot, "install.ps1")
	harnessPath := filepath.Join(t.TempDir(), "test-latest-version.ps1")
	harness := `param([string]$InstallerPath)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    $InstallerPath,
    [ref]$tokens,
    [ref]$parseErrors
)
if ($parseErrors -and $parseErrors.Count -ne 0) {
    throw "Could not parse installer: $($parseErrors[0].Message)"
}
$functionAst = $ast.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Get-LatestVersion'
}, $true)
if (-not $functionAst) {
    throw 'Get-LatestVersion was not found in the installer.'
}
Invoke-Expression $functionAst.Extent.Text

$script:Release = $null
function Invoke-RestMethod {
    param([string]$Uri, [switch]$UseBasicParsing)
    return $script:Release
}

$expectedError = 'Could not determine the latest release of test/repo.'
$Repo = 'test/repo'
$script:Release = [pscustomobject]@{}
try {
    Get-LatestVersion | Out-Null
    throw 'Get-LatestVersion unexpectedly accepted a release without tag_name.'
} catch {
    if ($_.Exception.Message -ne $expectedError) {
        throw "Unexpected missing-tag error: $($_.Exception.Message)"
    }
}

$script:Release = [pscustomobject]@{ tag_name = 'v1.2.3' }
$actual = Get-LatestVersion
if ($actual -ne 'v1.2.3') {
    throw "Get-LatestVersion returned '$actual', want 'v1.2.3'."
}
Write-Output $expectedError
`
	if err := os.WriteFile(harnessPath, []byte(harness), 0o600); err != nil {
		t.Fatalf("write PowerShell test harness: %v", err)
	}

	cmd := exec.Command(
		powerShell,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", harnessPath,
		"-InstallerPath", installerPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exercise Get-LatestVersion: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Could not determine the latest release of test/repo.") {
		t.Fatalf("PowerShell output missing the friendly error:\n%s", out)
	}
}

func TestPowerShellInstallerManagesUserPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell installer behavior is exercised by Windows CI")
	}
	powerShell, err := exec.LookPath("powershell")
	if err != nil {
		t.Skipf("Windows PowerShell is unavailable: %v", err)
	}

	repoRoot := findRepoRootForInstallScript(t)
	installerPath := filepath.Join(repoRoot, "install.ps1")
	harnessPath := filepath.Join(t.TempDir(), "test-path-management.ps1")
	harness := `param([string]$InstallerPath)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$tokens = $null
$parseErrors = $null
$ast = [System.Management.Automation.Language.Parser]::ParseFile(
    $InstallerPath,
    [ref]$tokens,
    [ref]$parseErrors
)
if ($parseErrors -and $parseErrors.Count -ne 0) {
    throw "Could not parse installer: $($parseErrors[0].Message)"
}
$functionAst = $ast.Find({
    param($node)
    $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and
        $node.Name -eq 'Update-InstallDirOnPath'
}, $true)
if (-not $functionAst) {
    throw 'Update-InstallDirOnPath was not found in the installer.'
}
Invoke-Expression $functionAst.Extent.Text

$default = Join-Path $env:LOCALAPPDATA 'Programs\example-tool'
$target = 'C:\tools\example-tool'
$expander = {
    param([string]$entry)
    [Environment]::ExpandEnvironmentVariables($entry)
}

function Assert-Path([string]$name, [string]$actual, [string]$expected) {
    if ($actual -ne $expected) {
        throw "$name failed: got '$actual', want '$expected'"
    }
}

# absent -> appended once
Assert-Path 'add' (Update-InstallDirOnPath -InstallDir $target -UserPath 'C:\Windows;C:\bin' -DefaultDir $default -Expander $expander) 'C:\Windows;C:\bin;C:\tools\example-tool'

# present -> unchanged (idempotent)
Assert-Path 'idempotent' (Update-InstallDirOnPath -InstallDir $target -UserPath "C:\Windows;$target;C:\bin" -DefaultDir $default -Expander $expander) "C:\Windows;$target;C:\bin"

# duplicated -> collapsed to one
Assert-Path 'dedupe' (Update-InstallDirOnPath -InstallDir $target -UserPath "$target;C:\Windows;$target\" -DefaultDir $default -Expander $expander) "$target;C:\Windows"

# stale default entry -> removed when install dir moved
Assert-Path 'stale-default' (Update-InstallDirOnPath -InstallDir $target -UserPath "C:\Windows;$default;C:\bin" -DefaultDir $default -Expander $expander) "C:\Windows;C:\bin;$target"

# default IS the install dir -> kept, not treated as stale
Assert-Path 'default-is-target' (Update-InstallDirOnPath -InstallDir $default -UserPath "C:\Windows;$default" -DefaultDir $default -Expander $expander) "C:\Windows;$default"

# empty user PATH -> just the install dir
Assert-Path 'empty' (Update-InstallDirOnPath -InstallDir $target -UserPath '' -DefaultDir $default -Expander $expander) $target

# unexpanded %VAR% entries survive the merge verbatim, keeping REG_EXPAND_SZ
Assert-Path 'expand-sz-preserved' (Update-InstallDirOnPath -InstallDir $target -UserPath "C:\Windows;%USERPROFILE%\AppData\Local\Microsoft\WindowsApps" -DefaultDir $default -Expander $expander) "C:\Windows;%USERPROFILE%\AppData\Local\Microsoft\WindowsApps;$target"

# an unexpanded %LOCALAPPDATA% entry that resolves to the default dir is
# recognized after expansion and treated like an expanded stale entry
Assert-Path 'expand-locapappdata-recognized' (Update-InstallDirOnPath -InstallDir $target -UserPath "C:\Windows;%LOCALAPPDATA%\Programs\example-tool;C:\bin" -DefaultDir $default -Expander $expander) "C:\Windows;C:\bin;$target"

Write-Output 'path-cases-ok'
`
	if err := os.WriteFile(harnessPath, []byte(harness), 0o600); err != nil {
		t.Fatalf("write PowerShell test harness: %v", err)
	}

	cmd := exec.Command(
		powerShell,
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", harnessPath,
		"-InstallerPath", installerPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("exercise Update-InstallDirOnPath: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "path-cases-ok") {
		t.Fatalf("PowerShell output missing success marker:\n%s", out)
	}
}

// TestPowerShellInstallerPreservesRegExpandSzUserPath is the macOS/Linux
// counterpart to the Windows-CI harness above: pwsh is unavailable here, so it
// statically checks that the installer keeps a REG_EXPAND_SZ user PATH intact —
// the marker strings below, not the behavior, are what this test asserts. The
// raw registry read (DoNotExpandEnvironmentNames) and the ExpandString write are
// what stop a stock user PATH — which holds entries like
// %USERPROFILE%\AppData\Local\Microsoft\WindowsApps — from being flattened to
// expanded values. The injected expander keeps Update-InstallDirOnPath pure
// while comparing entries by expansion, and the raw $trimmed form is what is
// emitted, so an unexpanded %VARIABLE% entry survives the merge verbatim.
func TestPowerShellInstallerPreservesRegExpandSzUserPath(t *testing.T) {
	repoRoot := findRepoRootForInstallScript(t)
	script, err := os.ReadFile(filepath.Join(repoRoot, "install.ps1"))
	if err != nil {
		t.Fatalf("read install.ps1: %v", err)
	}
	text := string(script)

	for _, marker := range []string{
		"[System.Func[string,string]]$Expander",
		"$Expander.Invoke(",
		"$kept += $trimmed",
		"[Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames",
		"[Microsoft.Win32.RegistryValueKind]::ExpandString",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("install.ps1 missing %q (REG_EXPAND_SZ preservation)", marker)
		}
	}
}

func runInstallScriptHelp(t *testing.T, env ...string) (string, error) {
	t.Helper()
	return runInstallScript(t, []string{"--help"}, env...)
}

func runInstallScript(t *testing.T, args []string, env ...string) (string, error) {
	t.Helper()
	bashPath := testhelpers.ResolveBashForScripts(t)
	repoRoot := findRepoRootForInstallScript(t)
	// Bash (Git for Windows) treats backslashes as escape characters, so a
	// Windows path like C:\Users\...\install.sh gets mangled. Pass the script
	// path with forward slashes, which bash accepts on every platform.
	scriptPath := filepath.ToSlash(filepath.Join(repoRoot, "install.sh"))
	cmd := exec.Command(bashPath, append([]string{scriptPath}, args...)...)
	cmd.Env = append(os.Environ(), "INSTALL_DIR="+t.TempDir())
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func findRepoRootForInstallScript(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}
