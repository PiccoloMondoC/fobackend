// Package data provides the controlled rich-text representation used for
// consumer-facing authored content.
//
// focodebase/fobackend/internal/data/rich_text.go
//
// GTM:
//
//	Layer: 2.4 Merchant / Future Offering Domain
//	Release Class: SPINE
//	Reason:
//	  Future Offering descriptions are merchant-authored content that will be
//	  presented to consumers. This file is the persistence boundary for that
//	  content: nothing reaches the database unless it parses into the
//	  canonical document below, and the bytes persisted are always this
//	  package's own serialization, never client-supplied bytes.
//
// Canonical representation (version 1):
//
//	{
//	  "type": "doc",
//	  "version": 1,
//	  "blocks": [
//	    {"type": "paragraph",    "content": [Run, ...]},
//	    {"type": "bullet_list",  "items":   [[Run, ...], ...]},
//	    {"type": "ordered_list", "items":   [[Run, ...], ...]}
//	  ]
//	}
//
//	Run = {"text": "...", "marks": ["bold", "italic"]}   // marks optional
//
// Permitted vocabulary: paragraphs, bulleted lists, numbered lists, bold,
// italic. No links, headings, nesting, markup, attributes, or styles.
// There is no HTML anywhere in the representation, so there is nothing to
// escape or strip; renderers emit text nodes only.
//
// Validation vs. canonicalization:
//
//	Rejected (ErrRichTextInvalid): malformed JSON, trailing data, unknown
//	fields, unknown block types or marks, wrong field for a block type,
//	control or bidirectional-override characters, and size limits exceeded.
//
//	Canonicalized (accepted silently): empty runs are dropped, adjacent runs
//	with identical marks are merged, marks are de-duplicated and sorted,
//	empty paragraphs/items/lists are dropped. A document with no remaining
//	text canonicalizes to nil (no description).
//
// SPINE Rule:
//
//	Keep compiling.
//	Keep production-ready.
//	Never persist bytes that were not produced by CanonicalJSON.
//	Never widen the vocabulary without a new version and a renderer update.
package data

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

// Rich-text vocabulary.
const (
	RichTextDocumentType    = "doc"
	RichTextDocumentVersion = 1

	RichTextBlockParagraph   = "paragraph"
	RichTextBlockBulletList  = "bullet_list"
	RichTextBlockOrderedList = "ordered_list"

	RichTextMarkBold   = "bold"
	RichTextMarkItalic = "italic"
)

// Engineering safety limits. Administration may narrow presentation limits;
// these bound storage and rendering cost.
const (
	MaxRichTextInputBytes    = 64 * 1024
	MaxRichTextCharacters    = 10_000
	MaxRichTextBlocks        = 100
	MaxRichTextListItems     = 50
	MaxRichTextRunsPerInline = 200
)

// RichTextRun is a span of text with an optional set of marks.
type RichTextRun struct {
	Text  string   `json:"text"`
	Marks []string `json:"marks,omitempty"`
}

// RichTextBlock is one block. Paragraphs use Content; lists use Items.
type RichTextBlock struct {
	Type    string          `json:"type"`
	Content []RichTextRun   `json:"content,omitempty"`
	Items   [][]RichTextRun `json:"items,omitempty"`
}

// RichTextDocument is the canonical controlled rich-text document.
type RichTextDocument struct {
	Type    string          `json:"type"`
	Version int             `json:"version"`
	Blocks  []RichTextBlock `json:"blocks"`
}

func richTextInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRichTextInvalid, fmt.Sprintf(format, args...))
}

