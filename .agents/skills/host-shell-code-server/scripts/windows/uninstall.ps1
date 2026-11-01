# Remove code-server installed by install.ps1. Settings, user data, extensions and the kit stay
# unless -Purge is given. Stop code-server (job_stop) first.
# Usage: uninstall.ps1 [-Purge]

[CmdletBinding()]
param(
    [switch]$Purge
)

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

$config = Get-HscsConfig
if (@(Get-HscsRunningIds -Config $config).Count -gt 0) {
    throw "code-server is running; stop it (job_stop) before uninstalling"
}

foreach ($dir in @(Get-ChildItem -LiteralPath $config.BaseDir -Directory -Filter "code-server-*" -ErrorAction SilentlyContinue)) {
    Remove-Item -LiteralPath $dir.FullName -Recurse -Force
    Write-Output "removed code-server: $($dir.FullName)"
}

if ($Purge) {
    # The kit, including this script, lives under the base folder; PowerShell has already read it.
    foreach ($path in @($config.BaseDir, $config.ConfigDir, (Join-Path $config.SharedDataDir "coder-logs"), (Join-Path $config.SharedDataDir "heartbeat"))) {
        if (Test-Path -LiteralPath $path) {
            Remove-Item -LiteralPath $path -Recurse -Force
        }
    }
    # Only the files code-server wrote outside --user-data-dir; the folder may be shared.
    if ((Test-Path -LiteralPath $config.SharedDataDir) -and -not (Get-ChildItem -LiteralPath $config.SharedDataDir -Force)) {
        Remove-Item -LiteralPath $config.SharedDataDir
    }
    Write-Output "removed the configuration, user data, extensions and kit"
}
else {
    Write-Output "kept the configuration, user data, extensions and kit (use -Purge to remove them)"
}
