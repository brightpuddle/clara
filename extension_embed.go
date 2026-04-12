package clara

import "embed"

// ExtensionFS contains the Chrome extension source files.
//
//go:embed all:extension
var ExtensionFS embed.FS
