import { test } from 'node:test';
import assert from 'node:assert/strict';
import * as fs from 'node:fs';
import * as path from 'node:path';
import * as os from 'node:os';
import { spawnSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';

const script = fileURLToPath(new URL('../scripts/lib/language-packs.mjs', import.meta.url));

function tempDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'host-shell-code-server-lp-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const userData = path.join(dir, 'user-data'), extensions = path.join(dir, 'extensions');
  fs.mkdirSync(userData);
  fs.mkdirSync(extensions);
  return { dir, userData, extensions };
}

function addExtension(extensions, folder, manifest) {
  fs.mkdirSync(path.join(extensions, folder), { recursive: true });
  fs.writeFileSync(path.join(extensions, folder, 'package.json'), JSON.stringify(manifest));
}

function run(userData, extensions, ...extra) {
  const r = spawnSync(process.execPath, [script, userData, extensions, ...extra], { encoding: 'utf8' });
  const file = path.join(userData, 'languagepacks.json');
  return { ...r, packs: fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, 'utf8')) : undefined };
}

const jaManifest = {
  name: 'vscode-language-pack-ja', version: '1.131.0',
  contributes: { localizations: [{ languageId: 'ja', languageName: 'Japanese', localizedLanguageName: '\u65e5\u672c\u8a9e', translations: [
    { id: 'vscode', path: './translations/main.i18n.json' },
    { id: 'vscode.git', path: './translations/extensions/vscode.git.i18n.json' },
  ] }] },
};

test('registers installed language packs in the server cache format', (t) => {
  const { userData, extensions, dir } = tempDir(t);
  addExtension(extensions, 'ms-ceintl.vscode-language-pack-ja-1.131.0', jaManifest);
  addExtension(extensions, 'ms-vscode.hexeditor-1.11.1', { name: 'hexeditor', version: '1.11.1' });
  addExtension(dir, 'outside', jaManifest);
  fs.writeFileSync(path.join(extensions, 'extensions.json'), JSON.stringify([
    { identifier: { id: 'ms-ceintl.vscode-language-pack-ja' }, version: '1.131.0', relativeLocation: 'ms-ceintl.vscode-language-pack-ja-1.131.0' },
    { identifier: { id: 'ms-vscode.hexeditor' }, version: '1.11.1', relativeLocation: 'ms-vscode.hexeditor-1.11.1' },
    // Entries outside the extensions directory are ignored.
    { identifier: { id: 'evil.pack' }, version: '1.0.0', location: { scheme: 'file', path: path.join(dir, 'outside') } },
  ]));
  const r = run(userData, extensions);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /registered language packs: ja/);
  assert.deepEqual(Object.keys(r.packs), ['ja']);
  const ja = r.packs.ja;
  assert.equal(ja.label, '\u65e5\u672c\u8a9e');
  assert.deepEqual(ja.extensions, [{ extensionIdentifier: { id: 'ms-ceintl.vscode-language-pack-ja' }, version: '1.131.0' }]);
  assert.equal(ja.hash, createHash('md5').update('ms-ceintl.vscode-language-pack-ja').update('1.131.0').digest('hex'));
  const base = path.join(extensions, 'ms-ceintl.vscode-language-pack-ja-1.131.0');
  assert.equal(ja.translations.vscode, path.join(base, 'translations/main.i18n.json'));
  assert.equal(ja.translations['vscode.git'], path.join(base, 'translations/extensions/vscode.git.i18n.json'));
});

test('writes an empty cache when no language pack is installed', (t) => {
  const { userData, extensions } = tempDir(t);
  fs.writeFileSync(path.join(userData, 'languagepacks.json'), '{"ja":{"stale":true}}');
  const r = run(userData, extensions);
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /no language packs installed/);
  assert.deepEqual(r.packs, {});
});

