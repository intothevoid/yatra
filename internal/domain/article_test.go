package domain

import (
	"testing"
)

func TestGenerateSlug(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Kyoto, Japan", "kyoto-japan"},
		{"Fushimi Inari Shrine", "fushimi-inari-shrine"},
		{"A day in Tokyo!", "a-day-in-tokyo"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := GenerateSlug(tt.title)
			if got != tt.want {
				t.Errorf("GenerateSlug(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestArticleDocumentValidate(t *testing.T) {
	// Valid document
	doc := ArticleDocument{
		SchemaVersion: 1,
		ArticleID:     "a_1",
		Title:         "Test",
		Blocks: []ArticleBlock{
			{ID: "b1", Type: BlockParagraph, Text: "Hello"},
		},
	}
	errs := doc.Validate()
	if len(errs) != 0 {
		t.Errorf("Validate() on valid doc = %v, want nil", errs)
	}
	if doc.Slug != "test" {
		t.Errorf("slug auto-generated = %q, want %q", doc.Slug, "test")
	}

	// Missing fields
	invalid := ArticleDocument{SchemaVersion: 1}
	errs = invalid.Validate()
	if len(errs) == 0 {
		t.Error("Validate() on invalid doc returned no errors")
	}

	// Unsupported schema version
	badSchema := ArticleDocument{SchemaVersion: 2, ArticleID: "a_2", Title: "Test"}
	errs = badSchema.Validate()
	if len(errs) == 0 {
		t.Error("Validate() on bad schema version returned no errors")
	}
}

func TestArticleBlockTypes(t *testing.T) {
	// Verify constants match spec §8
	validTypes := []string{BlockHeading, BlockParagraph, BlockImage, BlockGallery, BlockQuote}
	for _, typ := range validTypes {
		if typ == "" {
			t.Errorf("block type constant is empty")
		}
	}
}
