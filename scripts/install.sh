#!/usr/bin/env bash
set -euo pipefail

REPO="liwaisi-tech/liwaisi_assistant"
BINARY="liwaisi"
INSTALL_DIR="${HOME}/.local/bin"

usage() {
    cat <<EOF
Usage: install.sh [OPTIONS]

Install the liwaisi CLI binary to \$HOME/.local/bin.

Options:
  --version VERSION   Install a specific version (e.g. 0.1.0-beta.1)
                      Defaults to the latest release.
  --help              Show this help message.

Examples:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash -s -- --version 0.1.0-beta.1
EOF
}

log()   { printf '  %s\n' "$*"; }
info()  { printf '\033[1;34m==> %s\033[0m\n' "$*"; }
error() { printf '\033[1;31mError: %s\033[0m\n' "$*" >&2; exit 1; }

detect_os() {
    local os
    os="$(uname -s)"
    case "${os}" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        *)       error "Unsupported operating system: ${os}. Only Linux and macOS are supported." ;;
    esac
}

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "${arch}" in
        x86_64|amd64)   echo "amd64" ;;
        aarch64|arm64)  echo "arm64" ;;
        *)              error "Unsupported architecture: ${arch}. Only amd64 and arm64 are supported." ;;
    esac
}

latest_version() {
    local url="https://api.github.com/repos/${REPO}/releases?per_page=1"
    local tag

    if command -v curl >/dev/null 2>&1; then
        tag="$(curl -fsSL "${url}" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"
    elif command -v wget >/dev/null 2>&1; then
        tag="$(wget -qO- "${url}" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"//;s/".*//')"
    else
        error "curl or wget is required to download releases."
    fi

    if [ -z "${tag}" ]; then
        error "Could not determine the latest release version. Check https://github.com/${REPO}/releases"
    fi

    echo "${tag#v}"
}

download() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "${dest}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${dest}" "${url}"
    else
        error "curl or wget is required to download releases."
    fi
}

verify_checksum() {
    local archive="$1" checksums="$2"
    local filename
    filename="$(basename "${archive}")"

    local expected
    expected="$(grep "${filename}" "${checksums}" | awk '{print $1}')"

    if [ -z "${expected}" ]; then
        error "Checksum not found for ${filename} in checksums file."
    fi

    local actual
    if command -v sha256sum >/dev/null 2>&1; then
        actual="$(sha256sum "${archive}" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
        actual="$(shasum -a 256 "${archive}" | awk '{print $1}')"
    else
        error "sha256sum or shasum is required for checksum verification."
    fi

    if [ "${actual}" != "${expected}" ]; then
        error "Checksum mismatch for ${filename}.\n  Expected: ${expected}\n  Got:      ${actual}"
    fi

    log "Checksum verified."
}

main() {
    local version=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --version)
                shift
                [ $# -eq 0 ] && error "--version requires a value."
                version="$1"
                ;;
            --help)
                usage
                exit 0
                ;;
            *)
                error "Unknown option: $1. Use --help for usage."
                ;;
        esac
        shift
    done

    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"

    info "Detecting platform"
    log "OS: ${os}, Arch: ${arch}"

    if [ -z "${version}" ]; then
        info "Fetching latest release"
        version="$(latest_version)"
    fi
    log "Version: ${version}"

    local base_url="https://github.com/${REPO}/releases/download/v${version}"
    local archive_name="${BINARY}_${version}_${os}_${arch}.tar.gz"
    local archive_url="${base_url}/${archive_name}"
    local checksums_url="${base_url}/checksums.txt"

    local tmpdir
    tmpdir="$(mktemp -d)"
    trap 'rm -rf "${tmpdir}"' EXIT

    info "Downloading ${archive_name}"
    download "${archive_url}" "${tmpdir}/${archive_name}"
    download "${checksums_url}" "${tmpdir}/checksums.txt"

    info "Verifying checksum"
    verify_checksum "${tmpdir}/${archive_name}" "${tmpdir}/checksums.txt"

    info "Installing to ${INSTALL_DIR}"
    tar -xzf "${tmpdir}/${archive_name}" -C "${tmpdir}"
    mkdir -p "${INSTALL_DIR}"
    mv "${tmpdir}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
    chmod +x "${INSTALL_DIR}/${BINARY}"

    log "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"

    if ! echo "${PATH}" | tr ':' '\n' | grep -qx "${INSTALL_DIR}"; then
        printf '\n\033[1;33mNote:\033[0m %s is not in your PATH.\n' "${INSTALL_DIR}"
        printf 'Add it by appending this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):\n\n'
        printf '  export PATH="%s:$PATH"\n\n' "${INSTALL_DIR}"
    fi

    info "Done"
    "${INSTALL_DIR}/${BINARY}" version
}

main "$@"
