// Package domain defines the core value objects for the travel diary application.
// These structs mirror the spec's canonical data format and are persisted as JSON
// or mapped to SQLite columns by the store layer.
package domain

import (
	"strings"
	"time"
)

// Trip represents a single trip record imported from a directory or upload.
type Trip struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Location       string     `json:"location,omitempty"`
	Notes          string     `json:"notes,omitempty"`
	FeaturePhotoID string     `json:"feature_photo_id,omitempty"`
	SourcePath     string     `json:"source_path"`
	NotesHash      string     `json:"notes_hash"`
	Status         TripStatus `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TripStatus values from spec §9.
type TripStatus string

const (
	StatusImported        TripStatus = "IMPORTED"
	StatusAnalysing       TripStatus = "ANALYSING"
	StatusPhotoReview     TripStatus = "PHOTO_REVIEW"
	StatusPhotosApproved  TripStatus = "PHOTOS_APPROVED"
	StatusGenerating      TripStatus = "GENERATING"
	StatusEditing         TripStatus = "EDITING"
	StatusArticleApproved TripStatus = "ARTICLE_APPROVED"
	StatusExportedDraft   TripStatus = "EXPORTED_DRAFT"
)

// ValidTransitions defines allowed status transitions per the spec's state machine.
var ValidTransitions = map[TripStatus][]TripStatus{
	StatusImported:        {StatusAnalysing},
	StatusAnalysing:       {StatusPhotoReview, StatusPhotosApproved, StatusImported},
	StatusPhotoReview:     {StatusPhotosApproved, StatusAnalysing},
	StatusPhotosApproved:  {StatusGenerating, StatusAnalysing},
	StatusGenerating:      {StatusEditing, StatusPhotosApproved},
	StatusEditing:         {StatusArticleApproved, StatusGenerating},
	StatusArticleApproved: {StatusExportedDraft, StatusEditing},
	StatusExportedDraft:   {StatusEditing},
}

// CanTransition returns true if transitioning from current to next is valid.
func (s TripStatus) CanTransition(next TripStatus) bool {
	allowed, ok := ValidTransitions[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == next {
			return true
		}
	}
	return false
}

// GenerateTripID creates a stable ID from the source path.
func GenerateTripID(sourcePath string) string {
	return "trip_" + strings.ReplaceAll(strings.ToLower(sourcePath), "/", "_")
}
