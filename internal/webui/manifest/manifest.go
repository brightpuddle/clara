package manifest

import (
	"encoding/json"
	"os"
	"slices"

	"github.com/cockroachdb/errors"
)

type manifestRecord struct {
	File    string   `json:"file"`
	IsEntry bool     `json:"isEntry"`
	CSS     []string `json:"css"`
	Imports []string `json:"imports"`
}

// Manifest holds Vite asset manifest mappings.
type Manifest struct {
	records map[string]manifestRecord
	entry   string
	File    string
	CSS     []string
	Imports []string
}

// GetFile returns the entry JavaScript file.
func (m Manifest) GetFile() string {
	if m.records == nil {
		return ""
	}
	return m.records[m.entry].File
}

// GetCSS returns all CSS bundles.
func (m Manifest) GetCSS() []string {
	if m.records == nil {
		return nil
	}
	css := slices.Clone(m.records[m.entry].CSS)
	for _, chunk := range m.records[m.entry].Imports {
		css = append(css, m.records[chunk].CSS...)
	}
	return css
}

// GetImports returns all imported JS chunks.
func (m Manifest) GetImports() []string {
	if m.records == nil {
		return nil
	}
	imports := []string{}
	for _, chunkName := range m.records[m.entry].Imports {
		if chunk, ok := m.records[chunkName]; ok {
			imports = append(imports, chunk.File)
		}
	}
	return imports
}

// ParseManifest parses a Vite manifest from raw JSON bytes.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m.records); err != nil {
		return m, errors.Wrap(err, "unmarshal vite manifest")
	}
	for name, record := range m.records {
		if record.IsEntry {
			m.entry = name
			break
		}
	}
	if m.entry != "" {
		m.File = m.records[m.entry].File
		m.CSS = slices.Clone(m.records[m.entry].CSS)
		for _, chunk := range m.records[m.entry].Imports {
			m.CSS = append(m.CSS, m.records[chunk].CSS...)
		}
		for _, chunkName := range m.records[m.entry].Imports {
			if chunk, ok := m.records[chunkName]; ok {
				m.Imports = append(m.Imports, chunk.File)
			}
		}
	}
	return m, nil
}

// ReadManifest reads and parses the vite manifest file from disk.
func ReadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, errors.Wrapf(err, "read vite manifest from %s", path)
	}
	return ParseManifest(raw)
}
