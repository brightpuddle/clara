#!/bin/bash
set -e

# build_bridge.sh: Builds the Swift ClaraBridge and packages it into an .app bundle.
# This script is used by GoReleaser to prepare the macOS-specific component.

BUILD_DIR="build/ClaraBridge.app/Contents/MacOS"
PLIST_DIR="build/ClaraBridge.app/Contents"

echo "Building Swift ClaraBridge..."
cd swift
swift build -c release

# Create bundle structure
mkdir -p "../$BUILD_DIR"
mkdir -p "../$PLIST_DIR"

# Copy binary and Info.plist
cp .build/release/ClaraBridge "../$BUILD_DIR/ClaraBridge"
cp Sources/ClaraBridge/Info.plist "../$PLIST_DIR/Info.plist"

# Ad-hoc sign for local use/testing (proper signing requires a Dev ID in a real CI)
codesign --force --deep --sign - "../build/ClaraBridge.app"

# Create a self-contained wrapper script that knows where the .app is.
# This avoids needing complex Homebrew Ruby blocks.
cat <<EOF > "../build/ClaraBridge"
#!/bin/bash
# ClaraBridge wrapper for Homebrew
DIR="\$( cd "\$( dirname "\${BASH_SOURCE[0]}" )" && pwd )"
# If installed via Cask, the .app is in the same directory as the binary
exec "\$DIR/ClaraBridge.app/Contents/MacOS/ClaraBridge" "\$@"
EOF
chmod +x "../build/ClaraBridge"

echo "ClaraBridge built and packaged in build/ClaraBridge.app"
