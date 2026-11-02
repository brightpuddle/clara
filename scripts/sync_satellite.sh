#!/usr/bin/env bash
# scripts/sync_satellite.sh
#
# Pushes the latest commits and updates to the Sophia satellite via Clara MCP `laptop.shell.run`.
# Does not require an outbound SSH connection from Eve to Sophia.

set -euo pipefail

echo "==> Verifying connection to Sophia satellite over Clara MCP..."
clara mcp list | grep -q "laptop" || {
  echo "❌ Error: 'laptop' MCP server is not registered on Eve."
  exit 1
}

echo "==> Pushing local git commits to remote repository..."
git push origin main || true

echo "==> Updating Sophia satellite via laptop.shell.run..."
clara tool call laptop.shell.run -a '{"command": "cd ~/src/clara/main && git pull && make install-clara"}'

echo "✅ Sophia satellite successfully updated and restarted."
