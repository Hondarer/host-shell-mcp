// Register installed language pack extensions in <user-data>/languagepacks.json, as the running
// server does when it installs an extension itself. Extensions installed with the CLI
// (--install-extension) are not registered there, and the server serves translations only for
// registered packs, so without this the display language stays English until a language pack is
// installed again from the marketplace, which needs network access.
// With --locale, it also sets the display language in <user-data>/User/argv.json; any locale
// other than "en" must have a registered language pack.
// Usage: node language-packs.mjs <user-data-dir> <extensions-dir> [--locale LOCALE]
// Run it with the node bundled with code-server; it uses no network and no other tools.

import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const usage = "usage: node language-packs.mjs <user-data-dir> <extensions-dir> [--locale LOCALE]";
const args = process.argv.slice(2);
const [userDataDir, extensionsDir] = args;
let locale;
if (args.length === 4 && args[2] === "--locale") {
  locale = args[3];
} else if (args.length !== 2) {
  console.error(usage);
  process.exit(2);
}
if (!userDataDir || !extensionsDir) {
  console.error(usage);
  process.exit(2);
}
// An IETF language tag as VS Code uses it, such as "ja", "zh-cn", or "pt-br".
if (locale !== undefined && !/^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$/.test(locale)) {
  console.error(`invalid locale: ${locale}`);
  process.exit(2);
}

function readJson(file, fallback) {
  try {
    return JSON.parse(readFileSync(file, "utf8"));
  } catch (error) {
    if (error.code === "ENOENT") return fallback;
    throw new Error(`cannot read ${file}: ${error.message}`);
  }
}

// extensions.json lists what the extension management service considers installed.
function installedExtensions() {
  const entries = readJson(join(extensionsDir, "extensions.json"), []);
  if (!Array.isArray(entries)) throw new Error(`unexpected format: ${join(extensionsDir, "extensions.json")}`);
  const result = [];
  for (const entry of entries) {
    const id = entry?.identifier?.id;
    const relativeLocation = entry?.relativeLocation;
    let location;
    if (typeof relativeLocation === "string") {
      location = resolve(extensionsDir, relativeLocation);
    } else if (entry?.location?.scheme === "file" && typeof entry.location.path === "string") {
      location = entry.location.path;
    } else if (typeof entry?.location === "string" && entry.location.startsWith("file:")) {
      location = fileURLToPath(entry.location);
    }
    if (typeof id !== "string" || !location) continue;
    // Only extensions inside the extensions directory are trusted.
    const rel = relative(resolve(extensionsDir), location);
    if (rel === "" || rel.startsWith("..") || isAbsolute(rel)) continue;
    const manifest = readJson(join(location, "package.json"), null);
    if (manifest) result.push({ id, uuid: entry.identifier.uuid, location, manifest });
  }
  return result;
}

// Same shape and hash as the server's language pack cache (LanguagePacksCache).
function languagePacks(extensions) {
  const packs = {};
  for (const ext of extensions) {
    const localizations = ext.manifest?.contributes?.localizations;
    if (!Array.isArray(localizations)) continue;
    for (const loc of localizations) {
      if (typeof loc?.languageId !== "string" || !Array.isArray(loc.translations)) continue;
      const pack = (packs[loc.languageId] ??= {
        hash: "",
        extensions: [],
        translations: {},
        label: loc.localizedLanguageName ?? loc.languageName,
      });
      if (!pack.extensions.some((e) => e.extensionIdentifier.id.toLowerCase() === ext.id.toLowerCase())) {
        const identifier = ext.uuid ? { id: ext.id, uuid: ext.uuid } : { id: ext.id };
        pack.extensions.push({ extensionIdentifier: identifier, version: ext.manifest.version });
      }
      for (const t of loc.translations) {
        if (typeof t?.id === "string" && typeof t?.path === "string") pack.translations[t.id] = join(ext.location, t.path);
      }
    }
  }
  for (const pack of Object.values(packs)) {
    const md5 = createHash("md5");
    for (const e of pack.extensions) md5.update(e.extensionIdentifier.uuid || e.extensionIdentifier.id).update(e.version);
    pack.hash = md5.digest("hex");
  }
  return packs;
}

function writeAtomic(file, text) {
  const tmp = `${file}.tmp-${process.pid}`;
  writeFileSync(tmp, text);
  renameSync(tmp, file);
}

// The server resolves a locale such as "zh-tw" to a pack by trying "zh-tw", then "zh".
function packFor(packs, tag) {
  for (let t = tag.toLowerCase(); t; t = t.includes("-") ? t.slice(0, t.lastIndexOf("-")) : "") {
    if (packs[t]) return t;
  }
  return undefined;
}

// argv.json is JSON with comments and is also edited by "Configure Display Language", so only the
// "locale" property is replaced or added; other content and comments are kept.
function setLocale(file, value) {
  let text;
  try {
    text = readFileSync(file, "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  const entry = `"locale": ${JSON.stringify(value)}`;
  if (text === undefined || text.trim() === "") {
    text = `{\n    // Display language; change it with "Configure Display Language".\n    ${entry}\n}\n`;
  } else if (/"locale"\s*:\s*"[^"]*"/.test(text)) {
    text = text.replace(/"locale"\s*:\s*"[^"]*"/, entry);
  } else {
    const open = text.indexOf("{");
    if (open < 0) throw new Error(`unexpected format: ${file}`);
    const rest = text.slice(open + 1);
    const empty = /^\s*}/.test(rest.replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, ""));
    text = `${text.slice(0, open + 1)}\n    ${entry}${empty ? "" : ","}${rest}`;
  }
  mkdirSync(dirname(file), { recursive: true });
  writeAtomic(file, text);
}

const packs = languagePacks(installedExtensions());
if (locale !== undefined && locale.toLowerCase() !== "en" && !locale.toLowerCase().startsWith("en-") && !packFor(packs, locale)) {
  console.error(`no language pack for ${locale} is installed; add it to defaults/extensions.txt, run download, and send the kit again`);
  process.exit(1);
}
writeAtomic(join(userDataDir, "languagepacks.json"), JSON.stringify(packs));
const ids = Object.keys(packs).sort();
console.log(ids.length ? `registered language packs: ${ids.join(", ")}` : "no language packs installed");
if (locale !== undefined) {
  setLocale(join(userDataDir, "User", "argv.json"), locale.toLowerCase());
  console.log(`display language: ${locale.toLowerCase()}`);
}
