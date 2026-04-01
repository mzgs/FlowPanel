#!/usr/bin/env bash
set -euo pipefail

DOWNLOAD_URL="https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz"
VERSION_FILE=".flowpanel-version"

OS="$(uname -s)"
SUDO=""

case "$OS" in
  Linux*)
    FLOWPANEL_PATH="/var/flowpanel"
    IS_WINDOWS=0
    ;;
  Darwin*)
    FLOWPANEL_PATH="/Users/Shared/FlowPanel"
    IS_WINDOWS=0
    ;;
  MINGW*|MSYS*|CYGWIN*)
    FLOWPANEL_PATH="/c/FlowPanel"
    IS_WINDOWS=1
    ;;
  *)
    echo "Unsupported platform: $OS"
    exit 1
    ;;
esac

PHPMYADMIN_DIR="${FLOWPANEL_PATH}/phpmyadmin"

if [ "$IS_WINDOWS" -eq 0 ]; then
  if [ "$(id -u)" -ne 0 ] && command -v sudo >/dev/null 2>&1; then
    SUDO="sudo"
  fi
fi

run_maybe_sudo() {
  if [ -n "$SUDO" ]; then
    "$SUDO" "$@"
  else
    "$@"
  fi
}

download_file() {
  local url="$1"
  local output="$2"

  if command -v curl >/dev/null 2>&1; then
    curl -fL "$url" -o "$output"
    return
  fi

  if command -v wget >/dev/null 2>&1; then
    wget -O "$output" "$url"
    return
  fi

  echo "Error: curl or wget is required."
  exit 1
}

generate_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '\n'
    return
  fi

  if [ -r /dev/urandom ] && command -v base64 >/dev/null 2>&1; then
    dd if=/dev/urandom bs=32 count=1 2>/dev/null | base64 | tr -d '\n'
    return
  fi

  echo "Error: openssl is required to generate the phpMyAdmin blowfish secret."
  exit 1
}

replace_blowfish_secret() {
  local file="$1"
  local secret="$2"
  local tmp_file

  tmp_file="$(mktemp)"
  awk -v secret="$secret" '
    {
      gsub(/\$cfg\['\''blowfish_secret'\''\][[:space:]]*=[[:space:]]*'\'''\'';?/, "$cfg['\''blowfish_secret'\''] = '\''" secret "'\'';")
      print
    }
  ' "$file" > "$tmp_file"
  mv "$tmp_file" "$file"
}

echo "[1/8] Detecting platform..."
echo "Platform: $OS"
echo "FlowPanel path: $FLOWPANEL_PATH"

echo "[2/8] Verifying required tools..."
for tool in tar mktemp awk find sed; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Error: required tool '$tool' is not installed."
    exit 1
  fi
done

echo "[3/8] Creating FlowPanel path..."
run_maybe_sudo mkdir -p "$FLOWPANEL_PATH"

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
cd "$WORKDIR"

echo "[4/8] Downloading latest phpMyAdmin..."
download_file "$DOWNLOAD_URL" phpmyadmin.tar.gz

echo "[5/8] Extracting phpMyAdmin..."
tar -xzf phpmyadmin.tar.gz

EXTRACTED_DIR="$(find . -maxdepth 1 -type d -name 'phpMyAdmin-*-all-languages' | head -n 1)"

if [ -z "${EXTRACTED_DIR:-}" ]; then
  echo "Error: could not find extracted phpMyAdmin directory."
  exit 1
fi

VERSION="$(basename "$EXTRACTED_DIR" | sed -E 's/^phpMyAdmin-(.*)-all-languages$/\1/')"

echo "[6/8] Installing to ${PHPMYADMIN_DIR}..."
run_maybe_sudo rm -rf "$PHPMYADMIN_DIR"
run_maybe_sudo mv "$EXTRACTED_DIR" "$PHPMYADMIN_DIR"

echo "[7/8] Creating config..."
CONFIG_SAMPLE="${PHPMYADMIN_DIR}/config.sample.inc.php"
CONFIG_FILE="${PHPMYADMIN_DIR}/config.inc.php"
SECRET="$(generate_secret)"

TMP_CONFIG="$(mktemp)"
cp "$CONFIG_SAMPLE" "$TMP_CONFIG"
replace_blowfish_secret "$TMP_CONFIG" "$SECRET"

run_maybe_sudo cp "$TMP_CONFIG" "$CONFIG_FILE"
rm -f "$TMP_CONFIG"
printf '%s\n' "$VERSION" | run_maybe_sudo tee "${PHPMYADMIN_DIR}/${VERSION_FILE}" >/dev/null

echo "[8/8] Creating tmp directory and applying permissions..."
run_maybe_sudo mkdir -p "${PHPMYADMIN_DIR}/tmp"

if command -v chmod >/dev/null 2>&1; then
  run_maybe_sudo find "$PHPMYADMIN_DIR" -type d -exec chmod 755 {} +
  run_maybe_sudo find "$PHPMYADMIN_DIR" -type f -exec chmod 644 {} +
  run_maybe_sudo chmod 777 "${PHPMYADMIN_DIR}/tmp" 2>/dev/null || true
fi

echo
echo "phpMyAdmin installed successfully at:"
echo "  $PHPMYADMIN_DIR"
echo "Version:"
echo "  $VERSION"
echo
echo "FlowPanel will detect this path automatically."
