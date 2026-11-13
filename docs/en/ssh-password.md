# Storing SSH passwords (ssh-pass)

This document explains how to connect through password-authenticated jump servers or targets without typing the password. Store the password with `host-shell ssh-pass`, and `host-shell bridge` hands it to the `ssh` it starts through OpenSSH's standard `SSH_ASKPASS` mechanism. The route (`ProxyJump`, `ProxyCommand`, user name, port) is written in the client's `~/.ssh/config`, as with VS Code Remote - SSH.

## Use cases

- Using a jump server or target that cannot use (or is not allowed to use) public key authentication from an MCP agent.
- Reaching a password-authenticated server behind an authenticated HTTP proxy ([SSH route proxy](ssh-proxy.md)). Store the proxy password with `--password-file` and the SSH password with `ssh-pass`.

## Procedure

1. Describe the route in `~/.ssh/config`. When several hops use password authentication, use `ProxyJump` instead of a nested `ssh hop1 ssh target`.

    ```text
    Host hop1
        HostName hop1.example.com
        User alice

    Host target
        HostName target.internal
        User alice
        ProxyJump hop1
    ```

2. For each hop, store the password under the `USER@HOST` that `ssh` shows in its password prompt. `HOST` is the host name after `HostName` from `~/.ssh/config` is applied. Run this on a terminal; the input is not echoed.

    ```text
    host-shell ssh-pass set alice@hop1.example.com
    host-shell ssh-pass set alice@target.internal
    ```

3. Register the agent as usual. The `ssh` started by `bridge` receives the stored passwords.

    ```text
    host-shell bridge -- ssh -T -o BatchMode=no target /path/to/host-shell --mcp
    ```

    `BatchMode=yes` disables password authentication, so do not specify it for `ssh` processes that use a stored password.

## Subcommands

| Command | Behavior |
| --- | --- |
| `host-shell ssh-pass set USER@HOST` | Reads the password from the terminal (no echo) and stores it, replacing an existing one |
| `host-shell ssh-pass delete USER@HOST` | Deletes the stored password |
| `host-shell ssh-pass list` | Lists the stored `USER@HOST` (passwords are not shown) |
| `host-shell ssh-pass dir` | Shows the storage directory |

Table: ssh-pass subcommands

## Behavior

- `bridge` sets `SSH_ASKPASS` (`host-shell` itself) and `SSH_ASKPASS_REQUIRE=force` in the environment of the `ssh` it starts. OpenSSH before 8.4 ignores `SSH_ASKPASS_REQUIRE`, so `DISPLAY` is also set when it is unset. Nothing is changed when a non-empty `SSH_ASKPASS` is already set.
- When `ssh` calls it, `host-shell` answers only the "`USER@HOST's password:`" prompt, with the stored password. For host key confirmations, key passphrases, and `USER@HOST` that is not stored, it returns nothing and the `ssh` authentication fails. Register host keys beforehand by running `ssh` once on a terminal.
- The default storage is `host-shell/ssh-passwords` in the user's configuration directory (`$XDG_CONFIG_HOME` or `~/.config` on Linux and macOS, `%AppData%` on Windows). Set the environment variable `HOST_SHELL_SSH_PASSWORD_DIR` to change it. There is one file per `USER@HOST`, holding the password on one line.
- On Linux and macOS the directory is created with `0700` and the files with `0600`. On Windows the files inherit the ACL of the user profile. On reading, the same condition as for [password files](ssh-proxy.md#permission-check-of-the-password-file) is checked.
- Passwords are stored unencrypted (OS credential stores are not used). The protection level equals that of a private key without a passphrase.

## Verified scope

| Item | Method and environment | Status |
| --- | --- | --- |
| Store, get, delete, list, prompt matching (rejecting non-password prompts), building the `SSH_ASKPASS` environment | Automated tests on Linux | Verified (検証済) |
| Terminal input of `ssh-pass set`, direct connection and `ProxyJump` (two hops, both password-authenticated) of `ssh` started by `bridge` | OpenSSH 9.6p1 on Ubuntu 24.04 (client and sshd on the same host) | Verified (検証済) |
| OpenSSH before 8.4 (started through `DISPLAY`) | None | Unverified (未検証) |
| Windows and macOS clients (ACL of the storage, permission check, terminal input of `ssh-pass set`) | None | Unverified (未検証) |

Table: Verification status

## Limitations

- With a nested `ssh hop1 ssh target`, the second `ssh` runs on the jump server and cannot use passwords stored on the client. Use `ProxyJump`.
- Passwords are matched by user name and host name only; the port is not distinguished.
- Key passphrases and keyboard-interactive authentication (one-time passwords, for example) are not supported.
- OS credential stores (Windows Credential Manager, Secret Service, Keychain) are not supported.
