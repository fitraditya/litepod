#!/usr/bin/env bash
# Builds Litepod and installs it as a systemd service (see litepod.service).
#
# Usage:
#   sudo scripts/install.sh [--ssl] [--enable] [--node-id NAME] [-h|--help]
#
#   --ssl            Generate a self-signed cert (scripts/generate-cert.sh)
#                     and configure the service to serve HTTPS on :8443.
#                     For production, replace the generated cert under
#                     /etc/litepod/certs/ with one from a real CA afterward —
#                     nothing else needs to change.
#   --enable          Also `systemctl enable --now` the service once
#                      installed. Without this flag the script stops short of
#                      starting anything, so you can review the generated
#                      config first.
#   --node-id NAME    node_id to write into config.yaml (default: hostname).
#
# What it does:
#   1. Checks it's running as root (systemd unit install + binary in
#      /usr/local/bin require it).
#   2. Builds ./bin/litepod with `go build` (stripped, like the release
#      binaries — see .goreleaser.yaml) and installs it to
#      /usr/local/bin/litepod.
#   3. Installs config.yaml.example to /etc/litepod/config.yaml (unless one
#      already exists) with a freshly generated api_key — printed once at
#      the end, store it.
#   4. With --ssl: generates a self-signed cert/key under
#      /etc/litepod/certs/ and points TLS_CERT_FILE/TLS_KEY_FILE at it via
#      /etc/litepod/litepod.env.
#   5. Installs scripts/litepod.service to /etc/systemd/system/ and runs
#      `systemctl daemon-reload`.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BIN_DEST="/usr/local/bin/litepod"
CONF_DIR="/etc/litepod"
CONF_FILE="$CONF_DIR/config.yaml"
ENV_FILE="$CONF_DIR/litepod.env"
CERT_DIR="$CONF_DIR/certs"
UNIT_SRC="$SCRIPT_DIR/litepod.service"
UNIT_DEST="/etc/systemd/system/litepod.service"

USE_SSL=0
ENABLE_NOW=0
NODE_ID="$(hostname 2>/dev/null || echo node-1)"

usage() {
  sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ssl) USE_SSL=1; shift ;;
    --enable) ENABLE_NOW=1; shift ;;
    --node-id) NODE_ID="${2:?--node-id requires a value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

# --- root check ---
if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "This installs a systemd unit and a binary under /usr/local/bin — run it as root:" >&2
  echo "  sudo $0 $*" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go not found on PATH — install Go 1.25+ first, or build ./bin/litepod yourself and run this script's install steps manually." >&2
  exit 1
fi

if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemctl not found — this script targets systemd hosts." >&2
  exit 1
fi

# --- build ---
echo "==> Building litepod"
cd "$REPO_ROOT"
go build -trimpath -ldflags="-s -w" -o "$REPO_ROOT/bin/litepod" main.go

echo "==> Installing binary to $BIN_DEST"
install -m 755 "$REPO_ROOT/bin/litepod" "$BIN_DEST"

# --- config ---
mkdir -p "$CONF_DIR"
if [[ -f "$CONF_FILE" ]]; then
  echo "==> $CONF_FILE already exists, leaving it untouched"
else
  echo "==> Writing $CONF_FILE"
  API_KEY="$(openssl rand -hex 32)"
  cp "$REPO_ROOT/config.yaml.example" "$CONF_FILE"
  # config.yaml.example ships with a placeholder api_key and no node_id line
  # in the same shape every time, so a couple of targeted substitutions is
  # simpler and less fragile here than re-templating the whole file.
  sed -i \
    -e "s/^node_id:.*/node_id: \"$NODE_ID\"/" \
    -e "s/^api_key:.*/api_key: \"$API_KEY\"/" \
    "$CONF_FILE"
  GENERATED_API_KEY="$API_KEY"
fi
chmod 600 "$CONF_FILE"

# --- TLS ---
TLS_LINES=""
if [[ "$USE_SSL" -eq 1 ]]; then
  echo "==> Generating self-signed cert in $CERT_DIR"
  FORCE=1 "$SCRIPT_DIR/generate-cert.sh" 365 "$NODE_ID" "$CERT_DIR"
  TLS_LINES=$'TLS_CERT_FILE='"$CERT_DIR"$'/litepod.crt\nTLS_KEY_FILE='"$CERT_DIR"$'/litepod.key'
fi

if [[ -n "$TLS_LINES" ]]; then
  echo "==> Writing $ENV_FILE"
  printf '%s\n' "$TLS_LINES" > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
elif [[ ! -f "$ENV_FILE" ]]; then
  # Leave a template so the operator can add overrides (PORT, SENTRY_DSN,
  # etc.) later without needing to know the exact filename/location.
  printf '# Env var overrides for litepod.service — see scripts/litepod.service\n# TLS_CERT_FILE=\n# TLS_KEY_FILE=\n# PORT=\n' > "$ENV_FILE"
  chmod 600 "$ENV_FILE"
fi

# --- systemd unit ---
echo "==> Installing $UNIT_DEST"
install -m 644 "$UNIT_SRC" "$UNIT_DEST"
systemctl daemon-reload

echo
echo "Installed."
echo "  Binary:  $BIN_DEST"
echo "  Config:  $CONF_FILE"
[[ "$USE_SSL" -eq 1 ]] && echo "  TLS:     $CERT_DIR/litepod.{crt,key} (self-signed — replace with a real CA cert for production)"
echo "  Unit:    $UNIT_DEST"
if [[ -n "${GENERATED_API_KEY:-}" ]]; then
  echo
  echo "Generated api_key (store this, it won't be shown again): $GENERATED_API_KEY"
fi

if [[ "$ENABLE_NOW" -eq 1 ]]; then
  echo
  echo "==> Enabling and starting litepod"
  systemctl enable --now litepod
  echo "==> journalctl -u litepod -f  # to tail logs"
else
  echo
  echo "Review $CONF_FILE, then:"
  echo "  sudo systemctl enable --now litepod"
fi
