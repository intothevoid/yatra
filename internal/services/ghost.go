package services

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	if e.client == nil {
		return fmt.Errorf("Ghost integration is not configured")
	}
	// Get approved article
	articleID := "art_" + tripID
	doc, err := e.store.Articles.GetLatest(ctx, articleID)
	if err != nil {
		return fmt.Errorf("get article: %w", err)
	}
	revisionID, err := e.store.Articles.GetLatestRevisionID(ctx, articleID)
	if err != nil {
		return fmt.Errorf("get article revision: %w", err)
	}

	// Convert to Ghost format
	ghostPost, err := e.convertToGhost(ctx, doc)
	if err != nil {
		return fmt.Errorf("prepare Ghost post: %w", err)
	}

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
		ghostPost.UpdatedAt = existing.UpdatedAt
		if err := e.client.UpdatePost(ctx, ghostPost); err != nil {
			return fmt.Errorf("update post: %w", err)
		}
	} else {
		// Create new draft
		if err := e.client.CreatePost(ctx, &ghostPost); err != nil {
			return fmt.Errorf("create post: %w", err)
		}
		postID = ghostPost.ID
	}

	// Record export
	export := store.GhostExport{
		ArticleID:    articleID,
		RevisionID:   revisionID,
		RemotePostID: postID,
	}
	if err := e.store.SaveGhostExport(ctx, export); err != nil {
		return fmt.Errorf("record Ghost draft: %w", err)
	}

	return nil
}

// GhostPost is the Ghost Admin API post format.
type GhostPost struct {
	ID                  string     `json:"id,omitempty"`
	Title               string     `json:"title"`
	Slug                string     `json:"slug"`
	HTML                string     `json:"html,omitempty"`
	Lexical             string     `json:"lexical,omitempty"`
	Excerpt             string     `json:"custom_excerpt,omitempty"`
	Tags                []GhostTag `json:"tags"`
	Featured            bool       `json:"featured,omitempty"`
	FeatureImage        string     `json:"feature_image,omitempty"`
	FeatureImageAlt     string     `json:"feature_image_alt,omitempty"`
	FeatureImageCaption string     `json:"feature_image_caption,omitempty"`
	Status              string     `json:"status"` // "draft"
	UpdatedAt           string     `json:"updated_at,omitempty"`
}

// GhostTag is a Ghost tag.
type GhostTag struct {
	Name string `json:"name"`
}

func (e *GhostExporter) convertToGhost(ctx context.Context, doc *domain.ArticleDocument) (GhostPost, error) {
	photoURLs := make(map[string]string)
	photoDescriptions := make(map[string]string)
	photoIDs := []string{doc.FeaturePhotoID}
	if doc.Lexical != "" {
		for _, match := range regexp.MustCompile(`/api/v1/photos/(p_[^/]+)/thumbnail`).FindAllStringSubmatch(doc.Lexical, -1) {
			photoIDs = append(photoIDs, match[1])
		}
	}
	for _, block := range doc.Blocks {
		photoIDs = append(photoIDs, block.PhotoIDs...)
	}
	for _, photoID := range photoIDs {
		if photoID == "" || photoURLs[photoID] != "" {
			continue
		}
		photo, err := e.store.Photos.Get(ctx, photoID)
		if err != nil {
			return GhostPost{}, fmt.Errorf("load photo %s: %w", photoID, err)
		}
		path := photo.ExportPath
		if path == "" {
			path = photo.PreviewPath
		}
		if path == "" {
			return GhostPost{}, fmt.Errorf("photo %s has no export image", photoID)
		}
		photoDescriptions[photoID] = photoDescription(*photo)
		remoteURL, err := e.client.UploadImage(ctx, path)
		if err != nil {
			return GhostPost{}, fmt.Errorf("upload photo %s: %w", photoID, err)
		}
		photoURLs[photoID] = remoteURL
	}

	var blocks []domain.ArticleBlock
	for _, sourceBlock := range doc.Blocks {
		if sourceBlock.Type == domain.BlockGallery {
			// Expand legacy galleries so each image can carry its own description
			// as a caption in the Ghost post.
			for i, photoID := range sourceBlock.PhotoIDs {
				description := photoDescriptions[photoID]
				blocks = append(blocks, domain.ArticleBlock{ID: fmt.Sprintf("ghost_gallery_image_%d", i+1), Type: domain.BlockImage, PhotoIDs: []string{photoURLs[photoID]}, AltText: description, Caption: description})
			}
			continue
		}
		block := sourceBlock
		block.PhotoIDs = append([]string(nil), sourceBlock.PhotoIDs...)
		for j, photoID := range block.PhotoIDs {
			if block.Type == domain.BlockImage {
				if block.AltText == "" {
					block.AltText = photoDescriptions[photoID]
				}
				if block.Caption == "" {
					block.Caption = photoDescriptions[photoID]
				}
			}
			block.PhotoIDs[j] = photoURLs[photoID]
		}
		blocks = append(blocks, block)
	}
	contentHTML := blocksToHTML(blocks)
	var lexical string
	if doc.Lexical != "" {
		var state any
		if err := json.Unmarshal([]byte(doc.Lexical), &state); err != nil {
			return GhostPost{}, fmt.Errorf("parse editor document: %w", err)
		}
		state = enrichLexicalCaptions(state, photoDescriptions)
		state = replacePhotoURLs(state, photoURLs)
		data, err := json.Marshal(state)
		if err != nil {
			return GhostPost{}, fmt.Errorf("encode Ghost editor document: %w", err)
		}
		lexical = string(data)
	}

	// Convert tags
	var tags []GhostTag
	for _, t := range doc.Tags {
		tags = append(tags, GhostTag{Name: t})
	}

	return GhostPost{
		Title:               doc.Title,
		Slug:                doc.Slug,
		HTML:                contentHTML,
		Lexical:             lexical,
		Excerpt:             doc.Excerpt,
		Tags:                tags,
		FeatureImage:        photoURLs[doc.FeaturePhotoID],
		FeatureImageAlt:     limitUTF16(photoDescriptions[doc.FeaturePhotoID], 125),
		FeatureImageCaption: photoDescriptions[doc.FeaturePhotoID],
		Status:              "draft",
	}, nil
}

