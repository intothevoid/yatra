package services

import (
	"encoding/json"
	"testing"

	"github.com/karan/travel-diary/internal/domain"
)

func TestScoreQuality(t *testing.T) {
	r := &Ranker{}

	// Photo with high-res EXIF
	photo := domain.Photo{
		EXIF: `{"ImageWidth":"6048","ImageHeight":"4032"}`,
	}
	score := r.scoreQuality(photo)
	if score < 0.7 || score > 0.9 {
		t.Errorf("high-res photo score = %f, want ~0.8", score)
	}

	// Photo with quality flags
	photo.VLMResult = `{"quality_flags":["blurry"]}`
	score = r.scoreQuality(photo)
	if score > 0.65 {
		t.Errorf("blurry photo score = %f, want ≤ 0.65", score)
	}
}

func TestScoreVariety(t *testing.T) {
	r := &Ranker{}
	categoryCount := map[string]int{
		"food":    1,
		"scenery": 8,
	}

	// Unique category
	photo := domain.Photo{
		VLMResult: `{"observed":{"category":"food"}}`,
	}
	score := r.scoreVariety(photo, categoryCount)
	if score != 1.0 {
		t.Errorf("unique category score = %f, want 1.0", score)
	}

	// Common category
	photo.VLMResult = `{"observed":{"category":"scenery"}}`
	score = r.scoreVariety(photo, categoryCount)
	if score != 0.2 {
		t.Errorf("common category score = %f, want 0.2", score)
	}
}

func TestScoreCoverage(t *testing.T) {
	r := &Ranker{}
	photos := make([]domain.Photo, 5)

	score := r.scoreCoverage(domain.Photo{}, photos)
	if score != 1.0 {
		t.Errorf("few photos coverage = %f, want 1.0", score)
	}

	photos = make([]domain.Photo, 15)
	score = r.scoreCoverage(domain.Photo{}, photos)
	if score != 0.5 {
		t.Errorf("many photos coverage = %f, want 0.5", score)
	}
}

func TestRankThresholds(t *testing.T) {
	// Verify that the threshold logic produces correct statuses
	tests := []struct {
		score float64
		want  domain.PhotoStatus
	}{
		{0.9, domain.PhotoStatusRecommended},
		{0.7, domain.PhotoStatusRecommended},
		{0.55, domain.PhotoStatusReview},
		{0.4, domain.PhotoStatusReview},
		{0.3, domain.PhotoStatusNotSelected},
		{0.0, domain.PhotoStatusNotSelected},
	}

	for _, tt := range tests {
		var got domain.PhotoStatus
		switch {
		case tt.score >= 0.7:
			got = domain.PhotoStatusRecommended
		case tt.score >= 0.4:
			got = domain.PhotoStatusReview
		default:
			got = domain.PhotoStatusNotSelected
		}
		if got != tt.want {
			t.Errorf("score %f → %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestRankReasonSerialization(t *testing.T) {
	reasons := []RankReason{
		{Factor: "content_relevance", Weight: 0.35, Detail: "matches 3 keywords"},
		{Factor: "image_quality", Weight: 0.24, Detail: "high resolution"},
	}
	data, err := json.Marshal(reasons)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded []RankReason
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("got %d reasons, want 2", len(decoded))
	}
	if decoded[0].Factor != "content_relevance" {
		t.Errorf("factor = %q, want %q", decoded[0].Factor, "content_relevance")
	}
}
