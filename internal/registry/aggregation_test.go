package registry

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestNamespaceAggregation(t *testing.T) {
	reg := New(zerolog.Nop())

	// Simulate multiple servers registering tools in the same namespace
	reg.Register("mail.search", func(ctx context.Context, args map[string]any) (any, error) {
		return "search result", nil
	})
	reg.Register("mail.list", func(ctx context.Context, args map[string]any) (any, error) {
		return "list result", nil
	})

	// Check namespaces
	namespaces := reg.Namespaces()
	foundMail := false
	for _, ns := range namespaces {
		if ns == "mail" {
			foundMail = true
			break
		}
	}
	if !foundMail {
		t.Errorf("expected mail namespace, got %v", namespaces)
	}

	// Verify we can call both
	res1, err := reg.Call(context.Background(), "mail.search", nil)
	if err != nil { t.Fatalf("mail.search failed: %v", err) }
	if res1 != "search result" { t.Errorf("got %v", res1) }

	res2, err := reg.Call(context.Background(), "mail.list", nil)
	if err != nil { t.Fatalf("mail.list failed: %v", err) }
	if res2 != "list result" { t.Errorf("got %v", res2) }
}
