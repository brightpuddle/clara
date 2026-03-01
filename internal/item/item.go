// Package item defines the ClaraItem — the universal interchange format for
// actionable items surfaced by Clara across all data sources and clients.
//
// The wire format is YAML frontmatter followed by a Markdown body:
//
//	---
//	id: clara-2026-02-15-grocery
//	type: task
//	source: email
//	status: proposed
//	action_surface: cloud
//	---
//	Pick up milk from the grocery store
//
//	> Suggested from: "can you grab some milk?" — Sarah
package item

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Type constants for ClaraItem.Type.
const (
	TypeTask       = "task"
	TypeSuggestion = "suggestion" // backlink / knowledge-graph suggestion
	TypeInsight    = "insight"
)

// Source constants for ClaraItem.Source.
const (
	SourceEmail       = "email"
	SourceReminders   = "reminders"
	SourceTaskwarrior = "taskwarrior"
	SourceMarkdown    = "markdown"
	SourceCalendar    = "calendar"
	SourceManual      = "manual"
)

// Status constants for ClaraItem.Status.
const (
	StatusProposed  = "proposed"
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDismissed = "dismissed"
	StatusDone      = "done"
)

// ActionSurface constants determine which clients can act on a proposal.
const (
	SurfaceCloud    = "cloud"     // iCloud-replicated: Mac + iOS
	SurfaceLocalMac = "local_mac" // Mac filesystem / CLI only
)

// Priority constants.
const (
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
)

// ClaraItem is the universal representation of an actionable item.
// It serializes to/from YAML frontmatter + Markdown body, and to/from JSON
// for API responses.
type ClaraItem struct {
	// Frontmatter fields
	ID            string     `yaml:"id"             json:"id"`
	Type          string     `yaml:"type"           json:"type"`
	Source        string     `yaml:"source"         json:"source"`
	SourceRef     string     `yaml:"source_ref,omitempty" json:"source_ref,omitempty"`
	Priority      string     `yaml:"priority,omitempty"  json:"priority,omitempty"`
	Due           *time.Time `yaml:"due,omitempty"       json:"due,omitempty"`
	Tags          []string   `yaml:"tags,omitempty"      json:"tags,omitempty"`
	Status        string     `yaml:"status"         json:"status"`
	ActionSurface string     `yaml:"action_surface" json:"action_surface"`
	Created       time.Time  `yaml:"created"        json:"created"`

	// Markdown body (not in frontmatter)
	Body string `yaml:"-" json:"body,omitempty"`
}

// frontmatter is a private alias used purely for YAML marshal/unmarshal
// to avoid recursive marshalling issues.
type frontmatter struct {
	ID            string     `yaml:"id"`
	Type          string     `yaml:"type"`
	Source        string     `yaml:"source"`
	SourceRef     string     `yaml:"source_ref,omitempty"`
	Priority      string     `yaml:"priority,omitempty"`
	Due           *time.Time `yaml:"due,omitempty"`
	Tags          []string   `yaml:"tags,omitempty"`
	Status        string     `yaml:"status"`
	ActionSurface string     `yaml:"action_surface"`
	Created       time.Time  `yaml:"created"`
}

// Marshal encodes the item to YAML frontmatter + Markdown body text.
func (c ClaraItem) Marshal() ([]byte, error) {
	fm := frontmatter{
		ID:            c.ID,
		Type:          c.Type,
		Source:        c.Source,
		SourceRef:     c.SourceRef,
		Priority:      c.Priority,
		Due:           c.Due,
		Tags:          c.Tags,
		Status:        c.Status,
		ActionSurface: c.ActionSurface,
		Created:       c.Created,
	}
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return nil, fmt.Errorf("marshal frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	if c.Body != "" {
		buf.WriteString(c.Body)
		if !strings.HasSuffix(c.Body, "\n") {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes(), nil
}

// Unmarshal decodes a ClaraItem from YAML frontmatter + Markdown body text.
func Unmarshal(data []byte) (ClaraItem, error) {
	s := string(data)

	// Must start with "---\n"
	if !strings.HasPrefix(s, "---\n") {
		return ClaraItem{}, fmt.Errorf("missing opening '---'")
	}
	rest := s[4:]

	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return ClaraItem{}, fmt.Errorf("missing closing '---'")
	}

	fmText := rest[:end]
	body := strings.TrimPrefix(rest[end+5:], "\n")

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return ClaraItem{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	return ClaraItem{
		ID:            fm.ID,
		Type:          fm.Type,
		Source:        fm.Source,
		SourceRef:     fm.SourceRef,
		Priority:      fm.Priority,
		Due:           fm.Due,
		Tags:          fm.Tags,
		Status:        fm.Status,
		ActionSurface: fm.ActionSurface,
		Created:       fm.Created,
		Body:          body,
	}, nil
}
