package markdown

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	content := `---
title: My Note
tags: [project, work]
---
# Hello
This is a #test and a [[wikilink]].
`
	doc, err := Parse([]byte(content), "note")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if doc.Name != "note" {
		t.Errorf("expected name 'note', got %q", doc.Name)
	}

	expectedFM := map[string]any{
		"title": "My Note",
		"tags":  []any{"project", "work"},
	}
	if !reflect.DeepEqual(doc.Frontmatter, expectedFM) {
		t.Errorf("expected frontmatter %v, got %v", expectedFM, doc.Frontmatter)
	}

	expectedTags := []string{"project", "work", "test"}
	// Tags might be in different order because of map
	tagMap := make(map[string]bool)
	for _, tag := range doc.Tags {
		tagMap[tag] = true
	}
	for _, tag := range expectedTags {
		if !tagMap[tag] {
			t.Errorf("expected tag %q not found in %v", tag, doc.Tags)
		}
	}

	expectedWikilinks := []string{"wikilink"}
	if !reflect.DeepEqual(doc.Wikilinks, expectedWikilinks) {
		t.Errorf("expected wikilinks %v, got %v", expectedWikilinks, doc.Wikilinks)
	}
}
