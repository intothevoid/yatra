package domain

import (
	"time"
)

// Job represents a unit of work in the in-process worker pool.
type Job struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	TargetID      string    `json:"target_id"`
	State         JobState  `json:"state"`
	Step          int       `json:"step"`
	AttemptCount  int       `json:"attempt_count"`
	LeasedBy      string    `json:"leased_by,omitempty"`
	LeaseExpiry   time.Time `json:"lease_expiry,omitempty"`
	Error         string    `json:"error,omitempty"`
	Cancelled     bool      `json:"cancelled"`
	IdempotencyKey string   `json:"idempotency_key,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	JobTypeAnalysePhoto = "analyse_photo"
	JobTypeGenerateStory = "generate_story"
	JobTypeCreatePreview = "create_preview"
)

// IsExpired returns true if the lease has passed its expiry.
func (j *Job) IsExpired() bool {
	return j.State == JobStateRunning && time.Now().After(j.LeaseExpiry)
}
