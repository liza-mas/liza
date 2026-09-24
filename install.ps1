<#
.SYNOPSIS
    Installs the CLI on Windows from a published release.

.DESCRIPTION
    The PowerShell counterpart to install.sh, covering the release-download path
    only. Building from source is not reproduced here: it needs Go and GNU make,
    and anyone with both can run `make install` directly.

    Windows releases ship as a zip rather than a tar.gz, because tar is not a
    given on a Windows host and Expand-Archive reads zip only.

    Which product this installs is decided by the BRAND_* environment
    variables read below, so this help stays neutral: naming one here would
    describe the wrong thing under any other brand.

.PARAMETER Version
    Release tag to install, e.g. v1.2.3. Defaults to the latest release.

.PARAMETER InstallDir
    Where to place the executable. Defaults to a directory named after the
    binary under %LOCALAPPDATA%\Programs, which needs no elevation.

.EXAMPLE
    irm https://raw.githubusercontent.com/liza-mas/liza/main/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v1.2.3 -InstallDir C:\tools\cli
#>

[CmdletBinding()]
param(
    [string]$Version = $env:VERSION,
    [string]$InstallDir = $env:INSTALL_DIR
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Brand values are substituted at release time the same way install.sh reads
# them from the environment.
$NameLower = if ($env:BRAND_NAME_LOWER) { $env:BRAND_NAME_LOWER } else { 'liza' }
$Repo = if ($env:BRAND_INSTALL_REPO) { $env:BRAND_INSTALL_REPO }
        elseif ($env:BRAND_REPO) { $env:BRAND_REPO }
        else { 'liza-mas/liza' }
$BinaryName = if ($env:BRAND_BINARY_NAME) { $env:BRAND_BINARY_NAME } else { $NameLower }
$ArchivePrefix = if ($env:BRAND_ARCHIVE_PREFIX) { $env:BRAND_ARCHIVE_PREFIX } else { $BinaryName }
$ReleaseRepo = if ($env:BRAND_RELEASE_REPO) { $env:BRAND_RELEASE_REPO } else { $Repo }
$ReleaseBaseUrl = if ($env:BRAND_RELEASE_BASE_URL) { $env:BRAND_RELEASE_BASE_URL }
                  else { "https://github.com/$ReleaseRepo/releases/download" }

if (-not $InstallDir) {
    $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\$BinaryName"
}

function Get-LatestVersion {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -UseBasicParsing
    $tagProperty = $release.PSObject.Properties['tag_name']
    if (-not $tagProperty -or -not $tagProperty.Value) {
        throw "Could not determine the latest release of $Repo."
    }
    return [string]$tagProperty.Value
}

function Get-ReleaseArchitecture {
    switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        default { throw "Unsupported processor architecture: $($env:PROCESSOR_ARCHITECTURE)" }
    }
}

