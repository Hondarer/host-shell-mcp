#!/bin/bash
# Install code-server from the kit on this (target) machine, create its configuration, and on the
# first install inject the default settings and extensions. It never connects to the network:
# the release archive and extensions come from packages/ in the kit (see download.sh).
# Usage: install.sh [--extension FILE.vsix|ID]

set -euo pipefail
# Every location is under HOME; inside containers it may be unset or read-only for the exec user.
if [[ -z "${HOME:-}" || ! -d "${HOME}" || ! -w "${HOME}" ]]; then
  echo "error: HOME (${HOME:-not set}) must be a writable directory" >&2
  exit 1
fi
# shellcheck source=../lib/common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"

extension=""
case "${1:-}" in
  "") ;;
  --extension)
    [[ $# -eq 2 ]] || die "usage: $0 [--extension FILE.vsix|ID]"
    extension="$2"
    ;;
  *) die "usage: $0 [--extension FILE.vsix|ID]" ;;
esac

# python3, openssl and ss are deliberately not required: minimal closed environments often lack them.
require_cmd tar gzip sha256sum od uname tr
type="$(linux_type)" || die "unsupported architecture: $(uname -m) (supported: x86_64, aarch64)"
# The bundled node is linked against glibc; checking its loader first avoids sending and extracting
# the archive on musl-based systems, where node would fail with an obscure "not found".
loader="$(glibc_loader "${type}")"
[[ -e "${loader}" ]] || die "code-server needs glibc: ${loader} not found (musl-based systems such as Alpine are not supported)"

if code_server_installed; then
  echo "code-server ${CODE_SERVER_VERSION} is already installed: ${HSCS_INSTALL_DIR}"
else
  archive="${HSCS_PACKAGES_DIR}/$(asset_name "${type}")"
  [[ -f "${archive}" ]] || die "release archive not found: ${archive} (run download.sh ${type} on the client and send the kit)"
  file_sha256_matches "${archive}" "$(asset_sha256 "${type}")" || die "SHA-256 mismatch: ${archive}"
  if code_server_running; then
    die "code-server is running; stop it before reinstalling"
  fi
  mkdir -p "${HSCS_LIB_DIR}"
  tmp="$(mktemp -d "${HSCS_LIB_DIR}/.extract.XXXXXX")"
  trap 'rm -rf "${tmp}"' EXIT
  tar -C "${tmp}" -xzf "${archive}" || die "failed to extract ${archive}"
  [[ -d "${tmp}/code-server-${CODE_SERVER_VERSION}-${type}" ]] || die "unexpected archive layout: ${archive}"
  rm -rf "${HSCS_INSTALL_DIR}"
  mv "${tmp}/code-server-${CODE_SERVER_VERSION}-${type}" "${HSCS_INSTALL_DIR}"
  rm -rf "${tmp}"
  trap - EXIT
  echo "installed code-server ${CODE_SERVER_VERSION} (${type}): ${HSCS_INSTALL_DIR}"
fi

mkdir -p "${HSCS_USER_DATA_DIR}" "${HSCS_EXTENSIONS_DIR}" "${HSCS_CONFIG_DIR}"
chmod 700 "${HSCS_CONFIG_DIR}"
if [[ -f "${HSCS_CONFIG_FILE}" ]]; then
  echo "keeping the existing configuration: ${HSCS_CONFIG_FILE}"
else
  # The password is never printed: tool output stays in the agent's conversation. The agent copies
  # the configuration to the client with transfer_pull instead.
  password="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
  (
    umask 077
    printf '%s\n' \
      "bind-addr: 127.0.0.1:${CODE_SERVER_PORT_FIRST}" \
      "auth: password" \
      "password: ${password}" \
      "cert: false" \
      "disable-telemetry: true" \
      "disable-update-check: true" >"${HSCS_CONFIG_FILE}"
  )
  chmod 600 "${HSCS_CONFIG_FILE}"
  echo "created the configuration with a new password: ${HSCS_CONFIG_FILE}"
fi

# Default settings are placed only where no file exists, so changes made in code-server survive.
for scope in User Machine; do
  target="${HSCS_USER_DATA_DIR}/${scope}/settings.json"
  if [[ -e "${target}" ]]; then
    echo "keeping existing ${scope}/settings.json"
  else
    mkdir -p "${HSCS_USER_DATA_DIR}/${scope}"
    cp "${HSCS_DEFAULTS_DIR}/${scope}/settings.json" "${target}"
    echo "installed default ${scope}/settings.json"
  fi
done

# Default extensions are installed once: the marker keeps extensions the user removed from
# coming back. It is created last, so a failed run is retried by the next install.
if [[ -e "${HSCS_MARKER}" ]]; then
  echo "default extensions were already applied (remove ${HSCS_MARKER} to apply them again)"
else
  dir="${HSCS_PACKAGES_DIR}/extensions/${type}"
  list="${dir}/resolved-extensions.txt"
  [[ -f "${list}" ]] || die "extension list not found: ${list} (run download.sh ${type} on the client and send the kit)"
  installed="$(code_server_cli --list-extensions)"
  ids=""
  while read -r spec platform sha file || [[ -n "${spec}" ]]; do
    [[ -z "${spec}" || "${spec}" == \#* ]] && continue
    id="${spec%@*}"
    ids="${ids} ${id}"
    if printf '%s\n' "${installed}" | grep -Fqix "${id}"; then
      echo "default extension already installed: ${id}"
      continue
    fi
    file_sha256_matches "${dir}/${file}" "${sha}" || die "SHA-256 mismatch or missing file: ${dir}/${file}"
    code_server_cli --install-extension "${dir}/${file}" --force >/dev/null
    echo "installed default extension: ${spec} (${platform})"
  done <"${list}"
  installed="$(code_server_cli --list-extensions)"
  for id in ${ids}; do
    printf '%s\n' "${installed}" | grep -Fqix "${id}" || die "default extension is not installed: ${id}"
  done
  : >"${HSCS_MARKER}"
  echo "default extensions applied"
fi

if [[ -n "${extension}" ]]; then
  code_server_cli --install-extension "${extension}" --force
fi

echo "configuration: ${HSCS_CONFIG_FILE}"
echo "version: $(code_server_cli --version | head -n 1)"