func limitUTF16(value string, maxUnits int) string {
	runes := []rune(strings.TrimSpace(value))
	units := 0
	var result strings.Builder
	for _, r := range runes {
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if units+width > maxUnits {
			break
		}
		result.WriteRune(r)
		units += width
	}
	return strings.TrimSpace(result.String())
}

func replacePhotoURLs(value any, photoURLs map[string]string) any {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			item[key] = replacePhotoURLs(child, photoURLs)
		}
		return item
	case []any:
		for i, child := range item {
			item[i] = replacePhotoURLs(child, photoURLs)
		}
		return item
	case string:
		for photoID, remoteURL := range photoURLs {
			item = strings.ReplaceAll(item, "/api/v1/photos/"+photoID+"/thumbnail", remoteURL)
		}
		return item
	default:
		return value
	}
}

var lexicalPhotoURLPattern = regexp.MustCompile(`/api/v1/photos/(p_[^/]+)/thumbnail`)

func enrichLexicalCaptions(value any, descriptions map[string]string) any {
	switch item := value.(type) {
	case map[string]any:
		if item["type"] == "heading" {
			if children, ok := item["children"].([]any); ok {
				if level := stripMarkdownHeading(children); level > 0 {
					item["tag"] = fmt.Sprintf("h%d", level)
				}
			}
		}
		for key, child := range item {
			item[key] = enrichLexicalCaptions(child, descriptions)
		}
		if source, ok := item["src"].(string); ok {
			photoID := ""
			if match := lexicalPhotoURLPattern.FindStringSubmatch(source); len(match) == 2 {
				photoID = match[1]
			}
			if photoID == "" {
				if fileName, ok := item["fileName"].(string); ok && strings.HasPrefix(fileName, "p_") {
					photoID = fileName
				}
			}
			if description := descriptions[photoID]; description != "" {
				caption, _ := item["caption"].(string)
				if strings.TrimSpace(caption) == "" {
					item["caption"] = description
				}
				if item["type"] == "image" {
					alt, _ := item["alt"].(string)
					if strings.TrimSpace(alt) == "" {
						item["alt"] = description
					}
				}
			}
		}
		return item
	case []any:
		for i, child := range item {
			item[i] = enrichLexicalCaptions(child, descriptions)
		}
		return item
	default:
		return value
	}
}

func stripMarkdownHeading(children []any) int {
	for _, child := range children {
		switch item := child.(type) {
		case map[string]any:
			if item["type"] == "text" {
				if text, ok := item["text"].(string); ok {
					clean, level, marked := parseMarkdownHeading(text)
					if marked {
						item["text"] = clean
						return level
					}
				}
			}
			if nested, ok := item["children"].([]any); ok {
				if level := stripMarkdownHeading(nested); level > 0 {
					return level
				}
			}
		case []any:
			if level := stripMarkdownHeading(item); level > 0 {
				return level
			}
		}
	}
	return 0
}

