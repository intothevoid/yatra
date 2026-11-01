package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// GroundingAuditor verifies article claims against evidence.
type GroundingAuditor struct {
	store *store.Store
}

// NewGroundingAuditor creates a new auditor.
func NewGroundingAuditor(s *store.Store) *GroundingAuditor {
	return &GroundingAuditor{store: s}
}

// Audit checks an article document against evidence and returns unresolved issues.
func (a *GroundingAuditor) Audit(ctx context.Context, tripID string, doc *domain.ArticleDocument) []AuditIssue {
	experiences, err := a.store.GetExperiences(ctx, tripID)
	if err != nil {
		return []AuditIssue{{Type: "error", Detail: "cannot load experiences: " + err.Error()}}
	}

	evidence := buildEvidenceIndex(experiences)

	var issues []AuditIssue

	// Check each paragraph block
	for _, block := range doc.Blocks {
		if block.Type != domain.BlockParagraph {
			continue
		}

		// Check for unsupported claims
		claims := extractClaims(block.Text)
		for _, claim := range claims {
			if !isSupported(claim, evidence) {
				issues = append(issues, AuditIssue{
					Type:    "unsupported_claim",
					BlockID: block.ID,
					Detail:  fmt.Sprintf("claim not found in evidence: %q", claim),
				})
			}
		}
	}

	return issues
}

// AuditIssue represents an unresolved issue in the article.
type AuditIssue struct {
	Type    string `json:"type"`
	BlockID string `json:"block_id,omitempty"`
	Detail  string `json:"detail"`
}

type evidenceItem struct {
	text     string
	location string
	foods    string
}

func buildEvidenceIndex(experiences []store.Experience) []evidenceItem {
	var items []evidenceItem
	for _, exp := range experiences {
		items = append(items, evidenceItem{
			text:     exp.NoteSpans,
			location: exp.Location,
			foods:    exp.Foods,
		})
	}
	return items
}

func extractClaims(text string) []string {
	// Simple claim extraction: split by sentences
	var claims []string
	for _, sentence := range splitSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) > 20 {
			claims = append(claims, sentence)
		}
	}
	return claims
}

func splitSentences(text string) []string {
	// Simple sentence splitting
	var sentences []string
	var current strings.Builder
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			sentences = append(sentences, current.String())
			current.Reset()
		}
	}
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}
	return sentences
}

func isSupported(claim string, evidence []evidenceItem) bool {
	claimLower := strings.ToLower(claim)
	for _, item := range evidence {
		itemLower := strings.ToLower(item.text)
		// Check if claim words appear in evidence
		words := strings.Fields(claimLower)
		matches := 0
		for _, word := range words {
			if len(word) > 3 && strings.Contains(itemLower, word) {
				matches++
			}
		}
		// Support if >50% of words match
		if len(words) > 0 && matches > len(words)/2 {
			return true
		}
	}
	return false
}
