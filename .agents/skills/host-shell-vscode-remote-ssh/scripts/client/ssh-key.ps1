# Create, on the Windows machine that runs VS Code, the dedicated SSH key and known_hosts for one
# target, and print the ~/.ssh/config block for the user to add. Same behavior as ssh-key.sh;
# ~/.ssh/config itself is never modified.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File ssh-key.ps1 key NAME
#          create %USERPROFILE%\.ssh\host-shell-vscode-NAME_ed25519 (kept when present) and print
#          its public key
#        powershell -NoProfile -ExecutionPolicy Bypass -File ssh-key.ps1 trust NAME LISTEN_PORT USER "HOST_KEY"
#          write %USERPROFILE%\.ssh\host-shell-vscode-NAME_known_hosts for 127.0.0.1:LISTEN_PORT
#          with HOST_KEY (the "host key:" line of authorize.sh) and print the Host block
param(
    [Parameter(Mandatory = $true, Position = 0)][ValidateSet('key', 'trust')][string]$Mode,
    [Parameter(Mandatory = $true, Position = 1)][ValidatePattern('^[A-Za-z0-9._-]+$')][string]$Name,
    [Parameter(Position = 2)][string]$ListenPort = '',
    [Parameter(Position = 3)][string]$User = '',
    [Parameter(Position = 4)][string]$HostKey = ''
)
$ErrorActionPreference = 'Stop'
$alias = "host-shell-vscode-$Name"
$dir = Join-Path $env:USERPROFILE '.ssh'
$key = Join-Path $dir "${alias}_ed25519"
$known = Join-Path $dir "${alias}_known_hosts"
New-Item -ItemType Directory -Force -Path $dir | Out-Null

if ($Mode -eq 'key') {
    if (Test-Path -LiteralPath $key) {
        [Console]::Error.WriteLine("key exists: $key")
    } else {
        # Start-Process passes the command line as written, so the empty passphrase ("") reaches
        # ssh-keygen in Windows PowerShell 5.1 and PowerShell 7 alike.
        $p = Start-Process -FilePath 'ssh-keygen.exe' -NoNewWindow -Wait -PassThru `
            -ArgumentList "-q -t ed25519 -N `"`" -C $alias -f `"$key`""
        if ($p.ExitCode -ne 0) { throw "ssh-keygen failed with exit code $($p.ExitCode)" }
        [Console]::Error.WriteLine("created: $key")
    }
    Get-Content -LiteralPath "$key.pub"
    exit 0
}

if ($ListenPort -notmatch '^[0-9]+$' -or $User -notmatch '^[A-Za-z0-9._-]+$' -or -not $HostKey) {
    throw 'usage: ssh-key.ps1 trust NAME LISTEN_PORT USER "HOST_KEY"'
}
# Keep the key type and the key; drop the comment.
$fields = ($HostKey -replace '^host key:\s*', '').Trim() -split '\s+'
if ($fields.Count -lt 2 -or $fields[0] -ne 'ssh-ed25519') { throw 'HOST_KEY must be the ssh-ed25519 line from authorize.sh' }
[IO.File]::WriteAllText($known, "[127.0.0.1]:$ListenPort $($fields[0]) $($fields[1])`n")
[Console]::Error.WriteLine("wrote: $known")
@(
    "Host $alias",
    "    HostName 127.0.0.1",
    "    Port $ListenPort",
    "    User $User",
    "    IdentityFile ~/.ssh/${alias}_ed25519",
    "    IdentitiesOnly yes",
    "    UserKnownHostsFile ~/.ssh/${alias}_known_hosts",
    "    StrictHostKeyChecking yes"
) -join "`n"
