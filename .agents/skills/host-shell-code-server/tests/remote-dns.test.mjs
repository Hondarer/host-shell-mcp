import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';
import * as net from 'node:net';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';
import { prepare } from '../scripts/linux/remote-dns/patch.mjs';

const tools = fileURLToPath(new URL('../scripts/linux/remote-dns/', import.meta.url));
const helper = fs.readFileSync(path.join(tools, 'session.mjs'));
const hash = b => createHash('sha256').update(b).digest('hex');
const entry = 'lib/vscode/out/server-main.js';
const entries = [entry, 'lib/vscode/out/vs/workbench/api/node/extensionHostProcess.js', 'lib/vscode/out/vs/platform/agentHost/node/agentHostMain.js'];

function fixture(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'host-shell-code-server-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const source = path.join(dir, 'original'), dest = path.join(dir, 'patched');
  fs.mkdirSync(path.join(source, 'lib/vscode/out'), { recursive: true });
  fs.chmodSync(source, 0o700);
  fs.writeFileSync(path.join(source, 'package.json'), JSON.stringify({ version: '4.144.0', type: 'module' }));
  fs.writeFileSync(path.join(source, 'lib/vscode/package.json'), JSON.stringify({ version: '1.144.0', type: 'module' }));
  const code = Buffer.from(`function sanitize(env){delete env.LD_PRELOAD;delete env.NODE_OPTIONS;}\nfunction fork(env){globalThis.result=env;}\nexport function run(env){sanitize(env);fork(env);}\n`);
  for (const name of entries) { fs.mkdirSync(path.dirname(path.join(source, name)), {recursive:true}); fs.writeFileSync(path.join(source, name), code); }
  const manifest = { revision: 1, codeServerVersion: '4.144.0', vscodeVersion: '1.144.0', platforms: {
    'linux-amd64': { archiveSha256: 'verified-fixture', files: Object.fromEntries(entries.map(name => [name, { sha256: hash(code), replacements: [
      { before: 'sanitize(env);fork(env);', after: 'sanitize(env);__hostShellRemoteDNS(env);fork(env);' },
    ] }])) },
  } };
  return { source, dest, manifest, code, run: () => prepare(source, dest, 'linux-amd64', manifest, helper) };
}

async function session(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'host-shell-dns-test-'));
  const library = path.join(dir, 'libhostshell_dns.so'), socket = path.join(dir, 'dns.sock');
  fs.writeFileSync(library, Buffer.from([0x7f, 0x45, 0x4c, 0x46]), { mode: 0o600 });
  const server = net.createServer(c => c.end());
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(socket, resolve); });
  fs.chmodSync(socket, 0o600);
  t.after(async () => { await new Promise(resolve => server.close(resolve)); fs.rmSync(dir, { recursive: true, force: true }); });
  return { dir, library, socket, env: { HOST_SHELL_CODE_SERVER_REMOTE_DNS: '1', HOST_SHELL_DNS_LIBRARY: library, HOST_SHELL_DNS_SOCKET: socket } };
}

function child(f, env, alter = '', module = entry) {
  const inherited = { ...process.env, ...env };
  for (const k of ['LD_PRELOAD', 'NODE_OPTIONS', 'VSCODE_NODE_OPTIONS']) delete inherited[k];
  return spawnSync(process.execPath, ['--input-type=module', '-e', `
    const {run}=await import(${JSON.stringify(path.join(f.dest, module))});
    const env={...process.env,LD_PRELOAD:'/unrelated.so',NODE_OPTIONS:'--require=/unrelated.cjs'};
    ${alter}
    run(env); console.log(JSON.stringify({preload:env.LD_PRELOAD,nodeOptions:env.NODE_OPTIONS}));
  `], { env: inherited, encoding: 'utf8', timeout: 10_000 });
}

