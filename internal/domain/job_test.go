package domain

import (
	"testing"
	"time"
)

func TestJobIsExpired(t *testing.T) {
	// Active lease
	j := Job{State: JobStateRunning, LeaseExpiry: time.Now().Add(1 * time.Hour)}
	if j.IsExpired() {
		t.Error("active job should not be expired")
	}

	// Expired lease
	j.LeaseExpiry = time.Now().Add(-1 * time.Hour)
	if !j.IsExpired() {
		t.Error("expired job should be expired")
	}

	// Non-running job
	j.State = JobStateQueued
	if j.IsExpired() {
		t.Error("queued job should not be expired")
	}
}
