package services

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/karan/travel-diary/internal/domain"
)

var captionTags = regexp.MustCompile(`<[^>]*>`)

func plainCaption(s string) string {
	return strings.TrimSpace(html.UnescapeString(captionTags.ReplaceAllString(html.UnescapeString(s), "")))
}

// Keep pictures within their story section. Like the Goa reference, use a
// large lead photograph followed by pairs or a four-image mosaic.
func storyPhotoLayout(blocks []domain.ArticleBlock) []domain.ArticleBlock {
	var out []domain.ArticleBlock
	sequence := 0
	flush := func(section []domain.ArticleBlock) {
		var text, photos []domain.ArticleBlock
		for _, b := range section {
			if b.Type != domain.BlockImage && b.Type != domain.BlockGallery {
				text = append(text, b)
				continue
			}
			for i, id := range b.PhotoIDs {
				caption := b.Caption
				if i < len(b.PhotoCaptions) {
					caption = b.PhotoCaptions[i]
				}
				photos = append(photos, domain.ArticleBlock{Type: domain.BlockImage, PhotoIDs: []string{id}, Caption: plainCaption(caption), AltText: b.AltText})
			}
		}
		var cards []domain.ArticleBlock
		for len(photos) > 0 {
			sequence++
			size := 1
			if len(cards) > 0 || len(photos) == 2 {
				if len(photos) >= 4 {
					size = 4
				} else if len(photos) >= 2 {
					size = 2
				}
			}
			card := photos[0]
			card.ID = fmt.Sprintf("layout_%d", sequence)
			card.CardWidth = "full"
			if size > 1 {
				card.Type = domain.BlockGallery
				card.PhotoIDs = nil
				card.PhotoCaptions = nil
				card.CardWidth = "wide"
				var captions []string
				for _, photo := range photos[:size] {
					card.PhotoIDs = append(card.PhotoIDs, photo.PhotoIDs[0])
					card.PhotoCaptions = append(card.PhotoCaptions, photo.Caption)
					if photo.Caption != "" {
						captions = append(captions, photo.Caption)
					}
				}
				card.Caption = strings.Join(captions, " · ")
			}
			cards = append(cards, card)
			photos = photos[size:]
		}
		// A lead photo follows the first paragraph; remaining photos follow the
		// section's prose, rather than being collected at the start of the post.
		inserted := false
		for _, b := range text {
			out = append(out, b)
			if !inserted && b.Type == domain.BlockParagraph && len(cards) > 0 {
				out = append(out, cards[0])
				cards = cards[1:]
				inserted = true
			}
		}
		out = append(out, cards...)
	}
	start := 0
	for i, b := range blocks {
		if b.Type == domain.BlockHeading && i > start {
			flush(blocks[start:i])
			start = i
		}
	}
	flush(blocks[start:])
	return out
}
