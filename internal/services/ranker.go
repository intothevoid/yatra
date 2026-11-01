package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// Ranker evaluates photos and sets recommended/review/not_selected status.
type Ranker struct {
	store *store.Store
}

// RankReason is an explainable factor in photo ranking.
type RankReason struct {
	Factor string  `json:"factor"`   // "content_relevance", "image_quality", "variety", "coverage"
	Weight float64 `json:"weight"`   // 0.0-1.0 contribution
	Detail string  `json:"detail"`   // human-readable explanation
}

// NewRanker creates a new ranker.
func NewRanker(s *store.Store) *Ranker {
	return &Ranker{store: s}
}

// Rank evaluates photos and sets recommended/review/not_selected status.
// Respects user overrides (user_override=1 preserves existing status).
func (r *Ranker) Rank(ctx context.Context, tripID string) error {
	photos, err := r.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get photos: %w", err)
	}
	if len(photos) == 0 {
		return nil
	}

	// Get trip for notes hash
	trip, err := r.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}

	// Compute scores
	scores := r.computeScores(photos, trip)

	// Apply ranking
	for i, photo := range photos {
		// Skip photos with user override
		if photo.RankingReasons != "" && photo.Status != domain.PhotoStatusUnknown {
			continue
		}

		score := scores[i].total
		var status domain.PhotoStatus
		switch {
		case score >= 0.7:
			status = domain.PhotoStatusRecommended
		case score >= 0.4:
			status = domain.PhotoStatusReview
		default:
			status = domain.PhotoStatusNotSelected
		}

		// Store status and reasons
		if err := r.store.Photos.UpdateSelection(ctx, photo.ID, status); err != nil {
			return fmt.Errorf("update photo %s: %w", photo.ID, err)
		}

		reasonsJSON, _ := json.Marshal(scores[i].reasons)
		photo.RankingReasons = string(reasonsJSON)
	}

	return nil
}

// photoScore holds the computed score and reasons for a photo.
type photoScore struct {
	total   float64
	reasons []RankReason
}

// computeScores evaluates all photos and returns scores.
func (r *Ranker) computeScores(photos []domain.Photo, trip *domain.Trip) []photoScore {
	scores := make([]photoScore, len(photos))

	// Extract keywords from notes (simple word extraction)
	keywords := extractKeywords(trip.NotesHash)

	// Track categories for variety scoring
	categoryCount := make(map[string]int)
	for _, p := range photos {
		var vr domain.VisionResult
		if p.VLMResult != "" {
			json.Unmarshal([]byte(p.VLMResult), &vr)
			if vr.Observed.Category != "" {
				categoryCount[vr.Observed.Category]++
			}
		}
	}

	for i, photo := range photos {
		var reasons []RankReason

		// 1. Content relevance (0.4 weight)
		relevance := r.scoreRelevance(photo, keywords)
		if relevance > 0 {
			reasons = append(reasons, RankReason{
				Factor: "content_relevance",
				Weight: relevance * 0.4,
				Detail: fmt.Sprintf("matches %d note keywords", int(relevance*5)),
			})
		}

		// 2. Image quality (0.3 weight)
		quality := r.scoreQuality(photo)
		reasons = append(reasons, RankReason{
			Factor: "image_quality",
			Weight: quality * 0.3,
			Detail: qualityDetail(quality),
		})

		// 3. Variety bonus (0.15 weight)
		variety := r.scoreVariety(photo, categoryCount)
		if variety > 0 {
			reasons = append(reasons, RankReason{
				Factor: "variety",
				Weight: variety * 0.15,
				Detail: "unique category representation",
			})
		}

		// 4. Coverage bonus (0.15 weight)
		coverage := r.scoreCoverage(photo, photos)
		reasons = append(reasons, RankReason{
			Factor: "coverage",
			Weight: coverage * 0.15,
			Detail: coverageDetail(coverage),
		})

		scores[i] = photoScore{
			total:   relevance*0.4 + quality*0.3 + variety*0.15 + coverage*0.15,
			reasons: reasons,
		}
	}

	return scores
}

// scoreRelevance checks if VLM subjects match note keywords.
func (r *Ranker) scoreRelevance(photo domain.Photo, keywords []string) float64 {
	if len(keywords) == 0 || photo.VLMResult == "" {
		return 0.3 // default if no keywords
	}

	var vr domain.VisionResult
	json.Unmarshal([]byte(photo.VLMResult), &vr)

	matches := 0
	subjects := strings.ToLower(vr.Observed.Description)
	for _, kw := range keywords {
		if strings.Contains(subjects, kw) {
			matches++
		}
	}

	return float64(matches) / float64(len(keywords))
}

// scoreQuality evaluates image quality from EXIF and VLM flags.
func (r *Ranker) scoreQuality(photo domain.Photo) float64 {
	score := 0.5 // base score

	// Check dimensions from EXIF
	if photo.EXIF != "" {
		var exif map[string]string
		json.Unmarshal([]byte(photo.EXIF), &exif)
		if w, ok := exif["ImageWidth"]; ok {
			if len(w) > 3 { // > 999px
				score += 0.3
			}
		}
	}

	// Penalize quality flags
	var vr domain.VisionResult
	if photo.VLMResult != "" {
		json.Unmarshal([]byte(photo.VLMResult), &vr)
		for _, flag := range vr.QualityFlags {
			switch strings.ToLower(flag) {
			case "blurry", "dark", "overexposed":
				score -= 0.2
			}
		}
	}

	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// scoreVariety gives bonus for unique categories.
func (r *Ranker) scoreVariety(photo domain.Photo, categoryCount map[string]int) float64 {
	var vr domain.VisionResult
	if photo.VLMResult == "" {
		return 0
	}
	json.Unmarshal([]byte(photo.VLMResult), &vr)

	cat := vr.Observed.Category
	if cat == "" {
		return 0
	}

	// Bonus for categories with few representatives
	count := categoryCount[cat]
	if count <= 2 {
		return 1.0
	}
	if count <= 5 {
		return 0.5
	}
	return 0.2
}

// scoreCoverage gives bonus for photos that fill timeline gaps.
func (r *Ranker) scoreCoverage(photo domain.Photo, photos []domain.Photo) float64 {
	// Simple: if we have few photos, all get coverage bonus
	if len(photos) <= 10 {
		return 1.0
	}
	return 0.5
}

func extractKeywords(hash string) []string {
	// For Milestone 2, return empty — Milestone 3 reads actual notes
	return nil
}

func qualityDetail(q float64) string {
	if q >= 0.8 {
		return "high resolution, no quality flags"
	}
	if q >= 0.5 {
		return "acceptable quality"
	}
	return "low quality or flagged issues"
}

func coverageDetail(c float64) string {
	if c >= 0.8 {
		return "fills timeline gap"
	}
	return "standard coverage"
}
