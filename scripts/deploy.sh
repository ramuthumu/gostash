#!/usr/bin/env bash
#
# Deploy gostash to a remote server over SSH as a systemd service.
#
# Usage:
#   scripts/deploy.sh user@host [--arch amd64|arm64] [--remote-dir /opt/gostash]
#
# Assumes:
#   - you can SSH to the host with key-based auth (no password prompt)
#   - the server is Linux with systemd
#
# What it does:
#   1. Cross-compiles a static Linux binary for the target arch
#   2. scp's it to the server along with the systemd unit
#   3. Creates a gostash user + data dir if needed
#   4. Installs and (re)starts the systemd service, listening on 127.0.0.1:8090
#
# Put a reverse proxy (Caddy/nginx) in front for HTTPS and to expose :80 -> :8090.

set -euo pipefail

REMOTE="${1:?usage: deploy.sh user@host [--arch amd64|arm64] [--remote-dir /opt/gostash]}"
shift

ARCH="amd64"
REMOTE_DIR="/opt/gostash"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --arch) ARCH="$2"; shift 2 ;;
    --remote-dir) REMOTE_DIR="$2"; shift 2 ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

case "$ARCH" in
  amd64) GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> Building static Linux/$GOARCH binary"
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o "$TMP/gostash" .

echo "==> Preparing remote $REMOTE:$REMOTE_DIR"
ssh "$REMOTE" "sudo mkdir -p $REMOTE_DIR && sudo chown -R \$USER:\$USER $REMOTE_DIR"

echo "==> Copying binary and unit"
scp "$TMP/gostash" "$REMOTE:$REMOTE_DIR/gostash"

# Upload the systemd unit to a temp path on the remote, then install with sudo.
ssh "$REMOTE" "cat > /tmp/gostash.service" < deploy/gostash.service

echo "==> Installing service"
ssh "$REMOTE" 'set -e
  sudo install -d -o gostash -g gostash /var/lib/gostash 2>/dev/null || sudo install -d /var/lib/gostash
  sudo install -m 0755 '"$REMOTE_DIR"'/gostash /usr/local/bin/gostash
  sudo install -m 0644 /tmp/gostash.service /etc/systemd/system/gostash.service
  id -u gostash >/dev/null 2>&1 || sudo useradd --system --home /var/lib/gostash --shell /usr/sbin/nologin gostash
  sudo chown -R gostash:gostash /var/lib/gostash
  sudo systemctl daemon-reload
  sudo systemctl enable --now gostash
  sudo systemctl restart gostash
  sleep 1
  systemctl --no-pager --full status gostash | head -12 || true
  echo "--- listening on ---"; ss -ltnp 2>/dev/null | grep 8090 || true'

echo
echo "==> Done. gostash should be running on http://127.0.0.1:8090 on $REMOTE"
echo "    Put a reverse proxy (Caddy/nginx) in front for public HTTPS."