test('fails on a malformed extensions.json without touching the cache', (t) => {
  const { userData, extensions } = tempDir(t);
  fs.writeFileSync(path.join(extensions, 'extensions.json'), '{not json');
  fs.writeFileSync(path.join(userData, 'languagepacks.json'), '{"keep":1}');
  const r = run(userData, extensions);
  assert.notEqual(r.status, 0);
  assert.deepEqual(r.packs, { keep: 1 });
});

function withJa(t) {
  const d = tempDir(t);
  addExtension(d.extensions, 'ms-ceintl.vscode-language-pack-ja-1.131.0', jaManifest);
  fs.writeFileSync(path.join(d.extensions, 'extensions.json'), JSON.stringify([
    { identifier: { id: 'ms-ceintl.vscode-language-pack-ja' }, version: '1.131.0', relativeLocation: 'ms-ceintl.vscode-language-pack-ja-1.131.0' },
  ]));
  d.argv = path.join(d.userData, 'User', 'argv.json');
  return d;
}

// argv.json is JSON with comments.
const parseJsonc = (text) => JSON.parse(text.replace(/\/\/[^\n]*/g, ''));

test('--locale creates argv.json when it does not exist', (t) => {
  const { userData, extensions, argv } = withJa(t);
  const r = run(userData, extensions, '--locale', 'JA');
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /display language: ja/);
  assert.equal(parseJsonc(fs.readFileSync(argv, 'utf8')).locale, 'ja');
});

test('--locale replaces only the locale and keeps other content', (t) => {
  const { userData, extensions, argv } = withJa(t);
  fs.mkdirSync(path.dirname(argv), { recursive: true });
  fs.writeFileSync(argv, '{\n    // keep me\n    "locale": "en",\n    "enable-crash-reporter": false\n}\n');
  assert.equal(run(userData, extensions, '--locale', 'ja').status, 0);
  const text = fs.readFileSync(argv, 'utf8');
  assert.match(text, /\/\/ keep me/);
  assert.deepEqual(parseJsonc(text), { locale: 'ja', 'enable-crash-reporter': false });
});

test('--locale adds the locale to an argv.json without one', (t) => {
  const { userData, extensions, argv } = withJa(t);
  fs.mkdirSync(path.dirname(argv), { recursive: true });
  fs.writeFileSync(argv, '{\n    // comment only\n}\n');
  assert.equal(run(userData, extensions, '--locale', 'ja').status, 0);
  assert.deepEqual(parseJsonc(fs.readFileSync(argv, 'utf8')), { locale: 'ja' });
  fs.writeFileSync(argv, '{ "enable-crash-reporter": false }');
  assert.equal(run(userData, extensions, '--locale', 'ja').status, 0);
  assert.deepEqual(parseJsonc(fs.readFileSync(argv, 'utf8')), { locale: 'ja', 'enable-crash-reporter': false });
});

test('--locale en needs no language pack; other locales do', (t) => {
  const { userData, extensions, argv } = tempDir(t);
  const a = path.join(userData, 'User', 'argv.json');
  assert.equal(run(userData, extensions, '--locale', 'en').status, 0);
  assert.equal(parseJsonc(fs.readFileSync(a, 'utf8')).locale, 'en');
  const r = run(userData, extensions, '--locale', 'fr');
  assert.equal(r.status, 1);
  assert.match(r.stderr, /no language pack for fr is installed/);
  assert.equal(parseJsonc(fs.readFileSync(a, 'utf8')).locale, 'en');
});

test('--locale resolves a regional tag to its language pack and rejects malformed tags', (t) => {
  const { userData, extensions, argv } = withJa(t);
  assert.equal(run(userData, extensions, '--locale', 'ja-JP').status, 0);
  assert.equal(parseJsonc(fs.readFileSync(argv, 'utf8')).locale, 'ja-jp');
  assert.equal(run(userData, extensions, '--locale', 'ja"; rm').status, 2);
  assert.equal(run(userData, extensions, '--locale').status, 2);
});