test('patch copy is isolated, verified on reuse, and refuses modified output', t => {
  const f = fixture(t);
  assert.equal(f.run(), 'prepared');
  assert.deepEqual(fs.readFileSync(path.join(f.source, entry)), f.code);
  assert.notEqual(fs.statSync(path.join(f.source, entry)).ino, fs.statSync(path.join(f.dest, entry)).ino);
  assert.equal(f.run(), 'verified');
  fs.appendFileSync(path.join(f.dest, entry), '// modified');
  assert.throws(f.run, /patched SHA-256 mismatch/);
  assert.deepEqual(fs.readFileSync(path.join(f.source, entry)), f.code);
});

test('unknown source, duplicate anchors, and unsupported versions are rejected', t => {
  const f = fixture(t);
  fs.appendFileSync(path.join(f.source, entry), '// changed');
  assert.throws(f.run, /original SHA-256 mismatch/);
  fs.writeFileSync(path.join(f.source, entry), f.code);
  f.manifest.platforms['linux-amd64'].files[entry].replacements[0].before = 'env';
  assert.throws(f.run, /anchor is not unique/);
  fs.writeFileSync(path.join(f.source, 'package.json'), '{"version":"unknown"}');
  assert.throws(f.run, /unsupported code-server/);
  assert.equal(fs.existsSync(f.dest), false);
});

test('symlinked source and helper paths cannot be overwritten', t => {
  const f = fixture(t), original = path.join(f.source, entry);
  const outside = path.join(path.dirname(f.source), 'outside.js');
  fs.renameSync(original, outside); fs.symlinkSync(outside, original);
  assert.throws(f.run, /symbolic link/);
  fs.unlinkSync(original); fs.renameSync(outside, original);
  fs.symlinkSync('/nonexistent', path.join(f.source, 'lib/vscode/out/host-shell-remote-dns.mjs'));
  assert.throws(f.run, /patch artifacts/);
});

test('syntax failures leave no published copy or staging directories', t => {
  const f = fixture(t);
  assert.throws(() => prepare(f.source, f.dest, 'linux-amd64', f.manifest, Buffer.from('invalid javascript !!!')), /syntax check failed/);
  assert.equal(fs.existsSync(f.dest), false);
  assert.deepEqual(fs.readdirSync(path.dirname(f.source)), ['original']);
});

test('explicit sessions restore only their own adapter after sanitization', async t => {
  const f = fixture(t); f.run();
  const a = await session(t), b = await session(t);
  for (const s of [a, b]) for (const module of entries) {
    const r = child(f, s.env, "", module);
    assert.equal(r.status, 0, r.stderr);
    assert.deepEqual(JSON.parse(r.stdout), { preload: s.library });
  }
  const wrong = child(f, a.env, `env.HOST_SHELL_DNS_SOCKET=${JSON.stringify(b.socket)};`);
  assert.notEqual(wrong.status, 0); assert.match(wrong.stderr, /does not belong/);
});

test('disabled mode preserves normal sanitization; missing session fails closed', t => {
  const f = fixture(t); f.run();
  const r = child(f, { HOST_SHELL_CODE_SERVER_REMOTE_DNS: '0' });
  assert.equal(r.status, 0, r.stderr); assert.deepEqual(JSON.parse(r.stdout), {});
  const missing = child(f, { HOST_SHELL_CODE_SERVER_REMOTE_DNS: '1', HOST_SHELL_DNS_LIBRARY: '', HOST_SHELL_DNS_SOCKET: '' });
  assert.notEqual(missing.status, 0); assert.match(missing.stderr, /session metadata/);
});

test('unsafe adapter, symlink, and expired socket fail closed', async t => {
  const f = fixture(t); f.run(); const s = await session(t);
  fs.chmodSync(s.library, 0o666);
  assert.match(child(f, s.env).stderr, /not writable/);
  fs.chmodSync(s.library, 0o600);
  fs.renameSync(s.library, s.library + '.original'); fs.symlinkSync(s.library + '.original', s.library);
  assert.match(child(f, s.env).stderr, /not writable/);
  fs.unlinkSync(s.library); fs.renameSync(s.library + '.original', s.library);
  fs.unlinkSync(s.socket);
  assert.notEqual(child(f, s.env).status, 0);
});
