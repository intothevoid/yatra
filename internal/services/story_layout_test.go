package services

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/karan/travel-diary/internal/domain"
)

func TestStoryLayoutKeepsSubjectsAndUsesLeadPlusMosaic(t *testing.T) {
	blocks := []domain.ArticleBlock{{Type: "heading", Text: "Food"}, {Type: "paragraph", Text: "Food story."}}
	for _, id := range []string{"food1", "food2", "food3", "food4", "food5"} {
		blocks = append(blocks, domain.ArticleBlock{Type: "image", PhotoIDs: []string{id}, Caption: id})
	}
	blocks = append(blocks, domain.ArticleBlock{Type: "heading", Text: "Beach"}, domain.ArticleBlock{Type: "paragraph", Text: "Beach story."}, domain.ArticleBlock{Type: "image", PhotoIDs: []string{"beach"}})
	got := storyPhotoLayout(blocks)
	if len(got) != 7 || got[2].CardWidth != "full" || got[3].Type != "gallery" || len(got[3].PhotoIDs) != 4 || got[6].PhotoIDs[0] != "beach" {
		t.Fatalf("incorrect story layout: %#v", got)
	}
	if twice := storyPhotoLayout(got); !reflect.DeepEqual(got, twice) {
		t.Fatal("layout must be stable when reopened")
	}
	html := blocksToHTML(got)
	if !strings.Contains(html, "kg-width-full") || strings.Count(html, "kg-gallery-row") != 2 {
		t.Fatal("expected full width lead and two rows of two images")
	}
}

func TestGhostConvertsKoenigParagraphHeading(t *testing.T) {
	node := map[string]any{"type": "paragraph", "children": []any{map[string]any{"type": "extended-text", "text": "### Food"}}}
	enrichLexicalCaptions(node, nil, nil)
	if node["type"] != "heading" || node["tag"] != "h3" {
		t.Fatalf("heading stayed a paragraph: %#v", node)
	}
	if text := node["children"].([]any)[0].(map[string]any)["text"]; text != "Food" {
		t.Fatal(text)
	}
}

func TestGhostVideoMarkupUsesHostedPosterAndSilentLoopingPlayback(t *testing.T) {
	blocks := []domain.ArticleBlock{{Type: domain.BlockParagraph, Text: "Before"}, {Type: domain.BlockVideo, VideoID: "https://media.example/clip.mp4", VideoPosterURL: "https://media.example/poster.webp"}, {Type: domain.BlockParagraph, Text: "After"}}
	html := blocksToHTML(blocks)
	before, video, after := strings.Index(html, "Before"), strings.Index(html, "travel-video"), strings.Index(html, "After")
	if before < 0 || video < before || after < video {
		t.Fatalf("video markup order is wrong: %s", html)
	}
	for _, required := range []string{"autoplay", "muted", "loop", "playsinline", "preload=\"none\"", "poster=\"https://media.example/poster.webp\"", "https://media.example/clip.mp4"} {
		if !strings.Contains(html, required) {
			t.Errorf("video markup missing %q: %s", required, html)
		}
	}
}

// Invoked by npm run test:ghost so Ghost's real Node renderer can inspect the
// exact content produced by the Go exporter, rather than a duplicate converter.
func TestGhostRenderContract(t *testing.T) {
	input := os.Getenv("YATRA_GHOST_RENDER_INPUT")
	if input == "" {
		t.Skip("run via npm run test:ghost")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var doc domain.ArticleDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	urls, descriptions := map[string]string{}, map[string]string{}
	dimensions := map[string]image.Point{}
	for _, block := range doc.Blocks {
		for _, id := range block.PhotoIDs {
			urls[id] = "https://example.test/content/images/" + id + ".jpg"
			descriptions[id] = "Caption for " + id
			dimensions[urls[id]] = image.Pt(1600, 1200)
		}
	}
	urls[doc.FeaturePhotoID] = "https://example.test/content/images/" + doc.FeaturePhotoID + ".jpg"
	html, lexical, err := renderGhostContent(&doc, urls, descriptions, dimensions)
	if err != nil {
		t.Fatal(err)
	}
	payload := GhostPost{HTML: html, Lexical: lexical, FeatureImage: urls[doc.FeaturePhotoID], Status: "draft"}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("YATRA_GHOST_RENDER_OUTPUT"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestInterleaveKeepsEverySelectedPhoto(t *testing.T) {
	photos := []domain.Photo{{ID: "header", Status: domain.PhotoStatusRecommended}}
	for i := 0; i < 10; i++ {
		photos = append(photos, domain.Photo{ID: fmt.Sprintf("p_%d", i), Status: domain.PhotoStatusRecommended})
	}
	blocks := interleavePhotos([]domain.ArticleBlock{{Type: domain.BlockHeading, Text: "Market"}, {Type: domain.BlockParagraph, Text: "Market story."}}, photos, "header")
	actual := map[string]bool{}
	for _, block := range blocks {
		for _, id := range block.PhotoIDs {
			if actual[id] {
				t.Fatalf("duplicate photo %s", id)
			}
			actual[id] = true
		}
	}
	if len(actual) != 10 || actual["header"] {
		t.Fatalf("selected images lost or header repeated: %v", actual)
	}
}

func TestGhostClientFallbackSendsRenderableGalleries(t *testing.T) {
	blocks := []domain.ArticleBlock{{Type: domain.BlockHeading, Text: "Busan Baby Dinner", Level: 2}, {Type: domain.BlockGallery, PhotoIDs: []string{"https://example.test/one.jpg", "https://example.test/two.jpg"}}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"errors":[{"message":"Invalid lexical structure"}]}`)
			return
		}
		if r.URL.Query().Get("source") != "html" {
			t.Error("fallback did not use Ghost's HTML importer")
		}
		var payload struct {
			Posts []GhostPost `json:"posts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		post := payload.Posts[0]
		if post.Lexical != "" || !strings.Contains(post.HTML, "<h2>Busan Baby Dinner</h2>") || strings.Count(post.HTML, `width="1200" height="800"`) != 2 {
			t.Errorf("fallback lost required heading or image attributes")
		}
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, `{"posts":[{"id":"test-draft"}]}`)
	}))
	defer server.Close()
	client := NewGhostClient(server.URL, "test:"+strings.Repeat("ab", 32))
	post := GhostPost{HTML: blocksToHTML(blocks), Lexical: `{"root":{"type":"root","children":[]}}`, Status: "draft"}
	if err := client.CreatePost(context.Background(), &post); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || post.ID != "test-draft" {
		t.Fatal("draft fallback failed")
	}
}
