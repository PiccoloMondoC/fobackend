// focodebase/fobackend/internal/data/rich_text_test.go
package data

import (
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, raw string) *RichTextDocument {
	t.Helper()
	doc, err := ParseRichTextDocument([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return doc
}

func canonical(t *testing.T, doc *RichTextDocument) string {
	t.Helper()
	b, err := doc.CanonicalJSON()
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	return string(b)
}

func TestRichTextAcceptsPermittedVocabulary(t *testing.T) {
	doc := mustParse(t, `{"type":"doc","version":1,"blocks":[
		{"type":"paragraph","content":[{"text":"Meet "},{"text":"Veltrix","marks":["bold"]}]},
		{"type":"bullet_list","items":[[{"text":"Thinks fast","marks":["italic","bold"]}],[{"text":"Weighs 40 g"}]]},
		{"type":"ordered_list","items":[[{"text":"Reserve"}]]}
	]}`)
	got := canonical(t, doc)
	want := `{"type":"doc","version":1,"blocks":[` +
		`{"type":"paragraph","content":[{"text":"Meet "},{"text":"Veltrix","marks":["bold"]}]},` +
		`{"type":"bullet_list","items":[[{"text":"Thinks fast","marks":["bold","italic"]}],[{"text":"Weighs 40 g"}]]},` +
		`{"type":"ordered_list","items":[[{"text":"Reserve"}]]}]}`
	if got != want {
		t.Fatalf("canonical mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestRichTextCanonicalizesBenignVariation(t *testing.T) {
	doc := mustParse(t, `{"type":"doc","version":1,"blocks":[
		{"type":"paragraph","content":[{"text":"a","marks":["bold","bold"]},{"text":"b","marks":["bold"]},{"text":""}]},
		{"type":"paragraph","content":[{"text":"   "}]},
		{"type":"bullet_list","items":[[],[{"text":""}]]}
	]}`)
	if got := canonical(t, doc); got != `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"ab","marks":["bold"]}]}]}` {
		t.Fatalf("got %s", got)
	}
}

func TestRichTextEmptyDocumentMeansNoDescription(t *testing.T) {
	for _, raw := range []string{``, `null`, `  `, `{"type":"doc","version":1,"blocks":[]}`,
		`{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":" "}]}]}`} {
		doc, err := ParseRichTextDocument([]byte(raw))
		if err != nil || doc != nil {
			t.Fatalf("%q: want (nil, nil), got (%v, %v)", raw, doc, err)
		}
	}
}

// Nothing resembling markup, script or style can cross the boundary as
// structure: unknown shapes are rejected and markup-looking text stays text.
func TestRichTextRejectsDisallowedInput(t *testing.T) {
	cases := map[string]string{
		"html string":        `"<p onclick=alert(1)>hi</p>"`,
		"array root":         `[]`,
		"wrong type":         `{"type":"html","version":1,"blocks":[]}`,
		"future version":     `{"type":"doc","version":2,"blocks":[]}`,
		"unknown doc field":  `{"type":"doc","version":1,"blocks":[],"html":"<b>x</b>"}`,
		"unknown block":      `{"type":"doc","version":1,"blocks":[{"type":"heading","content":[{"text":"x"}]}]}`,
		"script block":       `{"type":"doc","version":1,"blocks":[{"type":"script","content":[{"text":"alert(1)"}]}]}`,
		"unknown run field":  `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"x","href":"javascript:alert(1)"}]}]}`,
		"link mark":          `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"x","marks":["link"]}]}]}`,
		"style attribute":    `{"type":"doc","version":1,"blocks":[{"type":"paragraph","style":"x","content":[]}]}`,
		"paragraph items":    `{"type":"doc","version":1,"blocks":[{"type":"paragraph","items":[[{"text":"x"}]]}]}`,
		"list content":       `{"type":"doc","version":1,"blocks":[{"type":"bullet_list","content":[{"text":"x"}]}]}`,
		"control char":       `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"a\u0000b"}]}]}`,
		"newline in run":     `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"a\nb"}]}]}`,
		"bidi override":      `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"a\u202eb"}]}]}`,
		"trailing data":      `{"type":"doc","version":1,"blocks":[]} {}`,
		"malformed":          `{"type":"doc",`,
		"text not a string":  `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":{"x":1}}]}]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			doc, err := ParseRichTextDocument([]byte(raw))
			if !errors.Is(err, ErrRichTextInvalid) {
				t.Fatalf("want ErrRichTextInvalid, got doc=%v err=%v", doc, err)
			}
		})
	}
}

func TestRichTextMarkupLikeTextRemainsText(t *testing.T) {
	doc := mustParse(t, `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"<script>alert(1)</script>"}]}]}`)
	if doc.PlainText() != "<script>alert(1)</script>" {
		t.Fatalf("text was altered: %q", doc.PlainText())
	}
	if len(doc.Blocks) != 1 || doc.Blocks[0].Type != RichTextBlockParagraph {
		t.Fatalf("markup-like text must not create structure")
	}
}

func TestRichTextLimits(t *testing.T) {
	long := strings.Repeat("a", MaxRichTextCharacters+1)
	_, err := ParseRichTextDocument([]byte(`{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"` + long + `"}]}]}`))
	if !errors.Is(err, ErrRichTextInvalid) {
		t.Fatalf("character limit not enforced: %v", err)
	}
	blocks := strings.Repeat(`{"type":"paragraph","content":[{"text":"x"}]},`, MaxRichTextBlocks+1)
	_, err = ParseRichTextDocument([]byte(`{"type":"doc","version":1,"blocks":[` + strings.TrimSuffix(blocks, ",") + `]}`))
	if !errors.Is(err, ErrRichTextInvalid) {
		t.Fatalf("block limit not enforced: %v", err)
	}
}

func TestRichTextCanonicalJSONRevalidates(t *testing.T) {
	// A document constructed in code (not parsed) is still canonicalized.
	doc := &RichTextDocument{Type: "doc", Version: 1, Blocks: []RichTextBlock{{Type: "table"}}}
	if _, err := doc.CanonicalJSON(); !errors.Is(err, ErrRichTextInvalid) {
		t.Fatalf("CanonicalJSON must reject non-canonical structure, got %v", err)
	}
}

func TestDescriptionCanonicalBytesAreServerProduced(t *testing.T) {
	doc := mustParse(t, `{"version":1,"blocks":[{"content":[{"marks":["italic"],"text":"x"}],"type":"paragraph"}],"type":"doc"}`)
	b, err := canonicalMerchantFutureOfferingDescription(doc)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"type":"doc","version":1,"blocks":[{"type":"paragraph","content":[{"text":"x","marks":["italic"]}]}]}` {
		t.Fatalf("persisted bytes are not canonical: %s", b)
	}
	if b, _ := canonicalMerchantFutureOfferingDescription(nil); b != nil {
		t.Fatalf("nil description must persist as NULL")
	}
}
