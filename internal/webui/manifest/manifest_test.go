package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadManifest(t *testing.T) {
	tempDir := t.TempDir()
	manifestPath := filepath.Join(tempDir, "manifest.json")

	content := `{
  "src/app.ts": {
    "file": "assets/app-1234.js",
    "name": "app",
    "src": "src/app.ts",
    "isEntry": true,
    "css": [
      "assets/app-5678.css"
    ],
    "imports": [
      "_vendor-9012.js"
    ]
  },
  "_vendor-9012.js": {
    "file": "assets/vendor-9012.js",
    "css": [
      "assets/vendor-extra.css"
    ]
  }
}`

	if err := os.WriteFile(manifestPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write test manifest: %v", err)
	}

	m, err := ReadManifest(manifestPath)
	if err != nil {
		t.Fatalf("ReadManifest returned error: %v", err)
	}

	if m.File != "assets/app-1234.js" {
		t.Errorf("expected File 'assets/app-1234.js', got %q", m.File)
	}
	if len(m.CSS) != 2 || m.CSS[0] != "assets/app-5678.css" || m.CSS[1] != "assets/vendor-extra.css" {
		t.Errorf("unexpected CSS: %#v", m.CSS)
	}
	if len(m.Imports) != 1 || m.Imports[0] != "assets/vendor-9012.js" {
		t.Errorf("unexpected Imports: %#v", m.Imports)
	}
}
