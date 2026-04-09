# Install script for nibs — https://github.com/alphaleonis/nibs
# Usage: irm https://raw.githubusercontent.com/alphaleonis/nibs/main/install.ps1 | iex
#   or:  .\install.ps1 -InstallDir C:\Tools -Version v0.2.0

[CmdletBinding()]
param(
    [string]$InstallDir = "$env:USERPROFILE\.local\bin",
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"
$Repo = "alphaleonis/nibs"
$Binary = "nibs"

function Fail($msg) {
    Write-Error $msg
    exit 1
}

# Detect architecture
$Arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64"   { "amd64" }
    "Arm64" { "arm64" }
    default { Fail "Unsupported architecture: $_" }
}

Write-Host "Installing $Binary $Version (windows/$Arch)" -ForegroundColor Cyan

# Resolve version
if (-not $Version) {
    $release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
    $Version = $release.tag_name
    if (-not $Version) { Fail "Could not determine latest version" }
}

Write-Host "Version: $Version"

# Build URLs
$Ver = $Version -replace '^v', ''
$Archive = "${Binary}_${Ver}_windows_${Arch}.zip"
$BaseUrl = "https://github.com/$Repo/releases/download/$Version"
$ArchiveUrl = "$BaseUrl/$Archive"
$ChecksumsUrl = "$BaseUrl/${Binary}_${Ver}_checksums.txt"

# Create temp directory
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "nibs-install-$([System.Guid]::NewGuid().ToString('N').Substring(0,8))"
New-Item -ItemType Directory -Path $TmpDir -Force | Out-Null

try {
    # Download archive and checksums
    Write-Host "Downloading $ArchiveUrl"
    Invoke-WebRequest -Uri $ArchiveUrl -OutFile (Join-Path $TmpDir $Archive) -UseBasicParsing
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile (Join-Path $TmpDir "checksums.txt") -UseBasicParsing

    # Verify checksum
    $checksums = Get-Content (Join-Path $TmpDir "checksums.txt")
    $expected = ($checksums | Where-Object { $_ -match $Archive }) -replace '\s+.*', ''
    if (-not $expected) { Fail "Checksum not found for $Archive" }

    $actual = (Get-FileHash (Join-Path $TmpDir $Archive) -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { Fail "Checksum mismatch: expected $expected, got $actual" }

    Write-Host "Checksum verified" -ForegroundColor Green

    # Extract
    Expand-Archive -Path (Join-Path $TmpDir $Archive) -DestinationPath $TmpDir -Force

    # Install
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item (Join-Path $TmpDir "$Binary.exe") (Join-Path $InstallDir "$Binary.exe") -Force

    Write-Host "Installed $(Join-Path $InstallDir "$Binary.exe")" -ForegroundColor Green

    # Check PATH
    $userPath = [System.Environment]::GetEnvironmentVariable("PATH", [System.EnvironmentVariableTarget]::User)
    if ($userPath -notlike "*$InstallDir*") {
        $addToPath = Read-Host "Add $InstallDir to your PATH? (y/N)"
        if ($addToPath -eq 'y') {
            [System.Environment]::SetEnvironmentVariable(
                "PATH",
                "$userPath;$InstallDir",
                [System.EnvironmentVariableTarget]::User
            )
            $env:PATH = "$env:PATH;$InstallDir"
            Write-Host "Added to PATH. Restart your terminal for it to take effect." -ForegroundColor Yellow
        } else {
            Write-Host "Note: Add $InstallDir to your PATH manually." -ForegroundColor Yellow
        }
    }
} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
