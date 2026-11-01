# Run code-server in the foreground on 127.0.0.1 (start it with host-shell's job_start).
# It listens on loopback only because it is reached through host-shell's port forwarding.
# $env:CODE_SERVER_WORKSPACE selects the folder to open (default: the user profile);
# It listens on the first free port from CODE_SERVER_PORT_FIRST to CODE_SERVER_PORT_LAST (version.env);
# $env:CODE_SERVER_PORT pins a single port instead. The agent reads the port from code-server's
# "HTTP server listening on" line and forwards to it.

[CmdletBinding()]
param()

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")

Assert-HscsWindowsAmd64
$config = Get-HscsConfig
$workspace = $env:USERPROFILE
if ($env:CODE_SERVER_WORKSPACE) {
    $workspace = $env:CODE_SERVER_WORKSPACE
}

if (-not (Test-HscsInstalled -Config $config)) {
    throw "code-server is not installed; run install.ps1 first"
}
if (-not (Test-Path -LiteralPath $config.ConfigFile)) {
    throw "configuration not found: $($config.ConfigFile); run install.ps1 first"
}
if (-not (Test-Path -LiteralPath $workspace -PathType Container)) {
    throw "workspace folder not found: $workspace"
}
$listening = @(Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | ForEach-Object { [int]$_.LocalPort })
if ($null -ne $config.Port) {
    $port = $config.Port
    if ($listening -contains $port) {
        throw "port $port on 127.0.0.1 is already in use"
    }
}
else {
    $port = $null
    for ($p = $config.PortFirst; $p -le $config.PortLast; $p++) {
        if ($listening -notcontains $p) {
            $port = $p
            break
        }
    }
    if ($null -eq $port) {
        throw "no free port in $($config.PortFirst)-$($config.PortLast) on 127.0.0.1"
    }
}

Write-Output "starting code-server $($config.Version) on 127.0.0.1:$port"
Write-Output "workspace: $workspace"
Write-Output "configuration: $($config.ConfigFile)"
# Inside a VS Code terminal these make code-server hand over to that VS Code instead of listening.
Remove-Item Env:VSCODE_IPC_HOOK_CLI -ErrorAction SilentlyContinue
Remove-Item Env:ELECTRON_RUN_AS_NODE -ErrorAction SilentlyContinue
Set-Location -LiteralPath $workspace
& $config.NodeExe $config.InstallDir --config $config.ConfigFile --bind-addr "127.0.0.1:$port" `
    --auth password --ignore-last-opened --user-data-dir $config.UserDataDir `
    --extensions-dir $config.ExtensionsDir $workspace
exit $LASTEXITCODE
