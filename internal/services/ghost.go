package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// GhostExporter handles publishing articles to Ghost.
type GhostExporter struct {
	client *GhostClient
	store  *store.Store
}

// NewGhostExporter creates a new exporter.
func NewGhostExporter(client *GhostClient, s *store.Store) *GhostExporter {
	return &GhostExporter{client: client, store: s}
}

// CreateDraft creates or updates a Ghost draft from an approved article.
func (e *GhostExporter) CreateDraft(ctx context.Context, tripID string) error {
	// Get approved article
	articleID := "art_" + tripID
	doc, err := e.store.Articles.GetLatest(ctx, articleID)
	if err != nil {
		return fmt.Errorf("get article: %w", err)
	}

	// Convert to Ghost format
	ghostPost := e.convertToGhost(doc)

	// Check if draft already exists
	existing, err := e.client.FindDraft(ctx, doc.Slug)
	if err != nil {
		return fmt.Errorf("find draft: %w", err)
	}

	var postID string
	if existing != nil {
		// Update existing draft
		postID = existing.ID
		ghostPost.ID = postID
		if err := e.client.UpdatePost(ctx, ghostPost); err != nil {
			return fmt.Errorf("update post: %w", err)
		}
	} else {
		// Create new draft
		if err := e.client.CreatePost(ctx, ghostPost); err != nil {
			return fmt.Errorf("create post: %w", err)
		}
		postID = ghostPost.ID
	}

	// Record export
	export := store.GhostExport{
		ArticleID:   articleID,
		RevisionID:  doc.ArticleID + "_rev1",
		RemotePostID: postID,
	}
	e.store.SaveGhostExport(ctx, export)

	return nil
}

// GhostPost is the Ghost Admin API post format.
type GhostPost struct {
	ID        string   `json:"id,omitempty"`
	Title     string   `json:"title"`
	Slug      string   `json:"slug"`
	HTML      string   `json:"html"`
	Excerpt   string   `json:"custom_excerpt,omitempty"`
	Tags      []GhostTag `json:"tags"`
	Featured  bool     `json:"featured,omitempty"`
	FeatureImage string `json:"feature_image,omitempty"`
	Status    string   `json:"status"` // "draft"
}

// GhostTag is a Ghost tag.
type GhostTag struct {
	Name string `json:"name"`
}

func (e *GhostExporter) convertToGhost(doc *domain.ArticleDocument) GhostPost {
	// Convert blocks to HTML
	html := blocksToHTML(doc.Blocks)

	// Convert tags
	var tags []GhostTag
	for _, t := range doc.Tags {
		tags = append(tags, GhostTag{Name: t})
	}

	return GhostPost{
		Title:        doc.Title,
		Slug:         doc.Slug,
		HTML:         html,
		Excerpt:      doc.Excerpt,
		Tags:         tags,
		FeatureImage: doc.FeaturePhotoID,
		Status:       "draft",
	}
}

func blocksToHTML(blocks []domain.ArticleBlock) string {
	var buf bytes.Buffer
	for _, block := range blocks {
		switch block.Type {
		case domain.BlockHeading:
			level := block.Level
			if level < 1 || level > 6 {
				level = 2
			}
			buf.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, escapeHTML(block.Text), level))
		case domain.BlockParagraph:
			buf.WriteString(fmt.Sprintf("<p>%s</p>\n", escapeHTML(block.Text)))
		case domain.BlockImage:
			buf.WriteString(fmt.Sprintf("<figure><img src=\"%s\" alt=\"%s\"></figure>\n", block.PhotoIDs[0], escapeHTML(block.AltText)))
		case domain.BlockGallery:
			buf.WriteString("<div class=\"kg-gallery\">\n")
			for _, photoID := range block.PhotoIDs {
				buf.WriteString(fmt.Sprintf("<img src=\"%s\">\n", photoID))
			}
			buf.WriteString("</div>\n")
		case domain.BlockQuote:
			buf.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n", escapeHTML(block.Text)))
		}
	}
	return buf.String()
}

func escapeHTML(s string) string {
	return s // TODO: proper HTML escaping
}

// GhostClient provides access to the Ghost Admin API.
type GhostClient struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewGhostClient creates a new Ghost client.
func NewGhostClient(baseURL, apiKey string) *GhostClient {
	return &GhostClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// generateJWT creates a JWT for Ghost Admin API authentication.
func (g *GhostClient) generateJWT() (string, error) {
	claims := jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(g.APIKey))
}

// CreatePost creates a new post in Ghost.
func (g *GhostClient) CreatePost(ctx context.Context, post GhostPost) error {
	token, err := g.generateJWT()
	if err != nil {
		return fmt.Errorf("generate JWT: %w", err)
	}

	payload := map[string]any{"posts": []GhostPost{post}}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", g.BaseURL+"/ghost/api/admin/posts/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ghost API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Posts []GhostPost `json:"posts"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Posts) > 0 {
		post.ID = result.Posts[0].ID
	}
	return nil
}

// UpdatePost updates an existing post in Ghost.
func (g *GhostClient) UpdatePost(ctx context.Context, post GhostPost) error {
	token, err := g.generateJWT()
	if err != nil {
		return fmt.Errorf("generate JWT: %w", err)
	}

	payload := map[string]any{"posts": []GhostPost{post}}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "PUT", g.BaseURL+"/ghost/api/admin/posts/"+post.ID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ghost API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// FindDraft finds an existing draft by slug.
func (g *GhostClient) FindDraft(ctx context.Context, slug string) (*GhostPost, error) {
	token, err := g.generateJWT()
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", g.BaseURL+"/ghost/api/admin/posts/?slug="+slug+"&status=draft&limit=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Ghost "+token)

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Posts []GhostPost `json:"posts"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Posts) > 0 {
		return &result.Posts[0], nil
	}
	return nil, nil
}

// UploadImage uploads an image to Ghost.
func (g *GhostClient) UploadImage(ctx context.Context, imagePath string) (string, error) {
	token, err := g.generateJWT()
	if err != nil {
		return "", fmt.Errorf("generate JWT: %w", err)
	}

	file, err := os.Open(imagePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("image", filepath.Base(imagePath))
	if err != nil {
		return "", err
	}
	io.Copy(part, file)
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, "POST", g.BaseURL+"/ghost/api/admin/images/upload/", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Images) > 0 {
		return result.Images[0].URL, nil
	}
	return "", fmt.Errorf("no image URL in response")
}

// GetPosts fetches published posts from Ghost.
func (g *GhostClient) GetPosts(ctx context.Context, limit int) ([]GhostPost, error) {
	token, err := g.generateJWT()
	if err != nil {
		return nil, fmt.Errorf("generate JWT: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", g.BaseURL+"/ghost/api/admin/posts/?limit="+fmt.Sprintf("%d", limit), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Ghost "+token)

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Posts []GhostPost `json:"posts"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Posts, nil
}
