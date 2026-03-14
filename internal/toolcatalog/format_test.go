package toolcatalog

import "testing"

func TestFormatProviderListDoesNotAddBlankLinesBetweenItems(t *testing.T) {
	got := FormatProviderList([]Provider{
		{Name: "db", Description: "Database tools"},
		{Name: "fs", Description: "Filesystem tools"},
	}, false)

	want := "db\n  Database tools\nfs\n  Filesystem tools"
	if got != want {
		t.Fatalf("provider list = %q, want %q", got, want)
	}
}

func TestFormatToolListDoesNotAddBlankLinesBetweenItems(t *testing.T) {
	got := FormatToolList([]Tool{
		{Name: "db.query", Description: "Query rows"},
		{Name: "db.exec", Description: "Execute SQL"},
	}, false)

	want := "db.query\n  Query rows\ndb.exec\n  Execute SQL"
	if got != want {
		t.Fatalf("tool list = %q, want %q", got, want)
	}
}
