package main

import (
	"testing"

	"github.com/brightpuddle/clara/internal/config"
)

func TestResolveExposeProfile(t *testing.T) {
	cfg := &config.Config{
		MCPExposeProfiles: map[string]config.MCPExposeProfile{
			"claude-code": {
				Include: []string{"mail.*", "task.*"},
				Exclude: []string{"task.delete"},
			},
		},
	}

	t.Run("unknown profile name errors", func(t *testing.T) {
		if _, err := resolveExposeProfile(cfg, "nope", nil, nil); err == nil {
			t.Fatal("expected error for unknown profile name")
		}
	})

	t.Run("no profile and no allow errors", func(t *testing.T) {
		if _, err := resolveExposeProfile(cfg, "", nil, nil); err == nil {
			t.Fatal("expected error when nothing is selected")
		}
	})

	t.Run("allow alone works without a profile", func(t *testing.T) {
		profile, err := resolveExposeProfile(cfg, "", []string{"fs.*"}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !profile.Allows("fs.read_file") {
			t.Error("expected fs.read_file to be allowed")
		}
		if profile.Allows("mail.search") {
			t.Error("expected mail.search to be denied when no profile is selected")
		}
	})

	t.Run("allow and deny union onto a named profile", func(t *testing.T) {
		profile, err := resolveExposeProfile(cfg, "claude-code", []string{"github.create_issue"}, []string{"mail.search"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !profile.Allows("task.list") {
			t.Error("expected task.list to remain allowed from the profile")
		}
		if profile.Allows("task.delete") {
			t.Error("expected task.delete to remain excluded from the profile")
		}
		if !profile.Allows("github.create_issue") {
			t.Error("expected github.create_issue to be allowed via --allow")
		}
		if profile.Allows("mail.search") {
			t.Error("expected mail.search to be denied via --deny")
		}
	})
}
