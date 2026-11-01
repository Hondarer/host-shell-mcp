#!/usr/bin/env python3
"""Resolve and fetch default code-server extensions from Open VSX for one target type.

Each line of the list names an extension as publisher.name or publisher.name@version; text after
# is a comment. For every extension this picks the target platform's build (linux-x64,
linux-arm64 or win32-x64), or the universal build when the extension has no platform builds,
skipping pre-releases. Without @version it takes the newest version whose engines.vscode accepts
the given VS Code version. The VSIX is checked against Open VSX's SHA-256 and the results are
written to resolved-extensions.txt, which install verifies offline on the target.

Extensions that cannot be resolved are skipped with a warning and a comment line, so one list
serves every target type. Called by download.sh; uses only the standard library and honours
HTTPS_PROXY.
"""

import argparse
import hashlib
import json
import os
import re
import sys
import urllib.parse
import urllib.request

OPEN_VSX = "https://open-vsx.org"
PLATFORMS = {"linux-amd64": "linux-x64", "linux-arm64": "linux-arm64", "windows-amd64": "win32-x64"}


def version_key(text):
    """Orders x.y.z versions numerically; a pre-release suffix sorts below the release."""
    main, _, pre = text.partition("-")
    parts = [int(p) if p.isdigit() else 0 for p in main.split(".")]
    while len(parts) < 3:
        parts.append(0)
    return parts[:3], 0 if pre else 1


def satisfies(spec, version):
    """Reports whether version satisfies an engines.vscode range such as ^1.92.0 or >=1.80.0."""
    spec = spec.strip()
    if spec in ("", "*"):
        return True
    m = re.fullmatch(r"(\^|~|>=|>|<=|<|=)?\s*v?(\d+)(?:\.(\d+|x|\*))?(?:\.(\d+|x|\*))?(?:-[\w.]+)?", spec)
    if not m:
        return False
    op = m.group(1) or "="
    want = [int(g) if g and g.isdigit() else 0 for g in m.group(2, 3, 4)]
    have = version_key(version)[0]
    if op == "^":
        # ^0.y.z pins the minor version, like npm's caret.
        same = have[0] == want[0] and (want[0] != 0 or have[1] == want[1])
        return same and have >= want
    if op == "~":
        return have[:2] == want[:2] and have >= want
    if op == ">=":
        return have >= want
    if op == ">":
        return have > want
    if op == "<=":
        return have <= want
    if op == "<":
        return have < want
    # A bare x.y.z, or x.y.x / x.y.* wildcards.
    wild = [g in ("x", "*") for g in m.group(3, 4)]
    return have[0] == want[0] and (wild[0] or have[1] == want[1]) and (wild[0] or wild[1] or have[2] == want[2])


def get(url):
    with urllib.request.urlopen(urllib.request.Request(url, headers={"User-Agent": "host-shell-code-server"}), timeout=60) as r:
        return r.read()


def query(ext_id, platform):
    url = OPEN_VSX + "/api/-/query?" + urllib.parse.urlencode(
        {"extensionId": ext_id, "includeAllVersions": "true", "targetPlatform": platform})
    return json.loads(get(url)).get("extensions", [])


def pick(entries, version, vscode):
    candidates = [e for e in entries if not e.get("preRelease")]
    if version:
        candidates = [e for e in candidates if e.get("version") == version]
    else:
        candidates = [e for e in candidates if satisfies((e.get("engines") or {}).get("vscode", ""), vscode)]
    candidates.sort(key=lambda e: version_key(e["version"]), reverse=True)
    return candidates[0] if candidates else None


def resolve(ext_id, version, platform, vscode):
    """Returns the chosen Open VSX entry, or None with the reason."""
    for target in (platform, "universal"):
        entries = query(ext_id, target)
        if entries:
            chosen = pick(entries, version, vscode)
            if chosen:
                return chosen, ""
            if version:
                return None, "version %s not found for %s" % (version, target)
            return None, "no version compatible with VS Code %s for %s" % (vscode, target)
    return None, "not found on Open VSX for %s" % platform


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--type", required=True, choices=sorted(PLATFORMS))
    ap.add_argument("--vscode", required=True, help="VS Code version of the code-server release")
    ap.add_argument("--list", required=True, help="extensions.txt")
    ap.add_argument("--out", required=True, help="output folder for the VSIX files and resolved-extensions.txt")
    args = ap.parse_args()
    platform = PLATFORMS[args.type]
    os.makedirs(args.out, exist_ok=True)

    lines, keep, skipped = [], set(), 0
    with open(args.list, encoding="utf-8") as f:
        specs = [line.split("#", 1)[0].strip() for line in f]
    for spec in filter(None, specs):
        ext_id, _, version = spec.partition("@")
        try:
            entry, reason = resolve(ext_id, version, platform, args.vscode)
        except Exception as e:  # network and API errors skip this extension only
            entry, reason = None, "query failed: %s" % e
        if not entry:
            print("warning: skipped %s: %s" % (spec, reason), file=sys.stderr)
            lines.append("# skipped %s: %s" % (spec, reason))
            skipped += 1
            continue
        files = entry["files"]
        name = os.path.basename(urllib.parse.unquote(urllib.parse.urlparse(files["download"]).path))
        path = os.path.join(args.out, name)
        try:
            expected = get(files["sha256"]).decode().split()[0].lower()
            if not (os.path.isfile(path) and sha256_file(path) == expected):
                partial = path + ".partial"
                with open(partial, "wb") as out:
                    out.write(get(files["download"]))
                if sha256_file(partial) != expected:
                    os.remove(partial)
                    raise ValueError("SHA-256 mismatch")
                os.replace(partial, path)
                print("fetched %s" % name)
            else:
                print("verified %s" % name)
        except Exception as e:
            print("warning: skipped %s: download failed: %s" % (spec, e), file=sys.stderr)
            lines.append("# skipped %s: download failed: %s" % (spec, e))
            skipped += 1
            continue
        ext = "%s.%s" % (entry["namespace"], entry["name"])
        lines.append("%s@%s %s %s %s" % (ext, entry["version"], entry["targetPlatform"], expected, name))
        keep.add(name)

    # Drop VSIX files of earlier resolutions, so the kit carries only what the list names.
    for name in os.listdir(args.out):
        if name.endswith(".vsix") and name not in keep:
            os.remove(os.path.join(args.out, name))
    with open(os.path.join(args.out, "resolved-extensions.txt"), "w", encoding="utf-8", newline="\n") as f:
        f.write("# Default extensions for %s (%s), VS Code %s\n" % (args.type, platform, args.vscode))
        f.write("# ID@VERSION PLATFORM SHA-256 FILE\n")
        f.write("\n".join(lines) + "\n")
    print("resolved %d extension(s) for %s, skipped %d: %s" % (len(keep), args.type, skipped, args.out))


if __name__ == "__main__":
    main()
