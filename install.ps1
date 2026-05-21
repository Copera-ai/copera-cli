$ErrorActionPreference = "Stop"

$cdn = "https://cli.copera.ai"
$installDir = if ($env:INSTALL_DIR) {
    $env:INSTALL_DIR
} else {
    Join-Path $env:LOCALAPPDATA "Microsoft\WindowsApps"
}

if (-not $env:VERSION) {
    $versionInfo = Invoke-RestMethod "$cdn/version.json"
    $version = $versionInfo.latest
} else {
    $version = $env:VERSION
}

if ([string]::IsNullOrWhiteSpace($version)) {
    throw "Could not determine latest copera version"
}

$version = $version.TrimStart("v")
$asset = "copera-$version-windows-amd64.zip"
$url = "$cdn/v$version/$asset"
$tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) "copera-install-$([System.Guid]::NewGuid())"
$zipPath = Join-Path $tmpDir $asset

Write-Host "Installing copera v$version (windows/amd64)..."

New-Item -ItemType Directory -Force $tmpDir | Out-Null
New-Item -ItemType Directory -Force $installDir | Out-Null

try {
    Invoke-WebRequest $url -OutFile $zipPath
    Expand-Archive $zipPath -DestinationPath $tmpDir -Force

    $binary = Join-Path $tmpDir "copera.exe"
    if (-not (Test-Path $binary)) {
        throw "Archive did not contain copera.exe"
    }

    Move-Item $binary (Join-Path $installDir "copera.exe") -Force

    Write-Host "Installed copera v$version to $(Join-Path $installDir "copera.exe")"
    Write-Host "Open a new PowerShell window, then run 'copera auth login' to get started."
} finally {
    Remove-Item $tmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
