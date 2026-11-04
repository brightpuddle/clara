#!/usr/bin/env bash
# scripts/setup_sophia.sh
#
# Configures the MacBook Pro ("sophia") as a stateless VPN & Local Session Satellite
# for the Mac mini ("eve") Clara Brain.
#
# Usage (run on sophia after `git pull && make install`):
#   ./scripts/setup_sophia.sh [eve-hostname]
# Defaults to `eve.brightpuddle.com` (using forwarded SSH port 22, which works
# whether sophia is on Cisco AnyConnect full-tunnel VPN, home Wi-Fi, or traveling).

set -euo pipefail

EVE_HOST="${1:-eve.brightpuddle.com}"

mkdir -p "$HOME/.config/clara/triggers"
CONFIG_FILE="$HOME/.config/clara/config.yaml"

if [[ -f "$CONFIG_FILE" ]]; then
  cp "$CONFIG_FILE" "${CONFIG_FILE}.bak.$(date +%s)"
  echo "Backed up existing $CONFIG_FILE"
fi

# Query active primary platform from Eve so Sophia respects Eve's current platform definition
echo "Querying active primary agent platform from Eve (${EVE_HOST})..."
EVE_PLATFORM="$(ssh -o BatchMode=yes -o ConnectTimeout=5 "${USER}@${EVE_HOST}" "grep -E '^primary_agent_platform:' ~/.config/clara/config.yaml | awk '{print \$2}'" 2>/dev/null || echo "")"
if [[ -z "$EVE_PLATFORM" ]]; then
  # Fall back to existing local definition if available, otherwise default to discord
  if [[ -f "$CONFIG_FILE" ]]; then
    EVE_PLATFORM="$(grep -E '^primary_agent_platform:' "$CONFIG_FILE" | awk '{print $2}' || echo "")"
  fi
  if [[ -z "$EVE_PLATFORM" ]]; then
    EVE_PLATFORM="discord"
  fi
fi
echo "Active primary platform on Eve: ${EVE_PLATFORM}"

cat > "$CONFIG_FILE" <<EOF
# yaml-language-server: \$schema=schemas/config.schema.json
# ~/.config/clara/config.yaml — Sophia (MacBook Pro) VPN & Session Satellite Config
# Note: Discord & Webex bots are intentionally omitted here so sophia does not
# compete with eve for single-host bot connections or duplicate trigger runs.

log_level: info

primary_agent_platform: ${EVE_PLATFORM}

server:
  listen_addr: 127.0.0.1:4444
  shared_secret: ""

integrations:
  chrome_nathan:
    profile: nathan
  db:
    path: ~/.local/share/clara/data.db
  zk:
    vault_root: \${HOME}/notes
    index_path: \${HOME}/.local/share/clara/indexes/zk/index.db

plugins:
  - name: chrome
  - name: chrome_nathan
  - name: task
  - name: tmux
  - name: web
  - name: zk
  - name: macos
    path: /usr/local/libexec/ClaraBridge.app/Contents/MacOS/ClaraBridge

mcp_command_search_paths:
  - /opt/homebrew/bin
  - /usr/local/bin
  - \${HOME}/.go/bin
EOF

# Ensure no duplicate event/schedule triggers run on sophia (all triggers run on eve)
rm -f "$HOME/.config/clara/triggers/"*.yaml 2>/dev/null || true

echo "✅ Wrote satellite config to $CONFIG_FILE and cleared local triggers."
echo "Restarting local Clara satellite daemon on sophia..."
/usr/local/bin/clara stop >/dev/null 2>&1 || true
/usr/local/bin/clara start

TUNNEL_PLIST="$HOME/Library/LaunchAgents/com.brightpuddle.clara.vpn-tunnel.plist"

cat > "$TUNNEL_PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.brightpuddle.clara.vpn-tunnel</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/bin/ssh</string>
        <string>-NT</string>
        <string>-o</string>
        <string>ServerAliveInterval=15</string>
        <string>-o</string>
        <string>ServerAliveCountMax=3</string>
        <string>-o</string>
        <string>ExitOnForwardFailure=yes</string>
        <string>-R</string>
        <string>14444:127.0.0.1:4444</string>
        <string>${USER}@${EVE_HOST}</string>
    </array>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
</dict>
</plist>
EOF

launchctl unload "$TUNNEL_PLIST" 2>/dev/null || true
launchctl load "$TUNNEL_PLIST"

echo "✅ Outbound SSH reverse tunnel LaunchAgent started (sophia:4444 -> ${EVE_HOST}:14444)."
echo "Registering 'laptop' MCP server on eve (${EVE_HOST})..."
ssh "${USER}@${EVE_HOST}" "/usr/local/bin/clara mcp remove laptop >/dev/null 2>&1 || true; /usr/local/bin/clara mcp add laptop --url http://127.0.0.1:14444/mcp --description 'Sophia (MacBook Pro) VPN & Tmux Satellite'" || true

if [[ "${EVE_PLATFORM}" == "webex" ]]; then
  SYNC_HINT="Run 'sync' in Webex to see [Clara:sophia] spaces."
else
  SYNC_HINT="Run 'sync' in Discord #agents to see #sophia-* channels."
fi
echo "🎉 Sophia is now connected to Eve! ${SYNC_HINT}"