func blocksToHTML(blocks []domain.ArticleBlock) string {
	var buf bytes.Buffer
	for _, block := range blocks {
		switch block.Type {
		case domain.BlockHeading:
			level := block.Level
			text, markedLevel, marked := parseMarkdownHeading(block.Text)
			if marked {
				level = markedLevel
			} else {
				text = block.Text
			}
			if level < 1 || level > 6 {
				level = 2
			}
			buf.WriteString(fmt.Sprintf("<h%d>%s</h%d>\n", level, escapeHTML(text), level))
		case domain.BlockParagraph:
			buf.WriteString(fmt.Sprintf("<p>%s</p>\n", escapeHTML(block.Text)))
		case domain.BlockImage:
			if len(block.PhotoIDs) > 0 {
				buf.WriteString(fmt.Sprintf("<figure class=\"kg-card kg-image-card\"><img src=\"%s\" alt=\"%s\">", html.EscapeString(block.PhotoIDs[0]), escapeHTML(block.AltText)))
				if block.Caption != "" {
					buf.WriteString(fmt.Sprintf("<figcaption>%s</figcaption>", escapeHTML(block.Caption)))
				}
				buf.WriteString("</figure>\n")
			}
		case domain.BlockGallery:
			buf.WriteString("<figure class=\"kg-card kg-gallery-card kg-width-wide\"><div class=\"kg-gallery-container\">")
			for i, photoURL := range block.PhotoIDs {
				if i%3 == 0 {
					if i > 0 {
						buf.WriteString("</div>")
					}
					buf.WriteString("<div class=\"kg-gallery-row\">")
				}
				buf.WriteString(fmt.Sprintf("<div class=\"kg-gallery-image\"><img src=\"%s\" alt=\"\"></div>", html.EscapeString(photoURL)))
			}
			if len(block.PhotoIDs) > 0 {
				buf.WriteString("</div>")
			}
			buf.WriteString("</div>")
			if block.Caption != "" {
				buf.WriteString("<figcaption>" + escapeHTML(block.Caption) + "</figcaption>")
			}
			buf.WriteString("</figure>\n")
		case domain.BlockQuote:
			buf.WriteString(fmt.Sprintf("<blockquote>%s</blockquote>\n", escapeHTML(block.Text)))
		}
	}
	return buf.String()
}

func escapeHTML(s string) string {
	return html.EscapeString(s)
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
	parts := strings.SplitN(g.APIKey, ":", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", fmt.Errorf("invalid Ghost Admin API key: expected id:secret")
	}
	secret, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode Ghost Admin API secret: %w", err)
	}
	claims := jwt.MapClaims{
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
		"aud": "/admin/",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = parts[0]
	return token.SignedString(secret)
}

// CreatePost creates a new post in Ghost.
func (g *GhostClient) CreatePost(ctx context.Context, post *GhostPost) error {
	token, err := g.generateJWT()
	if err != nil {
		return fmt.Errorf("generate JWT: %w", err)
	}

	payload := map[string]any{"posts": []GhostPost{*post}}
	body, _ := json.Marshal(payload)

	endpoint := g.BaseURL + "/ghost/api/admin/posts/"
	if post.Lexical == "" {
		endpoint += "?source=html"
	}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Version", "v5.0")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if post.Lexical != "" && resp.StatusCode == http.StatusUnprocessableEntity && bytes.Contains(body, []byte("Invalid lexical structure")) {
			// Let Ghost convert the HTML rendering when a client editor document
			// is incompatible with the installed Ghost release.
			fallback := *post
			fallback.Lexical = ""
			if err := g.CreatePost(ctx, &fallback); err != nil {
				return err
			}
			post.ID = fallback.ID
			return nil
		}
		return fmt.Errorf("ghost API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Posts []GhostPost `json:"posts"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result.Posts) > 0 {
		post.ID = result.Posts[0].ID
	} else {
		return fmt.Errorf("Ghost created no post in response")
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

	endpoint := g.BaseURL + "/ghost/api/admin/posts/" + url.PathEscape(post.ID) + "/"
	if post.Lexical == "" {
		endpoint += "?source=html"
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Version", "v5.0")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if post.Lexical != "" && resp.StatusCode == http.StatusUnprocessableEntity && bytes.Contains(body, []byte("Invalid lexical structure")) {
			fallback := post
			fallback.Lexical = ""
			return g.UpdatePost(ctx, fallback)
		}
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

	req, err := http.NewRequestWithContext(ctx, "GET", g.BaseURL+"/ghost/api/admin/posts/?filter=slug%3A"+url.QueryEscape(slug)+"%2Bstatus%3Adraft&limit=1", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Accept-Version", "v5.0")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Ghost request failed %d: %s", resp.StatusCode, string(body))
	}

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
	headerBytes := make([]byte, 512)
	n, readErr := io.ReadFull(file, headerBytes)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return "", fmt.Errorf("read image header: %w", readErr)
	}
	contentType := http.DetectContentType(headerBytes[:n])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind image: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filepath.Base(imagePath)}))
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.WriteField("purpose", "image"); err != nil {
		return "", err
	}
	if err := writer.WriteField("ref", filepath.Base(imagePath)); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", g.BaseURL+"/ghost/api/admin/images/upload/", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Ghost "+token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept-Version", "v5.0")

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ghost image upload failed %d: %s", resp.StatusCode, string(body))
	}

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
	req.Header.Set("Accept-Version", "v5.0")

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
