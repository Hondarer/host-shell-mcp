// Prepare an independently copied, version-pinned code-server distribution.
import * as fs from 'node:fs';
import * as path from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const toolDir = path.dirname(fileURLToPath(import.meta.url));
const helperPath = 'lib/vscode/out/host-shell-remote-dns.mjs';
const markerPath = '.host-shell-remote-dns.json';
const digest = data => createHash('sha256').update(data).digest('hex');
const reject = message => { throw new Error(`remote DNS patch: ${message}`); };

function regularFile(root, relative) {
  let p = root;
  const parts = relative.split('/');
  for (const [i, part] of parts.entries()) {
    p = path.join(p, part);
    const st = fs.lstatSync(p);
    if (st.isSymbolicLink() || (i < parts.length - 1 ? !st.isDirectory() : !st.isFile())) {
      reject(`unexpected file or symbolic link: ${relative}`);
    }
  }
  return fs.readFileSync(p);
}

function checkRoot(root) {
  const st = fs.lstatSync(root);
  if (!st.isDirectory() || st.isSymbolicLink() || st.uid !== process.getuid() || (st.mode & 0o022)) {
    reject('installation directory must be user-owned and not writable by group or others');
  }
}

function syntax(node, name) {
  // Parsing must not depend on any inherited Node startup flags or native preload.
  const env = { ...process.env };
  for (const key of ['NODE_OPTIONS', 'VSCODE_NODE_OPTIONS', 'LD_PRELOAD']) delete env[key];
  const r = spawnSync(node, ['--check', name], { env, encoding: 'utf8', timeout: 30_000 });
  if (r.error || r.status !== 0) reject(`syntax check failed for ${path.basename(name)}: ${r.error?.message || r.stderr}`);
}

function bytesNeeded(root) {
  let bytes = 0;
  for (const entry of fs.readdirSync(root, { withFileTypes: true })) {
    const p = path.join(root, entry.name);
    if (entry.isDirectory()) bytes += bytesNeeded(p);
    else if (entry.isFile()) bytes += fs.statSync(p).size;
    else if (!entry.isSymbolicLink()) reject('installation contains an unexpected special file');
  }
  return bytes;
}

export function prepare(source, destination, type, manifest, helper, node = process.execPath) {
  source = path.resolve(source);
  destination = path.resolve(destination);
  checkRoot(source);
  if (path.dirname(source) !== path.dirname(destination) || source === destination) {
    reject('destination must be a separate sibling of the original installation');
  }
  const platform = manifest.platforms[type];
  if (!platform) reject(`unsupported platform: ${type}`);
  const vsPackage = JSON.parse(regularFile(source, 'lib/vscode/package.json'));
  const serverPackage = JSON.parse(regularFile(source, 'package.json'));
  if (vsPackage.version !== manifest.vscodeVersion || serverPackage.version !== manifest.codeServerVersion) {
    reject('unsupported code-server / VS Code version');
  }
  for (const name of [helperPath, markerPath]) {
    try { fs.lstatSync(path.join(source, name)); reject('original installation contains patch artifacts'); }
    catch (e) { if (e.code !== 'ENOENT') throw e; }
  }
  const expected = new Map([[helperPath, helper]]);
  for (const [relative, spec] of Object.entries(platform.files)) {
    const original = regularFile(source, relative);
    if (digest(original) !== spec.sha256) reject(`original SHA-256 mismatch: ${relative}`);
    let text = original.toString('utf8');
    if (text.includes('__hostShellRemoteDNS')) reject('original installation is already patched');
    for (const { before, after } of spec.replacements) {
      if (text.split(before).length !== 2) reject(`patch anchor is not unique: ${relative}`);
      text = text.replace(before, after);
    }
    let relativeHelper = path.posix.relative(path.posix.dirname(relative), helperPath);
    if (!relativeHelper.startsWith('.')) relativeHelper = './' + relativeHelper;
    text = `import { applyHostShellRemoteDNS as __hostShellRemoteDNS } from ${JSON.stringify(relativeHelper)};\n` + text;
    expected.set(relative, Buffer.from(text));
  }
  const marker = Buffer.from(JSON.stringify({
    revision: manifest.revision, codeServerVersion: manifest.codeServerVersion,
    vscodeVersion: manifest.vscodeVersion, type, archiveSha256: platform.archiveSha256,
    files: Object.fromEntries([...expected].map(([name, data]) => [name, digest(data)])),
  }, null, 2) + '\n');

  function verify() {
    checkRoot(destination);
    if (!regularFile(destination, markerPath).equals(marker)) reject('existing patched installation has a different manifest');
    for (const [name, data] of expected) {
      if (digest(regularFile(destination, name)) !== digest(data)) reject(`patched SHA-256 mismatch: ${name}`);
    }
  }
  // Check for broken symlinks as well as existing directories.
  try { fs.lstatSync(destination); verify(); return 'verified'; }
  catch (e) { if (e.code !== 'ENOENT' || fs.existsSync(destination)) throw e; }

  const space = fs.statfsSync(path.dirname(destination));
  const needed = bytesNeeded(source) + 64 * 1024 * 1024;
  if (space.bavail * space.bsize < needed) reject(`not enough disk space; ${Math.ceil(needed / 1024 / 1024)} MiB needed`);
  const stage = fs.mkdtempSync(path.join(path.dirname(destination), '.host-shell-remote-dns-'));
  const copy = path.join(stage, 'install');
  try {
    fs.cpSync(source, copy, { recursive: true, verbatimSymlinks: true, mode: fs.constants.COPYFILE_FICLONE });
    fs.chmodSync(copy, 0o700);
    // Refuse if the source changed while being copied.
    for (const [relative, spec] of Object.entries(platform.files)) {
      if (digest(regularFile(copy, relative)) !== spec.sha256) reject(`source changed during copying: ${relative}`);
    }
    for (const [relative, data] of expected) {
      const name = path.join(copy, relative);
      // Never overwrite a shared inode, even if a future copy implementation changes.
      if (fs.existsSync(name) && fs.lstatSync(name).nlink !== 1) reject(`copied file is hard-linked: ${relative}`);
      fs.writeFileSync(name, data);
      syntax(node, name);
    }
    fs.writeFileSync(path.join(copy, markerPath), marker, { mode: 0o600 });
    try { fs.renameSync(copy, destination); }
    catch (e) {
      // Another session may have published the same verified copy while we prepared ours.
      if (!['EEXIST', 'ENOTEMPTY'].includes(e.code)) throw e;
    }
    verify();
    return 'prepared';
  } finally {
    fs.rmSync(stage, { recursive: true, force: true });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const [source, destination, type] = process.argv.slice(2);
    if (!source || !destination || !type || process.argv.length !== 5) reject('usage: patch.mjs SOURCE DESTINATION TYPE');
    const manifest = JSON.parse(fs.readFileSync(path.join(toolDir, 'manifest.json'), 'utf8'));
    const helper = fs.readFileSync(path.join(toolDir, 'session.mjs'));
    console.log(`code-server remote DNS: ${prepare(source, destination, type, manifest, helper)}`);
  } catch (e) {
    console.error(e.message);
    process.exitCode = 1;
  }
}
