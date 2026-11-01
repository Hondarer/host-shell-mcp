# Fetch, on a Windows client, everything install needs offline on a target of each TYPE: the
# code-server release archive and the default extensions in defaults\extensions.txt, verified by
# SHA-256 and stored under packages\. Extensions are resolved as scripts/lib/resolve_extensions.py
# does for Linux and macOS clients: the target platform's build or the universal build, no
# pre-releases, the given @version or else the newest one whose engines.vscode accepts
# CODE_SERVER_VSCODE_VERSION. Unresolvable extensions are skipped with a warning.
# Usage: download.ps1 TYPE...   (TYPE: linux-amd64, linux-arm64, windows-amd64)

Set-StrictMode -Version 2.0
$ErrorActionPreference = "Stop"
. (Join-Path $PSScriptRoot "common.ps1")
# Windows PowerShell 5.1 defaults to older TLS versions.
[System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12

$platforms = @{ "linux-amd64" = "linux-x64"; "linux-arm64" = "linux-arm64"; "windows-amd64" = "win32-x64" }
if ($args.Count -eq 0) {
    throw "usage: download.ps1 TYPE...   (TYPE: linux-amd64, linux-arm64, windows-amd64)"
}
foreach ($t in $args) {
    if (-not $platforms.ContainsKey([string]$t)) {
        throw "unknown TYPE: $t (use linux-amd64, linux-arm64 or windows-amd64)"
    }
}
$config = Get-HscsConfig
$curl = Join-Path $env:SystemRoot "System32\curl.exe"

function Get-VersionParts {
    param([string]$Text)
    $parts = @()
    foreach ($p in (($Text -split "-")[0] -split "\.")) {
        $n = 0
        [void][int]::TryParse($p, [ref]$n)
        $parts += $n
    }
    while ($parts.Count -lt 3) {
        $parts += 0
    }
    return , @($parts[0..2])
}

function Compare-Version {
    param($A, $B)
    for ($i = 0; $i -lt 3; $i++) {
        if ($A[$i] -ne $B[$i]) {
            return [Math]::Sign($A[$i] - $B[$i])
        }
    }
    return 0
}

# Test-EngineRange reports whether Version satisfies an engines.vscode range such as ^1.92.0.
function Test-EngineRange {
    param([string]$Range, [string]$Version)
    $Range = $Range.Trim()
    if ($Range -eq "" -or $Range -eq "*") {
        return $true
    }
    if ($Range -notmatch '^(\^|~|>=|>|<=|<|=)?\s*v?(\d+)(?:\.(\d+|x|\*))?(?:\.(\d+|x|\*))?(?:-[\w.]+)?$') {
        return $false
    }
    $op = if ($Matches[1]) { $Matches[1] } else { "=" }
    $want = @(foreach ($g in @($Matches[2], $Matches[3], $Matches[4])) { if ($g -match '^\d+$') { [int]$g } else { 0 } })
    $wild = @(foreach ($g in @($Matches[3], $Matches[4])) { $g -eq "x" -or $g -eq "*" })
    $have = Get-VersionParts $Version
    $cmp = Compare-Version $have $want
    switch ($op) {
        # ^0.y.z pins the minor version, like npm's caret.
        "^" { return ($have[0] -eq $want[0]) -and ($want[0] -ne 0 -or $have[1] -eq $want[1]) -and $cmp -ge 0 }
        "~" { return ($have[0] -eq $want[0]) -and ($have[1] -eq $want[1]) -and $cmp -ge 0 }
        ">=" { return $cmp -ge 0 }
        ">" { return $cmp -gt 0 }
        "<=" { return $cmp -le 0 }
        "<" { return $cmp -lt 0 }
    }
    return ($have[0] -eq $want[0]) -and ($wild[0] -or $have[1] -eq $want[1]) -and ($wild[0] -or $wild[1] -or $have[2] -eq $want[2])
}

function Get-OpenVsx {
    param([string]$Url)
    return Invoke-RestMethod -Uri $Url -UseBasicParsing -Headers @{ "User-Agent" = "host-shell-code-server" }
}

function Save-VerifiedFile {
    param([string]$Url, [string]$Destination, [string]$Sha256)
    if (Test-HscsFileHash -Path $Destination -Sha256 $Sha256) {
        return $false
    }
    $partial = "$Destination.partial"
    & $curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 20 -o $partial $Url
    if ($LASTEXITCODE -ne 0) {
        throw "download failed (curl exit $LASTEXITCODE): $Url"
    }
    if (-not (Test-HscsFileHash -Path $partial -Sha256 $Sha256)) {
        Remove-Item -LiteralPath $partial -Force
        throw "SHA-256 mismatch: $Url"
    }
    Move-Item -LiteralPath $partial -Destination $Destination -Force
    return $true
}

# Resolve-Extension returns the chosen Open VSX entry, or a string with the reason it was skipped.
function Resolve-Extension {
    param([string]$Id, [string]$Version, [string]$Platform)
    foreach ($target in @($Platform, "universal")) {
        $query = "https://open-vsx.org/api/-/query?extensionId=$([uri]::EscapeDataString($Id))&includeAllVersions=true&targetPlatform=$target"
        $entries = @((Get-OpenVsx $query).extensions)
        if ($entries.Count -eq 0) {
            continue
        }
        $candidates = @($entries | Where-Object { -not ($_.PSObject.Properties["preRelease"] -and $_.preRelease) })
        if ($Version) {
            $candidates = @($candidates | Where-Object { $_.version -eq $Version })
        }
        else {
            $candidates = @($candidates | Where-Object {
                    $range = ""
                    if ($_.PSObject.Properties["engines"] -and $_.engines -and $_.engines.PSObject.Properties["vscode"]) {
                        $range = [string]$_.engines.vscode
                    }
                    Test-EngineRange $range $config.VsCodeVersion
                })
        }
        $best = $null
        foreach ($c in $candidates) {
            if ($null -eq $best -or (Compare-Version (Get-VersionParts $c.version) (Get-VersionParts $best.version)) -gt 0) {
                $best = $c
            }
        }
        if ($best) {
            return $best
        }
        if ($Version) {
            return "version $Version not found for $target"
        }
        return "no version compatible with VS Code $($config.VsCodeVersion) for $target"
    }
    return "not found on Open VSX for $Platform"
}

New-Item -ItemType Directory -Force -Path $config.PackagesDir | Out-Null
foreach ($t in $args) {
    $type = [string]$t
    $name = Get-HscsAssetName -Config $config -Type $type
    $dest = Join-Path $config.PackagesDir $name
    if (Save-VerifiedFile -Url "$($config.ReleaseUrl)/$name" -Destination $dest -Sha256 $config.Sha256[$type]) {
        Write-Output "saved $dest"
    }
    else {
        Write-Output "verified archive already present: $dest"
    }

    $platform = $platforms[$type]
    $out = Join-Path $config.PackagesDir "extensions\$type"
    New-Item -ItemType Directory -Force -Path $out | Out-Null
    $lines = @()
    $keep = @{}
    $skipped = 0
    foreach ($raw in Get-Content -LiteralPath (Join-Path $config.DefaultsDir "extensions.txt") -Encoding UTF8) {
        $spec = ($raw -split "#", 2)[0].Trim()
        if ($spec.Length -eq 0) {
            continue
        }
        $id, $version = $spec, ""
        if ($spec.Contains("@")) {
            $id, $version = $spec -split "@", 2
        }
        try {
            $entry = Resolve-Extension -Id $id -Version $version -Platform $platform
            if ($entry -is [string]) {
                throw $entry
            }
            $file = [uri]::UnescapeDataString(([uri]$entry.files.download).Segments[-1])
            $sha = ([string](& $curl -fsSL --retry 3 $entry.files.sha256)).Trim().Split()[0].ToLowerInvariant()
            if ($LASTEXITCODE -ne 0 -or $sha.Length -ne 64) {
                throw "could not read the SHA-256 of $file"
            }
            if (Save-VerifiedFile -Url $entry.files.download -Destination (Join-Path $out $file) -Sha256 $sha) {
                Write-Output "fetched $file"
            }
            else {
                Write-Output "verified $file"
            }
            $lines += "$($entry.namespace).$($entry.name)@$($entry.version) $($entry.targetPlatform) $sha $file"
            $keep[$file] = $true
        }
        catch {
            Write-Warning "skipped ${spec}: $($_.Exception.Message)"
            $lines += "# skipped ${spec}: $($_.Exception.Message)"
            $skipped++
        }
    }
    # Drop VSIX files of earlier resolutions, so the kit carries only what the list names.
    foreach ($f in @(Get-ChildItem -LiteralPath $out -Filter "*.vsix")) {
        if (-not $keep.ContainsKey($f.Name)) {
            Remove-Item -LiteralPath $f.FullName -Force
        }
    }
    $header = @("# Default extensions for $type ($platform), VS Code $($config.VsCodeVersion)", "# ID@VERSION PLATFORM SHA-256 FILE")
    Write-HscsTextFile -Path (Join-Path $out "resolved-extensions.txt") -Content ((($header + $lines) -join "`n") + "`n")
    Write-Output "resolved $($keep.Count) extension(s) for $type, skipped ${skipped}: $out"
}
