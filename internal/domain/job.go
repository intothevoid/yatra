package domain

import (
	"time"
)

// Job represents a unit of work in the in-process worker pool.
type Job struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	TargetID       string    `json:"target_id"`
	State          JobState  `json:"state"`
	Step           int       `json:"step"`
	AttemptCount   int       `json:"attempt_count"`
	LeasedBy       string    `json:"leased_by,omitempty"`
	LeaseExpiry    time.Time `json:"lease_expiry,omitempty"`
	Error          string    `json:"error,omitempty"`
	Cancelled      bool      `json:"cancelled"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	Progress       int       `json:"progress"` // 0-100 percentage
	CurrentStep    string    `json:"current_step,omitempty"`
	TotalItems     int       `json:"total_items"`
	CompletedItems int       `json:"completed_items"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// JobState values from spec §9.
type JobState string

const (
	JobStateQueued    JobState = "queued"
	JobStateRunning   JobState = "running"
	JobStateSucceeded JobState = "succeeded"
	JobStateFailed    JobState = "failed"
	JobStateCancelled JobState = "cancelled"
	JobStateRetryWait JobState = "retry_wait"
)

// Job types used by the application.
const (
	JobTypeAnalysePhoto  = "analyse_photo"
	JobTypeGenerateStory = "generate_story"
	JobTypeCreatePreview = "create_preview"
)

// PhotoMatch links an experience to a photo.
type PhotoMatch struct {
	ExperienceID  string  `json:"experience_id"`
	PhotoID       string  `json:"photo_id"`
	Score         float64 `json:"score"`
	Justification string  `json:"justification"`
	UserOverride  bool    `json:"user_override"`
}

// IsExpired returns true if the lease has passed its expiry.
func (j *Job) IsExpired() bool {
	return j.State == JobStateRunning && time.Now().After(j.LeaseExpiry)
}
