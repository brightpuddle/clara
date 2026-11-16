#!/usr/bin/env bash
# scripts/install_hooks.sh
#
# Installs assistant scripts into ~/.config/clara/scripts/assistant/ and cleans
# up legacy Antigravity (agy) hooks from ~/.gemini/config/hooks.json.
# Safe to run on both Eve (hub) and Sophia (satellite).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

DEST_SCRIPTS_DIR="$HOME/.config/clara/scripts/assistant"
GEMINI_CONFIG_DIR="$HOME/.gemini/config"
HOOKS_FILE="$GEMINI_CONFIG_DIR/hooks.json"

mkdir -p "$DEST_SCRIPTS_DIR"

# 1. Install assistant scripts and clean up removed agy hooks
if [ -d "$REPO_DIR/scripts/assistant" ]; then
  cp -Rf "$REPO_DIR/scripts/assistant/"* "$DEST_SCRIPTS_DIR/"
  rm -f "$DEST_SCRIPTS_DIR/agy_prompt_hook.ts" "$DEST_SCRIPTS_DIR/agy_stop_hook.ts"
  chmod +x "$DEST_SCRIPTS_DIR/"*.ts 2>/dev/null || true
  echo "✅ Installed assistant scripts to $DEST_SCRIPTS_DIR"
fi

# 2. Remove legacy agy hooks registration so agy runs standalone
rm -f "$HOOKS_FILE"
echo "✅ Cleaned up legacy agy hooks ($HOOKS_FILE)"
