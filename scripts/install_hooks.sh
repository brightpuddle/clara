#!/usr/bin/env bash
# scripts/install_hooks.sh
#
# Dynamically installs and registers Antigravity (agy) hooks in ~/.gemini/config/hooks.json
# and ensures assistant scripts are placed in ~/.config/clara/scripts/assistant/.
# Safe to run on both Eve (hub) and Sophia (satellite).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"

DEST_SCRIPTS_DIR="$HOME/.config/clara/scripts/assistant"
GEMINI_CONFIG_DIR="$HOME/.gemini/config"
HOOKS_FILE="$GEMINI_CONFIG_DIR/hooks.json"

mkdir -p "$DEST_SCRIPTS_DIR"
mkdir -p "$GEMINI_CONFIG_DIR"

# 1. Install assistant scripts
if [ -d "$REPO_DIR/scripts/assistant" ]; then
  cp -f "$REPO_DIR/scripts/assistant/"*.ts "$DEST_SCRIPTS_DIR/"
  chmod +x "$DEST_SCRIPTS_DIR/"*.ts 2>/dev/null || true
  echo "✅ Installed assistant hook scripts to $DEST_SCRIPTS_DIR"
fi

# 2. Register hooks dynamically in ~/.gemini/config/hooks.json
BUN_BIN="$(which bun 2>/dev/null || echo "/opt/homebrew/bin/bun")"

cat > "$HOOKS_FILE" <<EOF
{
  "clara-discord-turn-notify": {
    "UserPromptSubmit": [
      {
        "type": "command",
        "command": "${BUN_BIN} run ${DEST_SCRIPTS_DIR}/agy_prompt_hook.ts",
        "timeout": 15
      }
    ],
    "Stop": [
      {
        "type": "command",
        "command": "${BUN_BIN} run ${DEST_SCRIPTS_DIR}/agy_stop_hook.ts",
        "timeout": 15
      }
    ]
  }
}
EOF

echo "✅ Registered Antigravity (agy) hooks in $HOOKS_FILE"
