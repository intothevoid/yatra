package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/karan/travel-diary/internal/adapters"
	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// EvidenceExtractor parses notes and extracts structured experiences.
type EvidenceExtractor struct {
	store *store.Store
	text  adapters.TextAdapter
}

// Experience represents a structured event extracted from notes.
type Experience struct {
	ID          string   `json:"id"`
	TripID      string   `json:"trip_id"`
	NoteSpans   []Span   `json:"note_spans"`
	Location    string   `json:"location,omitempty"`
	Foods       []string `json:"foods,omitempty"`
	Sentiment   string   `json:"sentiment,omitempty"`
	Uncertainty []string `json:"uncertainty,omitempty"`
}

// Span is a character offset range in the notes file.
type Span struct {
	Line  int    `json:"line"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// NewEvidenceExtractor creates a new extractor.
func NewEvidenceExtractor(s *store.Store, text adapters.TextAdapter) *EvidenceExtractor {
	return &EvidenceExtractor{store: s, text: text}
}

// Extract parses notes and creates Experience records.
func (e *EvidenceExtractor) Extract(ctx context.Context, tripID string) error {
	trip, err := e.store.Trips.Get(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get trip: %w", err)
	}

	notes := []byte(trip.Notes)
	if trip.Notes == "" && trip.SourcePath != "" {
		// Preserve compatibility with older folder imports that still have story.txt.
		if b, readErr := os.ReadFile(trip.SourcePath + "/story.txt"); readErr == nil {
			notes = b
		}
	}

	// Parse line by line
	experiences := e.parseLineByLine(string(notes))
	if trip.Location != "" {
		experiences = append([]Experience{{Location: trip.Location, NoteSpans: []Span{{Text: "Trip destination: " + trip.Location}}}}, experiences...)
	}

	// Store experiences
	for i := range experiences {
		experiences[i].ID = fmt.Sprintf("e_%s_%d", tripID, i+1)
		experiences[i].TripID = tripID
		if err := e.store.SaveExperience(ctx, experiences[i]); err != nil {
			return fmt.Errorf("save experience: %w", err)
		}
	}

	return nil
}

func (e *EvidenceExtractor) parseLineByLine(notes string) []Experience {
	var experiences []Experience
	lines := strings.Split(notes, "\n")
	offset := 0
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			offset += len(lines[i]) + 1
			continue
		}
		span := Span{Line: i + 1, Start: offset, End: offset + len(line), Text: line}
		exp := Experience{NoteSpans: []Span{span}}
		if loc := extractLocation(line); loc != "" {
			exp.Location = loc
		}
		if foods := extractFoods(line); len(foods) > 0 {
			exp.Foods = foods
		}
		experiences = append(experiences, exp)
		offset += len(lines[i]) + 1
	}
	return experiences
}

var locationWords = []string{"kyoto", "tokyo", "osaka", "nara", "hiroshima", "fushimi", "kiyomizu", "inar"}
var foodWords = []string{"ramen", "sushi", "tempura", "udon", "matcha", "mochi", "yakitori"}

func extractLocation(line string) string {
	lower := strings.ToLower(line)
	for _, word := range locationWords {
		if strings.Contains(lower, word) {
			return strings.Title(word)
		}
	}
	return ""
}

func extractFoods(line string) []string {
	lower := strings.ToLower(line)
	var foods []string
	for _, word := range foodWords {
		if strings.Contains(lower, word) {
			foods = append(foods, word)
		}
	}
	return foods
}

// PhotoMatcher matches approved photos to experiences.
type PhotoMatcher struct {
	store *store.Store
}

// NewPhotoMatcher creates a new matcher.
func NewPhotoMatcher(s *store.Store) *PhotoMatcher {
	return &PhotoMatcher{store: s}
}

// Match links approved photos to experiences.
func (m *PhotoMatcher) Match(ctx context.Context, tripID string) error {
	experiences, err := m.store.GetExperiences(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get experiences: %w", err)
	}

	photos, err := m.store.Photos.GetByTrip(ctx, tripID)
	if err != nil {
		return fmt.Errorf("get photos: %w", err)
	}

	approved := make([]domain.Photo, 0, len(photos))
	for _, p := range photos {
		if p.Status == domain.PhotoStatusRecommended || p.Status == domain.PhotoStatusReview {
			approved = append(approved, p)
		}
	}

	for _, exp := range experiences {
		for _, photo := range approved {
			score := m.matchScore(exp, photo)
			if score > 0.3 {
				match := domain.PhotoMatch{
					ExperienceID:  exp.ID,
					PhotoID:       photo.ID,
					Score:         score,
					Justification: fmt.Sprintf("semantic match score %.2f", score),
				}
				if err := m.store.SavePhotoMatch(ctx, match); err != nil {
					return fmt.Errorf("save match: %w", err)
				}
			}
		}
	}

	return nil
}

func (m *PhotoMatcher) matchScore(exp store.Experience, photo domain.Photo) float64 {
	var score float64
	var vr domain.VisionResult
	if photo.VLMResult != "" {
		json.Unmarshal([]byte(photo.VLMResult), &vr)
		// NoteSpans is JSON, extract text
		var spans []struct{ Text string }
		json.Unmarshal([]byte(exp.NoteSpans), &spans)
		if len(spans) > 0 {
			text := strings.ToLower(spans[0].Text)
			for _, subject := range vr.Observed.Subjects {
				if strings.Contains(text, strings.ToLower(subject)) {
					score += 0.3
				}
			}
			if exp.Location != "" {
				for _, h := range vr.Hypotheses {
					if strings.Contains(strings.ToLower(h.Value), strings.ToLower(exp.Location)) {
						score += 0.4
					}
				}
			}
			// Foods is JSON array string
			var foods []string
			json.Unmarshal([]byte(exp.Foods), &foods)
			for _, food := range foods {
				if strings.Contains(strings.ToLower(vr.Observed.Description), food) {
					score += 0.2
				}
			}
		}
	}
	return score
}

// ensure regexp is used
var _ = regexp.MustCompile
