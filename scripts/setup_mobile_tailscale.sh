#!/usr/bin/env bash
# scripts/setup_mobile_tailscale.sh
#
# Configures Tailscale Serve on Eve to expose Eve's local opencode service
# (port 49374 -> https://<eve-dns>) and Sophia's SSH-forwarded opencode service
# (port 14374 -> https://<eve-dns>:8443), enables CORS on Sophia for the Eve PWA,
# and prints one-time pairing links for mobile setup.

set -euo pipefail

TAILSCALE_BIN="${TAILSCALE_BIN:-$(command -v tailscale || echo /opt/homebrew/bin/tailscale)}"
CLARA_BIN="${CLARA_BIN:-$(command -v clara || echo /usr/local/bin/clara)}"

if [[ ! -x "$TAILSCALE_BIN" ]]; then
  echo "error: tailscale binary not found (install with 'brew install tailscale')" >&2
  exit 1
fi

status_json="$("$TAILSCALE_BIN" status --json 2>/dev/null || echo '{}')"
backend_state="$(jq -r '.BackendState // ""' <<<"$status_json")"

if [[ "$backend_state" != "Running" ]]; then
  echo "Tailscale is not running (state: ${backend_state:-unknown}); attempting 'sudo tailscale up --hostname=eve'..."
  sudo "$TAILSCALE_BIN" up --hostname=eve --timeout=10s || true
  status_json="$("$TAILSCALE_BIN" status --json 2>/dev/null || echo '{}')"
  backend_state="$(jq -r '.BackendState // ""' <<<"$status_json")"
fi

if [[ "$backend_state" != "Running" ]]; then
  auth_url="$(jq -r '.AuthURL // ""' <<<"$status_json")"
  echo "Tailscale still requires login (state: ${backend_state:-unknown})." >&2
  if [[ -n "$auth_url" ]]; then
    echo "Authorize Eve in your browser: $auth_url" >&2
  else
    echo "Run: sudo $TAILSCALE_BIN up --hostname=eve" >&2
  fi
  exit 1
fi

DNS_NAME="$(jq -r '.Self.DNSName // "" | rtrimstr(".")' <<<"$status_json")"
if [[ -z "$DNS_NAME" ]]; then
  echo "error: could not determine Tailscale MagicDNS name from 'tailscale status --json'" >&2
  exit 1
fi

EVE_URL="https://${DNS_NAME}"
SOPHIA_URL="https://${DNS_NAME}:8443"

echo "Configuring Tailscale Serve on ${DNS_NAME}..."
sudo "$TAILSCALE_BIN" serve --bg --yes 49374
sudo "$TAILSCALE_BIN" serve --bg --yes --https=8443 14374

echo ""
echo "Current Tailscale Serve status:"
"$TAILSCALE_BIN" serve status

echo ""
echo "Configuring CORS on Sophia (${SOPHIA_URL} <- ${EVE_URL})..."
sophia_cors_cmd=$(cat <<EOF
umask 077
mkdir -p "\$HOME/.config/opencode"
if [ ! -f "\$HOME/.config/opencode/service.json" ]; then
  echo '{}' > "\$HOME/.config/opencode/service.json"
  chmod 600 "\$HOME/.config/opencode/service.json"
fi
if jq -e --arg origin "${EVE_URL}" '(.cors // []) | index(\$origin) != null' "\$HOME/.config/opencode/service.json" >/dev/null 2>&1; then
  echo "CORS already configured for ${EVE_URL}"
else
  tmp=\$(mktemp "\$HOME/.config/opencode/service.json.XXXXXX")
  jq --arg origin "${EVE_URL}" '.cors = ((.cors // []) + [\$origin] | unique)' "\$HOME/.config/opencode/service.json" > "\$tmp"
  chmod 600 "\$tmp"
  mv "\$tmp" "\$HOME/.config/opencode/service.json"
  opencode service restart
  echo "Updated CORS and restarted Sophia opencode service."
fi
EOF
)
cors_payload="$(jq -n --arg cmd "$sophia_cors_cmd" '{command: $cmd}')"
if ! "$CLARA_BIN" tool call laptop.shell.run -a "$cors_payload"; then
  echo "warning: could not reach Sophia via laptop.shell.run to configure CORS (connect Sophia and re-run)." >&2
fi

echo ""
echo "=== Eve pairing link (${EVE_URL}) ==="
opencode pair --url "$EVE_URL"

echo ""
echo "=== Sophia pairing link (${SOPHIA_URL}) ==="
sophia_pair_payload="$(jq -n --arg cmd "opencode pair --url \"${SOPHIA_URL}\"" '{command: $cmd}')"
if ! "$CLARA_BIN" tool call laptop.shell.run -a "$sophia_pair_payload"; then
  echo "warning: could not reach Sophia via laptop.shell.run for pairing link." >&2
fi

echo ""
echo "Next steps:"
echo "  1. Disable key expiry for 'eve' in Tailscale admin: https://login.tailscale.com/admin/machines"
echo "  2. Open Eve's pairing link on your phone (over Tailscale) and Add to Home Screen."
echo "  3. Optionally add Sophia (${SOPHIA_URL}) in the PWA using Sophia's pairing link or password."
