#!/usr/bin/env bash
set -euo pipefail

# On Git Bash / MSYS, arguments that look like "/O=..." get silently
# rewritten as Windows paths before openssl ever sees them. This opts out
# of that rewriting; it's a no-op on real Unix shells.
export MSYS_NO_PATHCONV=1

# Generates a local development CA and per-service leaf certificates for
# mutual TLS between hapto-api and hapto-crypto.
#
# Usage: ./scripts/gen-certs.sh
#
# Re-running this script wipes and regenerates everything in certs/ from
# scratch — it's a repeatable build step, not a one-time secret. certs/ is
# gitignored: nothing this script produces is ever committed. Real
# deployments must generate their own certs the same way (or via a real CA)
# and inject them however that environment manages secrets; these are for
# local dev only.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CERT_DIR="$ROOT_DIR/certs"
DAYS_CA=3650
DAYS_LEAF=825

rm -rf "$CERT_DIR"
mkdir -p "$CERT_DIR"
cd "$CERT_DIR"

echo "Generating CA..."
openssl genrsa -out ca.key 4096 >/dev/null 2>&1
openssl req -x509 -new -nodes -key ca.key -sha256 -days "$DAYS_CA" \
  -subj "/O=hapto local dev/CN=hapto local dev CA" \
  -out ca.crt

gen_leaf() {
  local name="$1" cn="$2" sans="$3" eku="$4"

  echo "Generating $name cert..."
  openssl genrsa -out "$name.key" 2048 >/dev/null 2>&1
  openssl req -new -key "$name.key" -subj "/O=hapto local dev/CN=$cn" -out "$name.csr"

  ext_file="$name.ext"
  printf 'basicConstraints = CA:FALSE\nkeyUsage = digitalSignature, keyEncipherment\nextendedKeyUsage = %s\nsubjectAltName = %s\n' \
    "$eku" "$sans" > "$ext_file"

  openssl x509 -req -in "$name.csr" -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out "$name.crt" -days "$DAYS_LEAF" -sha256 -extfile "$ext_file"

  rm -f "$name.csr" "$ext_file"
}

# hapto-crypto is the gRPC server: SANs cover both local dev (localhost) and
# docker-compose (the "hapto-crypto" service hostname).
gen_leaf hapto-crypto "hapto-crypto" "DNS:hapto-crypto,DNS:localhost,IP:127.0.0.1" "serverAuth"

# hapto-api is the gRPC client presenting its own cert during the handshake.
gen_leaf hapto-api "hapto-api" "DNS:hapto-api,DNS:localhost,IP:127.0.0.1" "clientAuth"

rm -f ca.srl

echo
echo "Done. Certs written to $CERT_DIR:"
echo "  ca.crt                          — trust root both services verify peers against"
echo "  hapto-crypto.crt / .key         — server cert/key for hapto-crypto"
echo "  hapto-api.crt / .key            — client cert/key for hapto-api"
