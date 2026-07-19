#!/bin/sh
# Install script for nibs — https://github.com/alphaleonis/nibs
# Usage: curl -sSfL https://raw.githubusercontent.com/alphaleonis/nibs/main/install.sh | sh
#   or:  curl -sSfL https://raw.githubusercontent.com/alphaleonis/nibs/main/install.sh | sh -s -- -b /usr/local/bin v0.2.0

set -eu

REPO="alphaleonis/nibs"
BINARY="nibs"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

usage() {
  cat <<EOF
Usage: install.sh [options] [version]

Options:
  -b DIR    Install directory (default: ${DEFAULT_INSTALL_DIR})
  -h        Show this help

Examples:
  install.sh                    # latest release to ~/.local/bin
  install.sh v0.2.0             # specific version
  install.sh -b /usr/local/bin  # custom directory
EOF
  exit 0
}

fail() {
  echo "Error: $1" >&2
  exit 1
}

INSTALL_DIR="${DEFAULT_INSTALL_DIR}"

while getopts "b:h" opt; do
  case "$opt" in
    b) INSTALL_DIR="$OPTARG" ;;
    h) usage ;;
    *) usage ;;
  esac
done
shift $((OPTIND - 1))

VERSION="${1:-}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *) fail "unsupported OS: $OS" ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *) fail "unsupported architecture: $ARCH" ;;
esac

# Pick download tool
if command -v curl >/dev/null 2>&1; then
  download() { curl -sSfL -o "$1" "$2"; }
  fetch() { curl -sSfL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  download() { wget -qO "$1" "$2"; }
  fetch() { wget -qO- "$1"; }
else
  fail "curl or wget is required"
fi

# Resolve version
if [ -z "$VERSION" ]; then
  VERSION="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"tag_name":\s*"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || fail "could not determine latest version"
fi

echo "Installing ${BINARY} ${VERSION} (${OS}/${ARCH})"

# Build URLs. Asset names are version-less ({binary}_{os}_{arch}) and the
# checksum file is a fixed "checksums.txt" — see .goreleaser.yaml. This keeps
# them in lockstep with what `nibs upgrade` (go-selfupdate) expects.
TAG="${VERSION}"
EXT="tar.gz"
[ "$OS" = "windows" ] && EXT="zip"
ARCHIVE="${BINARY}_${OS}_${ARCH}.${EXT}"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
ARCHIVE_URL="${BASE_URL}/${ARCHIVE}"
CHECKSUMS_URL="${BASE_URL}/checksums.txt"

# Create temp directory with cleanup
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

# Download archive and checksums
echo "Downloading ${ARCHIVE_URL}"
download "${TMPDIR}/${ARCHIVE}" "${ARCHIVE_URL}"
download "${TMPDIR}/checksums.txt" "${CHECKSUMS_URL}"

# Verify checksum
EXPECTED="$(grep "${ARCHIVE}" "${TMPDIR}/checksums.txt" | awk '{print $1}')"
[ -n "$EXPECTED" ] || fail "checksum not found for ${ARCHIVE}"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMPDIR}/${ARCHIVE}" | awk '{print $1}')"
else
  echo "Warning: sha256sum/shasum not found, skipping checksum verification"
  ACTUAL="$EXPECTED"
fi

[ "$EXPECTED" = "$ACTUAL" ] || fail "checksum mismatch: expected ${EXPECTED}, got ${ACTUAL}"

# Extract
cd "$TMPDIR"
if [ "$EXT" = "zip" ]; then
  unzip -q "${ARCHIVE}" || fail "failed to extract archive"
else
  tar -xzf "${ARCHIVE}" || fail "failed to extract archive"
fi

# Install
mkdir -p "${INSTALL_DIR}"
BIN_NAME="${BINARY}"
[ "$OS" = "windows" ] && BIN_NAME="${BINARY}.exe"

cp "${TMPDIR}/${BIN_NAME}" "${INSTALL_DIR}/${BIN_NAME}"
chmod +x "${INSTALL_DIR}/${BIN_NAME}"

echo "Installed ${INSTALL_DIR}/${BIN_NAME}"

# Check PATH
case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) echo "Note: ${INSTALL_DIR} is not in your PATH. Add it with:"
     echo "  export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac
