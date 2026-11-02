// Only the host-shell DNS adapter is restored after VS Code sanitizes a child environment.
// This module never restores arbitrary LD_PRELOAD entries or NODE_OPTIONS.
import { lstatSync, readSync, openSync, closeSync } from 'node:fs';
import { basename, dirname, isAbsolute } from 'node:path';

const enabled = process.env.HOST_SHELL_CODE_SERVER_REMOTE_DNS === '1';
// Capture the session from the parent, rather than accepting a child's replacement paths.
const library = process.env.HOST_SHELL_DNS_LIBRARY;
const socket = process.env.HOST_SHELL_DNS_SOCKET;

function fail(message) {
  throw new Error(`host-shell code-server remote DNS: ${message}`);
}

function owned(path, kind) {
  const st = lstatSync(path);
  if (st.isSymbolicLink() || st.uid !== process.getuid() || (st.mode & 0o022)) {
    fail(`${kind} must be owned by the current user and not writable by group or others`);
  }
  return st;
}

export function checkSession() {
  if (!enabled) return false;
  if (process.platform !== 'linux' || !library || !socket ||
      !isAbsolute(library) || !isAbsolute(socket) || /[\s:]/.test(library) ||
      basename(library) !== 'libhostshell_dns.so' || basename(socket) !== 'dns.sock' ||
      dirname(library) !== dirname(socket)) {
    fail('missing or invalid host-shell session metadata; start both ends with --remote-dns');
  }
  const dir = dirname(library);
  if (!basename(dir).startsWith('host-shell-dns-')) fail('unexpected session directory');
  const ds = owned(dir, 'session directory');
  if (!ds.isDirectory() || (ds.mode & 0o077)) fail('session directory must be private (0700)');
  const ls = owned(library, 'adapter');
  if (!ls.isFile() || ls.nlink !== 1) fail('adapter must be a regular, unshared file');
  if (!owned(socket, 'resolver socket').isSocket()) fail('resolver socket is unavailable');
  const fd = openSync(library, 'r');
  try {
    const header = Buffer.alloc(4);
    if (readSync(fd, header, 0, 4, 0) !== 4 || !header.equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46]))) {
      fail('adapter is not an ELF library');
    }
  } finally {
    closeSync(fd);
  }
  return true;
}

export function applyHostShellRemoteDNS(env) {
  if (!enabled) return;
  checkSession();
  if (!env || env.HOST_SHELL_DNS_LIBRARY !== library || env.HOST_SHELL_DNS_SOCKET !== socket) {
    fail('child environment does not belong to this DNS session');
  }
  env.LD_PRELOAD = library;
}

// A requested feature must fail before the server starts if the session has expired.
checkSession();
