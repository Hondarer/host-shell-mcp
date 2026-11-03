# Fetch, on a Windows client, the VS Code CLI and server for one client commit and check them
# against the SHA-256 published by the VS Code update service. Same result as fetch.sh.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File fetch.ps1 COMMIT [x64|arm64] [OUT_DIR]
#   COMMIT is the second line of `code --version` on the machine running VS Code.
#   x64 / arm64 is the CPU of the target (default x64).
#   OUT_DIR defaults to the skill's packages\ folder.
# Result: OUT_DIR\<COMMIT>-<ARCH>\{cli.tar.gz,server.tar.gz,SHA256SUMS,COMMIT}; place.sh takes
# that folder. A folder that already verifies is kept as it is.
param(
    [Parameter(Mandatory = $true, Position = 0)][string]$Commit,
    [Parameter(Position = 1)][ValidateSet('x64', 'arm64')][string]$Arch = 'x64',
    [Parameter(Position = 2)][string]$OutDir = ''
)
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

if ($Commit -cnotmatch '^[0-9a-f]{40}$') { throw "commit must be 40 lowercase hex digits: $Commit" }
if (-not $OutDir) { $OutDir = Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'packages' }
$dir = Join-Path $OutDir "$Commit-$Arch"
$sums = Join-Path $dir 'SHA256SUMS'
$commitFile = Join-Path $dir 'COMMIT'

function Test-Sums([string]$Path) {
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -notmatch '^([0-9a-f]{64})  (\S+)$') { return $false }
        $f = Join-Path $dir $Matches[2]
        if (-not (Test-Path -LiteralPath $f)) { return $false }
        if ((Get-FileHash -Algorithm SHA256 -LiteralPath $f).Hash.ToLowerInvariant() -ne $Matches[1]) { return $false }
    }
    return $true
}

if ((Test-Path -LiteralPath $commitFile) -and (Test-Sums $sums)) {
    "verified distribution already present: $dir"
    exit 0
}
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Remove-Item -LiteralPath $commitFile, $sums -Force -ErrorAction SilentlyContinue
$api = "https://update.code.visualstudio.com/api/versions/commit:$Commit"
$lines = @()
# The CLI build for Alpine is statically linked and runs on glibc targets too; Remote - SSH uses it.
foreach ($pair in @(@("cli-alpine-$Arch", 'cli.tar.gz'), @("server-linux-$Arch", 'server.tar.gz'))) {
    try { $meta = Invoke-RestMethod -UseBasicParsing -Uri "$api/$($pair[0])/stable" }
    catch { throw "the update service has no $($pair[0]) build for ${Commit}: $($_.Exception.Message)" }
    if (-not $meta.url -or $meta.sha256hash -notmatch '^[0-9a-f]{64}$') { throw "no download information for $($pair[0]) at $Commit" }
    "downloading $($pair[0]) $($meta.productVersion)"
    $dest = Join-Path $dir $pair[1]
    Invoke-WebRequest -UseBasicParsing -Uri $meta.url -OutFile "$dest.part"
    Move-Item -Force -LiteralPath "$dest.part" -Destination $dest
    $lines += "$($meta.sha256hash)  $($pair[1])"
}
# LF line endings and two spaces, as sha256sum -c on the target expects.
[IO.File]::WriteAllText($sums, (($lines -join "`n") + "`n"))
if (-not (Test-Sums $sums)) { throw "SHA-256 mismatch in $dir" }
[IO.File]::WriteAllText($commitFile, "$Commit`n")
$mib = [math]::Round(((Get-ChildItem -LiteralPath $dir | Measure-Object -Property Length -Sum).Sum) / 1MB)
"saved $dir ($mib MiB)"
