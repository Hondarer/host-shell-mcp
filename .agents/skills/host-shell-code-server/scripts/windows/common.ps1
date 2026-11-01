# Shared settings and helpers for the host-shell-code-server Windows scripts; dot-sourced, not run.
# Derived from Hondarer/code-server-launcher (MIT License). Requires Windows PowerShell 5.1 or later.

Set-StrictMode -Version 2.0

function Get-HscsConfig {
    $root = [System.IO.Path]::GetFullPath((Resolve-Path (Join-Path $PSScriptRoot "..\..")).ProviderPath)
    $values = @{}
    foreach ($raw in Get-Content -LiteralPath (Join-Path $root "version.env") -Encoding UTF8) {
        $line = $raw.Trim()
        if ($line.Length -eq 0 -or $line.StartsWith("#")) {
            continue
        }
        $idx = $line.IndexOf("=")
        if ($idx -gt 0) {
            $values[$line.Substring(0, $idx)] = $line.Substring($idx + 1)
        }
    }
    $port = $values["CODE_SERVER_PORT"]
    if ($env:CODE_SERVER_PORT) {
        $port = $env:CODE_SERVER_PORT
    }
    $version = $values["CODE_SERVER_VERSION"]
    # Locations differ from code-server-launcher's, so the two never share an installation or settings.
    $base = Join-Path $env:LOCALAPPDATA "host-shell-code-server"
    $installDir = Join-Path $base ("code-server-" + $version)
    $userDataDir = Join-Path $base "user-data"
    $configDir = Join-Path $env:USERPROFILE ".config\host-shell-code-server"
    return [pscustomobject]@{
        Root          = $root
        Version       = $version
        VsCodeVersion = $values["CODE_SERVER_VSCODE_VERSION"]
        Port          = [int]$port
        Sha256        = @{
            "linux-amd64"   = $values["CODE_SERVER_LINUX_AMD64_SHA256"]
            "linux-arm64"   = $values["CODE_SERVER_LINUX_ARM64_SHA256"]
            "windows-amd64" = $values["CODE_SERVER_WINDOWS_AMD64_SHA256"]
        }
        ReleaseUrl    = "https://github.com/coder/code-server/releases/download/v$version"
        PackagesDir   = Join-Path $root "packages"
        DefaultsDir   = Join-Path $root "defaults"
        BaseDir       = $base
        InstallDir    = $installDir
        NodeExe       = Join-Path $installDir "lib\node.exe"
        UserDataDir   = $userDataDir
        ExtensionsDir = Join-Path $base "extensions"
        ConfigDir     = $configDir
        ConfigFile    = Join-Path $configDir "config.yaml"
        # Created once every default extension is installed; while it exists, install leaves extensions alone.
        Marker        = Join-Path $userDataDir ".host-shell-code-server-defaults"
        # code-server writes coder-logs and heartbeat here whatever --user-data-dir says.
        SharedDataDir = Join-Path $env:LOCALAPPDATA "code-server\Data"
    }
}

function Assert-HscsWindowsAmd64 {
    $arch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) {
        $arch = $env:PROCESSOR_ARCHITEW6432
    }
    if ($arch -ne "AMD64") {
        throw "unsupported architecture: $arch (supported: AMD64)"
    }
}

function Get-HscsAssetName {
    param($Config, [string]$Type)
    return "code-server-$($Config.Version)-$Type.tar.gz"
}

function Test-HscsFileHash {
    param([string]$Path, [string]$Sha256)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $false
    }
    return (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant() -eq $Sha256.ToLowerInvariant()
}

function Test-HscsInstalled {
    param($Config)
    return (Test-Path -LiteralPath $Config.NodeExe) -and (Test-Path -LiteralPath (Join-Path $Config.InstallDir "package.json"))
}

# Invoke-HscsCli runs the code-server command line with this tool's configuration, settings and extensions.
function Invoke-HscsCli {
    param($Config, [string[]]$Arguments)
    $output = & $Config.NodeExe $Config.InstallDir --config $Config.ConfigFile --user-data-dir $Config.UserDataDir --extensions-dir $Config.ExtensionsDir @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "code-server $($Arguments -join ' ') failed (exit $LASTEXITCODE)"
    }
    return $output
}

# Get-HscsRunningIds returns the node.exe processes serving code-server with this tool's configuration.
function Get-HscsRunningIds {
    param($Config)
    $ids = @()
    foreach ($p in @(Get-CimInstance Win32_Process -Filter "Name = 'node.exe'" -ErrorAction SilentlyContinue)) {
        if ($null -eq $p) {
            continue
        }
        $command = [string]$p.CommandLine
        if ($command.Contains($Config.ConfigFile) -and -not $command.Contains("--install-extension") -and -not $command.Contains("--list-extensions")) {
            $ids += [int]$p.ProcessId
        }
    }
    return @($ids)
}

function Write-HscsTextFile {
    param([string]$Path, [string]$Content)
    [System.IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding $false))
}

# Protect-HscsFile limits a file to the current user by removing inherited access.
function Protect-HscsFile {
    param([string]$Path)
    $acl = New-Object System.Security.AccessControl.FileSecurity
    $acl.SetAccessRuleProtection($true, $false)
    $identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    $acl.AddAccessRule((New-Object System.Security.AccessControl.FileSystemAccessRule($identity, "FullControl", "Allow")))
    [System.IO.File]::SetAccessControl($Path, $acl)
}

function New-HscsPassword {
    $bytes = New-Object byte[] 24
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    }
    finally {
        $rng.Dispose()
    }
    return -join ($bytes | ForEach-Object { $_.ToString("x2") })
}
