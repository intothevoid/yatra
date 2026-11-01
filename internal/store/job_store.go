package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/karan/travel-diary/internal/domain"
)

// JobStore provides CRUD operations for the job queue.
type JobStore struct {
	db *sql.DB
}

// Create inserts a new job.
func (s *JobStore) Create(ctx context.Context, job domain.Job) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, target_id, state, step, attempt_count, cancelled, idempotency_key, created_at, updated_at)
		VALUES (?, ?, ?, 'queued', 0, 0, 0, ?, ?, ?)
	`, job.ID, job.Type, job.TargetID, job.IdempotencyKey, now, now)
	return err
}

// Claim picks the next queued job and marks it as running with a lease.
func (s *JobStore) Claim(ctx context.Context, workerID string) (*domain.Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Reclaim expired leases
	expired := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `
		UPDATE jobs SET state = 'queued', leased_by = NULL, lease_expiry = NULL
		WHERE state = 'running' AND lease_expiry < ?
	`, expired)
	if err != nil {
		return nil, err
	}

	// Claim next queued job
	var job domain.Job
	var leasedBy, leaseExpiryStr, createdAt, updatedAt string
	var jobError sql.NullString
	var cancelled int
	leaseExpiry := time.Now().UTC().Add(30 * time.Second).Format(time.RFC3339)
	err = tx.QueryRowContext(ctx, `
		UPDATE jobs SET state = 'running', leased_by = ?, lease_expiry = ?, updated_at = ?
		WHERE id = (
			SELECT id FROM jobs
			WHERE state = 'queued' AND cancelled = 0
			ORDER BY created_at ASC LIMIT 1
		)
		RETURNING id, type, target_id, state, step, attempt_count, leased_by, lease_expiry, error, cancelled, idempotency_key, created_at, updated_at
	`, workerID, leaseExpiry, expired).Scan(
		&job.ID, &job.Type, &job.TargetID, &job.State, &job.Step, &job.AttemptCount,
		&leasedBy, &leaseExpiryStr, &jobError, &cancelled, &job.IdempotencyKey,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // no jobs to claim
		}
		return nil, err
	}
	if leasedBy != "" {
		job.LeasedBy = leasedBy
	}
	if leaseExpiryStr != "" {
		job.LeaseExpiry, _ = time.Parse(time.RFC3339, leaseExpiryStr)
	}
	if jobError.Valid {
		job.Error = jobError.String
	}
	job.Cancelled = cancelled == 1
	job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &job, tx.Commit()
}

// Update modifies a job's state and metadata.
func (s *JobStore) Update(ctx context.Context, jobID string, fn func(*domain.Job)) error {
	job, err := s.Get(ctx, jobID)
	if err != nil {
		return err
	}
	fn(job)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `
		UPDATE jobs SET state = ?, step = ?, attempt_count = ?, error = ?, cancelled = ?, updated_at = ?
		WHERE id = ?
	`, job.State, job.Step, job.AttemptCount, job.Error, job.Cancelled, now, jobID)
	return err
}

// Get retrieves a job by ID.
func (s *JobStore) Get(ctx context.Context, id string) (*domain.Job, error) {
	var job domain.Job
	var leasedBy, leaseExpiry, idempKey, createdAt, updatedAt sql.NullString
	var jobError sql.NullString
	var cancelled int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, type, target_id, state, step, attempt_count, leased_by, lease_expiry,
			error, cancelled, idempotency_key, created_at, updated_at
		FROM jobs WHERE id = ?
	`, id).Scan(&job.ID, &job.Type, &job.TargetID, &job.State, &job.Step, &job.AttemptCount,
		&leasedBy, &leaseExpiry, &jobError, &cancelled, &idempKey, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	if leasedBy.Valid {
		job.LeasedBy = leasedBy.String
	}
	if leaseExpiry.Valid {
		job.LeaseExpiry, _ = time.Parse(time.RFC3339, leaseExpiry.String)
	}
	if jobError.Valid {
		job.Error = jobError.String
	}
	if idempKey.Valid {
		job.IdempotencyKey = idempKey.String
	}
	job.Cancelled = cancelled == 1
	if createdAt.Valid {
		job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}
	return &job, nil
}

// Cancel marks a job as cancelled.
func (s *JobStore) Cancel(ctx context.Context, jobID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET cancelled = 1, state = 'cancelled', updated_at = datetime('now')
		WHERE id = ?
	`, jobID)
	return err
}

// ListQueued returns all queued jobs.
func (s *JobStore) ListQueued(ctx context.Context) ([]domain.Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, target_id, state, step, attempt_count, leased_by, lease_expiry,
			error, cancelled, idempotency_key, created_at, updated_at
		FROM jobs WHERE state = 'queued' AND cancelled = 0
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		var job domain.Job
		var leasedBy, leaseExpiry, createdAt, updatedAt string
		var cancelled int
		if err := rows.Scan(&job.ID, &job.Type, &job.TargetID, &job.State, &job.Step, &job.AttemptCount,
			&leasedBy, &leaseExpiry, &job.Error, &cancelled, &job.IdempotencyKey, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if leasedBy != "" {
			job.LeasedBy = leasedBy
		}
		if leaseExpiry != "" {
			job.LeaseExpiry, _ = time.Parse(time.RFC3339, leaseExpiry)
		}
		job.Cancelled = cancelled == 1
		job.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		job.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
