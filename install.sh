#!/usr/bin/env bash
set -euo pipefail

# Copera CLI installer
# Usage: curl -fsSL https://cli.copera.ai/install.sh | bash

CDN="https://cli.copera.ai"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY="copera"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# Get latest version
VERSION="${VERSION:-$(curl -fsSL "${CDN}/version.json" | grep '"latest"' | sed 's/.*"latest"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/')}"

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version" >&2
  exit 1
fi

echo "Installing copera v${VERSION} (${OS}/${ARCH})..."

# Download
EXT="tar.gz"
[ "$OS" = "windows" ] && EXT="zip"
ASSET="${BINARY}-${VERSION}-${OS}-${ARCH}.${EXT}"
URL="${CDN}/v${VERSION}/${ASSET}"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

curl -fsSL "$URL" -o "${TMP}/archive.${EXT}"

# Extract
if [ "$EXT" = "zip" ]; then
  unzip -q "${TMP}/archive.zip" -d "$TMP"
else
  tar -xzf "${TMP}/archive.tar.gz" -C "$TMP"
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  echo "Need sudo to install to ${INSTALL_DIR}"
  sudo mv "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo "Installed copera v${VERSION} to ${INSTALL_DIR}/${BINARY}"
echo "Run 'copera auth login' to get started."