# Kept pure (no registry, environment, or session reads/writes) so Windows CI
# can exercise the merge rules without mutating the runner. The caller injects
# the expander so the function can decide whether an unexpanded %VARIABLE%
# entry resolves to the install dir without freezing it to its current value.
#
# Entries are compared after expansion (so %LOCALAPPDATA%\Programs\<binary>
# still matches an expanded $InstallDir) but emitted verbatim in the output, so
# an existing REG_EXPAND_SZ user PATH keeps its raw %VARIABLE% entries.
function Update-InstallDirOnPath {
    param(
        [Parameter(Mandatory = $true)][string]$InstallDir,
        [Parameter(Mandatory = $true)][AllowEmptyString()][string]$UserPath,
        [Parameter(Mandatory = $true)][string]$DefaultDir,
        [Parameter(Mandatory = $true)][System.Func[string,string]]$Expander
    )

    if (-not $UserPath) { return $InstallDir }

    $targetNorm = $Expander.Invoke($InstallDir).Trim().TrimEnd('\', '/')
    $defaultNorm = $Expander.Invoke($DefaultDir).Trim().TrimEnd('\', '/')

    $kept = @()
    $seenTarget = $false
    foreach ($entry in ($UserPath -split ';')) {
        $trimmed = $entry.Trim()
        if ($trimmed -eq '') { continue }
        $norm = $Expander.Invoke($trimmed).TrimEnd('\', '/')
        if ($norm -eq $targetNorm) {
            if ($seenTarget) { continue }
            $seenTarget = $true
        }
        elseif ($norm -eq $defaultNorm) {
            continue
        }
        $kept += $trimmed
    }
    if (-not $seenTarget) { $kept += $InstallDir }
    return ($kept -join ';')
}

if (-not $Version) { $Version = Get-LatestVersion }
$versionBare = $Version -replace '^v', ''
$arch = Get-ReleaseArchitecture
$archiveName = "$ArchivePrefix-$versionBare-windows-$arch.zip"
$archiveUrl = "$ReleaseBaseUrl/$Version/$archiveName"
$checksumsUrl = "$ReleaseBaseUrl/$Version/checksums.txt"

Write-Host "Installing $BinaryName $Version (windows/$arch)"
Write-Host "  Archive:           $archiveUrl"
Write-Host "  Install directory: $InstallDir"

$work = Join-Path ([System.IO.Path]::GetTempPath()) ("$BinaryName-install-" + [System.Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $work | Out-Null
try {
    $archivePath = Join-Path $work $archiveName
    Invoke-WebRequest -Uri $archiveUrl -OutFile $archivePath -UseBasicParsing

    # Verify against the published checksums, as install.sh does. A release
    # without a checksums.txt is a broken release, not a reason to proceed.
    $checksums = (Invoke-WebRequest -Uri $checksumsUrl -UseBasicParsing).Content
    $expected = $null
    foreach ($line in $checksums -split "`n") {
        $fields = ($line.Trim() -split '\s+')
        if ($fields.Count -ge 2 -and $fields[-1] -eq $archiveName) { $expected = $fields[0] }
    }
    if (-not $expected) {
        throw "No checksum published for $archiveName."
    }
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash
    if ($actual -ne $expected.ToUpperInvariant()) {
        throw "Checksum mismatch for ${archiveName}: got $actual, expected $expected."
    }

    Expand-Archive -Path $archivePath -DestinationPath $work -Force
    $binary = Get-ChildItem -Path $work -Recurse -Filter "$BinaryName.exe" | Select-Object -First 1
    if (-not $binary) {
        throw "The archive does not contain $BinaryName.exe."
    }

    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    $target = Join-Path $InstallDir "$BinaryName.exe"

    # A running executable cannot be overwritten on Windows, but it can be
    # renamed out of the way, the same move the self-updater performs. The
    # displaced copy is kept until the new binary is safely in place, so a
    # failed Move-Item below restores it rather than leaving no installation
    # at all.
    $displaced = $null
    if (Test-Path $target) {
        $displaced = "$target.old"
        Remove-Item -Path $displaced -Force -ErrorAction SilentlyContinue
        Rename-Item -Path $target -NewName "$BinaryName.exe.old" -Force
    }
    try {
        Move-Item -Path $binary.FullName -Destination $target -Force
    }
    catch {
        if ($displaced -and (Test-Path $displaced)) {
            Move-Item -Path $displaced -Destination $target -Force
        }
        throw
    }
    if ($displaced -and (Test-Path $displaced)) {
        Remove-Item -Path $displaced -Force -ErrorAction SilentlyContinue
    }
}
finally {
    Remove-Item -Path $work -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ""
Write-Host "Installed $target"

$defaultInstallDir = Join-Path $env:LOCALAPPDATA "Programs\$BinaryName"

# Read and write the user PATH through the registry so its raw, unexpanded
# entries survive. [System.Environment]::GetEnvironmentVariable returns every
# %VAR% already expanded, and SetEnvironmentVariable writes the result back as
# REG_SZ - flattening a stock REG_EXPAND_SZ user PATH and freezing entries like
# %JAVA_HOME%\bin to their current value. Reading with DoNotExpandEnvironmentNames
# and writing with RegistryValueKind.ExpandString keep the REG_EXPAND_SZ
# contract intact, so a later variable change is still followed.
$environmentKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
if (-not $environmentKey) {
    throw 'Could not open HKCU\Environment to update the user PATH.'
}
try {
    $rawUserPath = $environmentKey.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
    $userPath = if ($rawUserPath) { [string]$rawUserPath } else { '' }
    $newPath = Update-InstallDirOnPath -InstallDir $InstallDir -UserPath $userPath -DefaultDir $defaultInstallDir -Expander { param($entry) [Environment]::ExpandEnvironmentVariables($entry) }
    if ($newPath -ne $userPath) {
        $environmentKey.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)

        # [Environment]::SetEnvironmentVariable(..., 'User') broadcast
        # WM_SETTINGCHANGE as a side effect; a direct registry write does not.
        # Broadcast it so running Explorer instances reload their environment
        # block and terminals started afterwards inherit the change without a
        # sign-out. (HWND_BROADCAST, WM_SETTINGCHANGE, SMTO_ABORTIFHUNG.)
        Add-Type -Namespace PInvoke -Name User32 -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam, string lParam, uint fuFlags, uint uTimeout, out UIntPtr lpdwResult);
'@
        $broadcastResult = [UIntPtr]::Zero
        [PInvoke.User32]::SendMessageTimeout([IntPtr]0xFFFF, 0x1A, [UIntPtr]::Zero, 'Environment', 0x0002, 5000, [ref]$broadcastResult) | Out-Null

        Write-Host ""
        Write-Host "Updated your user PATH so it lists $InstallDir exactly once."
        Write-Host "Open a new terminal for other sessions to pick up the change."
    }
}
finally {
    $environmentKey.Close()
}

# Make the binary resolvable in this session too, so `irm ... | iex` users can
# run it without opening a new terminal.
$sessionHasDir = $false
foreach ($entry in ($env:PATH -split ';')) {
    if ($entry.Trim().TrimEnd('\', '/') -eq $InstallDir.Trim().TrimEnd('\', '/')) {
        $sessionHasDir = $true
        break
    }
}
if (-not $sessionHasDir) {
    $env:PATH = "$env:PATH;$InstallDir"
}

Write-Host ""
Write-Host "$BinaryName needs Git for Windows: the hooks it deploys are POSIX shell"
Write-Host "and run through bash. Make sure bash.exe from Git for Windows is on PATH,"
Write-Host "ahead of the WSL launcher in system32."
Write-Host ""
Write-Host "Next: run '$BinaryName setup' from a shell that can create symlinks -"
Write-Host "either with Developer Mode enabled or as Administrator."
