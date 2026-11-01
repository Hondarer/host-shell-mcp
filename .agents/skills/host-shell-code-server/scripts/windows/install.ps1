# Install code-server from the kit on this (target) machine, create its configuration, and on the
# first install inject the default settings and extensions. It never connects to the network:
# the release archive and extensions come from packages\ in the kit (see download.ps1 or download.sh).
# Usage: install.ps1 [-Extension FILE.vsix|ID]

[CmdletBinding()]
param(
    [string]$Extension
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

Assert-HscsWindowsAmd64
$config = Get-HscsConfig
$type = "windows-amd64"

if (Test-HscsInstalled -Config $config) {
    Write-Output "code-server $($config.Version) is already installed: $($config.InstallDir)"
}
else {
    $archive = Join-Path $config.PackagesDir (Get-HscsAssetName -Config $config -Type $type)
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "release archive not found: $archive (run download for $type on the client and send the kit)"
    }
    if (-not (Test-HscsFileHash -Path $archive -Sha256 $config.Sha256[$type])) {
        throw "SHA-256 mismatch: $archive"
    }
    if (@(Get-HscsRunningIds -Config $config).Count -gt 0) {
        throw "code-server is running; stop it before reinstalling"
    }
    New-Item -ItemType Directory -Force -Path $config.BaseDir | Out-Null
    $temp = Join-Path $config.BaseDir (".extract-" + [guid]::NewGuid().ToString("n"))
    New-Item -ItemType Directory -Force -Path $temp | Out-Null
    try {
        # The OS tar.exe, as GNU tar from Git for Windows on PATH cannot handle paths with C:\.
        & (Join-Path $env:SystemRoot "System32\tar.exe") -xzf $archive -C $temp
        if ($LASTEXITCODE -ne 0) {
            throw "failed to extract $archive (tar exit $LASTEXITCODE)"
        }
        $extracted = Join-Path $temp ("code-server-" + $config.Version + "-" + $type)
        if (-not (Test-Path -LiteralPath $extracted)) {
            throw "unexpected archive layout: $archive"
        }
        if (Test-Path -LiteralPath $config.InstallDir) {
            Remove-Item -LiteralPath $config.InstallDir -Recurse -Force
        }
        Move-Item -LiteralPath $extracted -Destination $config.InstallDir
    }
    finally {
        if (Test-Path -LiteralPath $temp) {
            Remove-Item -LiteralPath $temp -Recurse -Force
        }
    }
    Write-Output "installed code-server $($config.Version) ($type): $($config.InstallDir)"
}

foreach ($dir in @($config.UserDataDir, $config.ExtensionsDir, $config.ConfigDir)) {
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
}
if (Test-Path -LiteralPath $config.ConfigFile) {
    Write-Output "keeping the existing configuration: $($config.ConfigFile)"
}
else {
    # The password is never printed: tool output stays in the agent's conversation. The agent copies
    # the configuration to the client with transfer_pull instead.
    $content = @(
        "bind-addr: 127.0.0.1:$($config.PortFirst)"
        "auth: password"
        "password: $(New-HscsPassword)"
        "cert: false"
        "disable-telemetry: true"
        "disable-update-check: true"
    ) -join "`r`n"
    Write-HscsTextFile -Path $config.ConfigFile -Content ($content + "`r`n")
    Protect-HscsFile -Path $config.ConfigFile
    Write-Output "created the configuration with a new password: $($config.ConfigFile)"
}

# Default settings are placed only where no file exists, so changes made in code-server survive.
foreach ($scope in @("User", "Machine")) {
    $target = Join-Path $config.UserDataDir "$scope\settings.json"
    if (Test-Path -LiteralPath $target) {
        Write-Output "keeping existing $scope\settings.json"
    }
    else {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
        Copy-Item -LiteralPath (Join-Path $config.DefaultsDir "$scope\settings.json") -Destination $target
        Write-Output "installed default $scope\settings.json"
    }
}

# Default extensions are installed once: the marker keeps extensions the user removed from
# coming back. It is created last, so a failed run is retried by the next install.
if (Test-Path -LiteralPath $config.Marker) {
    Write-Output "default extensions were already applied (remove $($config.Marker) to apply them again)"
}
else {
    $dir = Join-Path $config.PackagesDir "extensions\$type"
    $list = Join-Path $dir "resolved-extensions.txt"
    if (-not (Test-Path -LiteralPath $list -PathType Leaf)) {
        throw "extension list not found: $list (run download for $type on the client and send the kit)"
    }
    $installed = @(Invoke-HscsCli -Config $config -Arguments @("--list-extensions"))
    $ids = @()
    foreach ($line in Get-Content -LiteralPath $list -Encoding UTF8) {
        $line = $line.Trim()
        if ($line.Length -eq 0 -or $line.StartsWith("#")) {
            continue
        }
        $fields = $line -split "\s+"
        $spec, $platform, $sha, $file = $fields[0], $fields[1], $fields[2], $fields[3]
        $id = $spec.Substring(0, $spec.LastIndexOf("@"))
        $ids += $id
        if ($installed -contains $id) {
            Write-Output "default extension already installed: $id"
            continue
        }
        $vsix = Join-Path $dir $file
        if (-not (Test-HscsFileHash -Path $vsix -Sha256 $sha)) {
            throw "SHA-256 mismatch or missing file: $vsix"
        }
        Invoke-HscsCli -Config $config -Arguments @("--install-extension", $vsix, "--force") | Out-Null
        Write-Output "installed default extension: $spec ($platform)"
    }
    $installed = @(Invoke-HscsCli -Config $config -Arguments @("--list-extensions"))
    foreach ($id in $ids) {
        if (-not ($installed -contains $id)) {
            throw "default extension is not installed: $id"
        }
    }
    Write-HscsTextFile -Path $config.Marker -Content ""
    Write-Output "default extensions applied"
}

if ($Extension) {
    Invoke-HscsCli -Config $config -Arguments @("--install-extension", $Extension, "--force")
}

Write-Output "configuration: $($config.ConfigFile)"
Write-Output "version: $(@(Invoke-HscsCli -Config $config -Arguments @('--version'))[0])"
