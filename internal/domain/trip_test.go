package domain

import (
	"testing"
)

func TestTripStatusTransitions(t *testing.T) {
	tests := []struct {
		from    TripStatus
		to      TripStatus
		allowed bool
	}{
		{StatusImported, StatusAnalysing, true},
		{StatusImported, StatusPhotoReview, false},
		{StatusAnalysing, StatusPhotoReview, true},
		{StatusAnalysing, StatusImported, true},
		{StatusPhotoReview, StatusPhotosApproved, true},
		{StatusPhotosApproved, StatusGenerating, true},
		{StatusGenerating, StatusEditing, true},
		{StatusEditing, StatusArticleApproved, true},
		{StatusArticleApproved, StatusExportedDraft, true},
		{StatusExportedDraft, StatusEditing, true},
		{StatusExportedDraft, StatusImported, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			got := tt.from.CanTransition(tt.to)
			if got != tt.allowed {
				t.Errorf("CanTransition(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.allowed)
			}
		})
	}
}

func TestGenerateTripID(t *testing.T) {
	id := GenerateTripID("/Users/testuser/trips/Japan-2026")
	if id != "trip__users_testuser_trips_japan-2026" {
		t.Errorf("GenerateTripID() = %q, want %q", id, "trip__users_testuser_trips_japan-2026")
	}
}
