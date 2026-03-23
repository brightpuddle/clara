#!/bin/bash
set -e

# Clara Installer
# This script installs Clara on macOS using Homebrew Cask.

echo "Clara Installer"
echo "==============="

# Check for macOS
if [[ "$OSTYPE" != "darwin"* ]]; then
    echo "Error: Clara currently only supports macOS."
    exit 1
fi

# Check for Homebrew
if ! command -v brew >/dev/null 2>&1; then
    echo "Homebrew is not installed. Installing Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi

echo "Updating Homebrew..."
brew update

echo "Adding brightpuddle tap..."
brew tap brightpuddle/homebrew-tap || true

echo "Installing Clara (Cask)..."
brew install --cask clara

echo ""
echo "Clara has been installed successfully!"
echo ""
echo "Next steps:"
echo "1. Run 'clara paths' to see important file locations."
echo "2. Copy the example config to the default location:"
echo "   mkdir -p ~/.config/clara"
echo "   cp $(brew --prefix --cask clara)/config.yaml.example ~/.config/clara/config.yaml"
echo "3. Load the Chrome extension from:"
echo "   $(clara paths | grep Extension | awk '{print $2}')"
echo "4. Start the Clara agent:"
echo "   cp $(brew --prefix --cask clara)/com.brightpuddle.clara.agent.plist ~/Library/LaunchAgents/"
echo "   launchctl load ~/Library/LaunchAgents/com.brightpuddle.clara.agent.plist"
echo ""
echo "Run 'clara --help' to get started."
