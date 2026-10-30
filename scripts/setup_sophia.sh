#!/usr/bin/env bash
# scripts/setup_sophia.sh
#
# Configures the MacBook Pro ("sophia") as a stateless VPN & Local Session Satellite
# for the Mac mini ("eve") Clara Brain.
#
# Usage (run on sophia after `git pull && make install`):
#   ./scripts/setup_sophia.sh <eve-ip-or-hostname>
# Example:
#   ./scripts/setup_sophia.sh eve.local

set -euo pipefail

EVE_HOST="${1:-}"
if [[ -z "$EVE_HOST" ]]; then
  echo "Usage: $0 <eve-lan-ip-or-hostname>"
  echo "Example: $0 eve.local   (or 192.168.86.x)"
  exit 1
fi

mkdir -p "$HOME/.config/clara/triggers"
CONFIG_FILE="$HOME/.config/clara/config.yaml"

if [[ -f "$CONFIG_FILE" ]]; then
  cp "$CONFIG_FILE" "${CONFIG_FILE}.bak.$(date +%s)"
  echo "Backed up existing $CONFIG_FILE"
fi

cat > "$CONFIG_FILE" <<'EOF'
# yaml-language-server: $schema=schemas/config.schema.json
# ~/.config/clara/config.yaml — Sophia (MacBook Pro) VPN & Session Satellite Config
# Note: Discord & Webex bots are intentionally omitted here so sophia does not
# compete with eve for single-host bot connections or duplicate trigger runs.

log_level: info

server:
  listen_addr: 127.0.0.1:4444
  shared_secret: ""

integrations:
  chrome_nathan:
    profile: nathan
  db:
    path: ~/.local/share/clara/data.db
  zk:
    vault_root: ${HOME}/notes
    index_path: ${HOME}/.local/share/clara/indexes/zk/index.db

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
  - ${HOME}/.go/bin
EOF

# Ensure no duplicate event/schedule triggers run on sophia (all triggers run on eve)
rm -f "$HOME/.config/clara/triggers/"*.yaml 2>/dev/null || true

echo "✅ Wrote satellite config to $CONFIG_FILE and cleared local triggers."
echo "Restarting local Clara satellite daemon on sophia..."
/usr/local/bin/clara agent stop >/dev/null 2>&1 || true
/usr/local/bin/clara agent start

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
ssh "${USER}@${EVE_HOST}" "/usr/local/bin/clara mcp add laptop --url http://127.0.0.1:14444/mcp --description 'Sophia (MacBook Pro) VPN & Tmux Satellite' --overwrite" || true

echo "🎉 Sophia is now connected to Eve! Run 'sync' in Discord #agents to see #sophia-* channels."