// ParseRichTextDocument strictly decodes raw JSON into a canonical document.
//
// A nil/empty/"null" input, or a document with no text after
// canonicalization, returns (nil, nil): the caller stores no content.
func ParseRichTextDocument(raw []byte) (*RichTextDocument, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > MaxRichTextInputBytes {
		return nil, richTextInvalid("document exceeds %d bytes", MaxRichTextInputBytes)
	}
	if !utf8.Valid(trimmed) {
		return nil, richTextInvalid("document is not valid UTF-8")
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	var doc RichTextDocument
	if err := dec.Decode(&doc); err != nil {
		return nil, richTextInvalid("malformed document")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, richTextInvalid("unexpected data after document")
	}
	return CanonicalizeRichTextDocument(&doc)
}

// CanonicalizeRichTextDocument validates a decoded document and returns its
// canonical form, or (nil, nil) when it contains no text.
func CanonicalizeRichTextDocument(doc *RichTextDocument) (*RichTextDocument, error) {
	if doc == nil {
		return nil, nil
	}
	if doc.Type != RichTextDocumentType {
		return nil, richTextInvalid("unsupported document type %q", doc.Type)
	}
	if doc.Version != RichTextDocumentVersion {
		return nil, richTextInvalid("unsupported document version %d", doc.Version)
	}
	if len(doc.Blocks) > MaxRichTextBlocks {
		return nil, richTextInvalid("document exceeds %d blocks", MaxRichTextBlocks)
	}

	out := &RichTextDocument{Type: RichTextDocumentType, Version: RichTextDocumentVersion, Blocks: []RichTextBlock{}}
	characters := 0
	for i, block := range doc.Blocks {
		switch block.Type {
		case RichTextBlockParagraph:
			if block.Items != nil {
				return nil, richTextInvalid("block %d: paragraph must not have items", i)
			}
			runs, n, err := canonicalRuns(block.Content)
			if err != nil {
				return nil, fmt.Errorf("block %d: %w", i, err)
			}
			characters += n
			if len(runs) > 0 {
				out.Blocks = append(out.Blocks, RichTextBlock{Type: RichTextBlockParagraph, Content: runs})
			}
		case RichTextBlockBulletList, RichTextBlockOrderedList:
			if block.Content != nil {
				return nil, richTextInvalid("block %d: list must not have content", i)
			}
			if len(block.Items) > MaxRichTextListItems {
				return nil, richTextInvalid("block %d: list exceeds %d items", i, MaxRichTextListItems)
			}
			items := make([][]RichTextRun, 0, len(block.Items))
			for j, item := range block.Items {
				runs, n, err := canonicalRuns(item)
				if err != nil {
					return nil, fmt.Errorf("block %d item %d: %w", i, j, err)
				}
				characters += n
				if len(runs) > 0 {
					items = append(items, runs)
				}
			}
			if len(items) > 0 {
				out.Blocks = append(out.Blocks, RichTextBlock{Type: block.Type, Items: items})
			}
		default:
			return nil, richTextInvalid("block %d: unsupported block type %q", i, block.Type)
		}
		if characters > MaxRichTextCharacters {
			return nil, richTextInvalid("document exceeds %d characters", MaxRichTextCharacters)
		}
	}
	if len(out.Blocks) == 0 {
		return nil, nil
	}
	return out, nil
}

// canonicalRuns validates runs, drops empty ones, canonicalizes marks and
// merges neighbours with identical marks. It returns the rune count.
func canonicalRuns(runs []RichTextRun) ([]RichTextRun, int, error) {
	if len(runs) > MaxRichTextRunsPerInline {
		return nil, 0, richTextInvalid("too many text runs")
	}
	out := make([]RichTextRun, 0, len(runs))
	count := 0
	for _, run := range runs {
		if err := validateRichTextCharacters(run.Text); err != nil {
			return nil, 0, err
		}
		marks, err := canonicalMarks(run.Marks)
		if err != nil {
			return nil, 0, err
		}
		if run.Text == "" {
			continue
		}
		count += utf8.RuneCountInString(run.Text)
		if last := len(out) - 1; last >= 0 && sameMarks(out[last].Marks, marks) {
			out[last].Text += run.Text
			continue
		}
		out = append(out, RichTextRun{Text: run.Text, Marks: marks})
	}
	// A run list containing only whitespace carries no content.
	if !hasVisibleText(out) {
		return nil, 0, nil
	}
	return out, count, nil
}

func canonicalMarks(marks []string) ([]string, error) {
	if len(marks) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, m := range marks {
		if m != RichTextMarkBold && m != RichTextMarkItalic {
			return nil, richTextInvalid("unsupported mark %q", m)
		}
		seen[m] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out, nil
}

func sameMarks(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasVisibleText(runs []RichTextRun) bool {
	for _, r := range runs {
		for _, c := range r.Text {
			if c != ' ' && c != '\u00a0' {
				return true
			}
		}
	}
	return false
}

// validateRichTextCharacters rejects control characters (line structure is
// expressed by blocks, never by characters) and bidirectional override or
// isolate characters, which can visually disguise consumer-facing text.
func validateRichTextCharacters(text string) error {
	for _, c := range text {
		switch {
		case c < 0x20, c >= 0x7f && c <= 0x9f:
			return richTextInvalid("control characters are not allowed")
		case c >= 0x202a && c <= 0x202e, c >= 0x2066 && c <= 0x2069:
			return richTextInvalid("bidirectional control characters are not allowed")
		case c == utf8.RuneError:
			return richTextInvalid("invalid character")
		}
	}
	return nil
}

// CanonicalJSON returns the canonical serialization, or nil for no content.
// This is the only byte sequence that may be persisted.
func (d *RichTextDocument) CanonicalJSON() ([]byte, error) {
	if d == nil {
		return nil, nil
	}
	canonical, err := CanonicalizeRichTextDocument(d)
	if err != nil || canonical == nil {
		return nil, err
	}
	return json.Marshal(canonical)
}

// PlainText returns the document's text with blocks separated by newlines.
func (d *RichTextDocument) PlainText() string {
	if d == nil {
		return ""
	}
	var b bytes.Buffer
	write := func(runs []RichTextRun) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		for _, r := range runs {
			b.WriteString(r.Text)
		}
	}
	for _, block := range d.Blocks {
		if block.Type == RichTextBlockParagraph {
			write(block.Content)
			continue
		}
		for _, item := range block.Items {
			write(item)
		}
	}
	return b.String()
}
