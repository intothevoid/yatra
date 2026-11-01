package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// StyleProfiler imports and analyzes writing style from Ghost posts.
type StyleProfiler struct {
	store *store.Store
	ghost *GhostClient
}

// NewStyleProfiler creates a new profiler.
func NewStyleProfiler(s *store.Store, ghost *GhostClient) *StyleProfiler {
	return &StyleProfiler{store: s, ghost: ghost}
}

// ImportFromGhost fetches posts from Ghost and builds a style profile.
func (sp *StyleProfiler) ImportFromGhost(ctx context.Context, profileID string) (*domain.StyleProfile, error) {
	if sp.ghost == nil {
		return nil, fmt.Errorf("ghost client not configured")
	}

	posts, err := sp.ghost.GetPosts(ctx, 5)
	if err != nil {
		return nil, fmt.Errorf("get posts: %w", err)
	}
	if len(posts) == 0 {
		return nil, fmt.Errorf("no posts found")
	}

	profile := sp.analyzeStyle(posts)
	profile.ID = profileID
	profile.Version = 1

	if err := sp.store.SaveStyleProfile(ctx, profile); err != nil {
		return nil, fmt.Errorf("save profile: %w", err)
	}

	return profile, nil
}

func (sp *StyleProfiler) analyzeStyle(posts []GhostPost) *domain.StyleProfile {
	var totalLen int
	var sampleExcerpt string

	for _, p := range posts {
		text := stripHTML(p.HTML)
		totalLen += len(text)
		if sampleExcerpt == "" {
			sampleExcerpt = text[:min(200, len(text))]
		}
	}

	avgLen := totalLen / len(posts)
	paragraphLen := "medium"
	if avgLen < 500 {
		paragraphLen = "short"
	} else if avgLen > 1500 {
		paragraphLen = "long"
	}

	return &domain.StyleProfile{
		Name:           "Default",
		ParagraphLen:   paragraphLen,
		Perspective:    "first_person",
		Tone:           "warm",
		Humour:         "subtle",
		SectionOrg:     "chronological",
		CaptionStyle:   "descriptive",
		Representative: sampleExcerpt,
	}
}

func stripHTML(html string) string {
	var result strings.Builder
	inTag := false
	for _, r := range html {
		if r == '<' {
			inTag = true
		} else if r == '>' {
			inTag = false
			result.WriteByte(' ')
		} else if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
