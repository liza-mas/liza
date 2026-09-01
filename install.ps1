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

$userPath = [System.Environment]::GetEnvironmentVariable('PATH', 'User')
if (($userPath -split ';') -notcontains $InstallDir) {
    Write-Host ""
    $addToPath = "[System.Environment]::SetEnvironmentVariable('PATH', " +
        "[System.Environment]::GetEnvironmentVariable('PATH','User') + ';' + '$InstallDir', 'User')"
    Write-Host "$InstallDir is not on your PATH. To add it for your user:"
    Write-Host "  $addToPath"
    Write-Host "Open a new terminal afterwards for the change to take effect."
}

Write-Host ""
Write-Host "$BinaryName needs Git for Windows: the hooks it deploys are POSIX shell"
Write-Host "and run through bash. Make sure bash.exe from Git for Windows is on PATH,"
Write-Host "ahead of the WSL launcher in system32."
Write-Host ""
Write-Host "Next: run '$BinaryName setup' from a shell that can create symlinks -"
Write-Host "either with Developer Mode enabled or as Administrator."
