#!/usr/bin/env bash
# Generates a self-signed TLS cert/key pair for running Litepod with SSL.
#
# This is for testing/dev use, or as a stopgap on a host with no reverse
# proxy / no cert from a real CA (Let's Encrypt, your org's PKI, etc). A
# self-signed cert isn't validated by any client trust store, so browsers
# and most HTTP clients will refuse it (or need -k/--insecure) unless you
# also distribute the cert to whatever's calling this API.
#
# The output key is a real private key — never commit it. scripts/certs/ is
# gitignored for exactly this reason; don't fight that by force-adding it.
#
# Usage:
#   scripts/generate-cert.sh [days] [common-name] [output-dir]
#
#   scripts/generate-cert.sh                              # 365 days, CN=localhost, ./scripts/certs
#   scripts/generate-cert.sh 730                           # 2 years, CN=localhost
#   scripts/generate-cert.sh 365 node1.internal            # custom CN/SAN
#   scripts/generate-cert.sh 365 node1.internal /etc/litepod/certs   # custom output dir (e.g. for install.sh)

set -euo pipefail

DAYS="${1:-365}"
CN="${2:-localhost}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${3:-$SCRIPT_DIR/certs}"
mkdir -p "$OUT_DIR"

CERT_FILE="$OUT_DIR/litepod.crt"
KEY_FILE="$OUT_DIR/litepod.key"

if [[ -f "$CERT_FILE" || -f "$KEY_FILE" ]]; then
  if [[ "${FORCE:-}" == "1" ]]; then
    confirm="y"
  else
    read -rp "$CERT_FILE / $KEY_FILE already exist — overwrite? [y/N] " confirm
  fi
  if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
    echo "Aborted."
    exit 1
  fi
fi

openssl req -x509 -nodes \
  -newkey rsa:4096 \
  -days "$DAYS" \
  -keyout "$KEY_FILE" \
  -out "$CERT_FILE" \
  -subj "/CN=$CN" \
  -addext "subjectAltName=DNS:$CN,DNS:localhost,IP:127.0.0.1"

chmod 600 "$KEY_FILE"
chmod 644 "$CERT_FILE"

echo
echo "Generated:"
echo "  $CERT_FILE"
echo "  $KEY_FILE"
echo
echo "Run Litepod with SSL:"
echo "  TLS_CERT_FILE=$CERT_FILE TLS_KEY_FILE=$KEY_FILE ./bin/litepod"
echo
echo "Or set them in /etc/litepod/litepod.env if running as a systemd service"
echo "(see scripts/litepod.service). Default port is 8443 when TLS is enabled"
echo "unless PORT / config.yaml's port is set."
