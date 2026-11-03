package webexapi

import "testing"

func TestHTMLToMarkdown(t *testing.T) {
	input := `<p>Please ignore this. I&apos;m just testing the updates in formatting directly from webex. These should each be numbered and appear on their own lines:</p><ol><li>This is item one</li><li>This is item two</li><li>This is item three</li></ol>`
	expected := "Please ignore this. I'm just testing the updates in formatting directly from webex. These should each be numbered and appear on their own lines:\n\n1. This is item one\n2. This is item two\n3. This is item three"

	got := HTMLToMarkdown(input)
	if got != expected {
		t.Fatalf("expected:\n%q\ngot:\n%q", expected, got)
	}
}

func TestHTMLToMarkdownUnordered(t *testing.T) {
	input := `<p>Bullet points:</p><ul><li>First <strong>bold</strong> item</li><li>Second <em>italic</em> with <code>code</code></li></ul>`
	expected := "Bullet points:\n\n- First **bold** item\n- Second *italic* with `code`"

	got := HTMLToMarkdown(input)
	if got != expected {
		t.Fatalf("expected:\n%q\ngot:\n%q", expected, got)
	}
}
