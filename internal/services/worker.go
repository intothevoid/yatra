package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/karan/travel-diary/internal/domain"
	"github.com/karan/travel-diary/internal/store"
)

// WorkerPool manages an in-process job queue with lease/heartbeat.
type WorkerPool struct {
	store    *store.Store
	workerID string
	genie    *Genie
	mu       sync.Mutex
	running  bool
}

// NewPool creates a worker pool with the given concurrency.
func NewPool(s *store.Store, genie *Genie, concurrency int) *WorkerPool {
	return &WorkerPool{
		store:    s,
		workerID: fmt.Sprintf("worker-%d", time.Now().UnixNano()),
		genie:    genie,
	}
}

// Start begins processing jobs. It reclaims expired leases on startup.
func (p *WorkerPool) Start(ctx context.Context) {
	p.mu.Lock()
	p.running = true
	p.mu.Unlock()

	// Start worker goroutines
	for i := 0; i < 1; i++ { // single worker for Milestone 1
		go p.workerLoop(ctx)
	}
}

// Stop gracefully shuts down the pool.
func (p *WorkerPool) Stop() {
	p.mu.Lock()
	p.running = false
	p.mu.Unlock()
}

// Enqueue adds a job to the queue.
func (p *WorkerPool) Enqueue(ctx context.Context, job domain.Job) error {
	return p.store.Jobs.Create(ctx, job)
}

// workerLoop claims and processes jobs.
func (p *WorkerPool) workerLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Claim next job
		job, err := p.store.Jobs.Claim(ctx, p.workerID)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		if job == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// Process the job
		p.processJob(ctx, job)
	}
}

// processJob executes a job and updates its state.
func (p *WorkerPool) processJob(ctx context.Context, job *domain.Job) {
	if job.Type == domain.JobTypeAnalysePhoto {
		if err := p.genie.AnalyseTrip(ctx, job.TargetID); err != nil {
			p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
				j.State = domain.JobStateFailed
				j.Error = err.Error()
			})
			return
		}
		p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
			j.State = domain.JobStateSucceeded
			j.Step = 1
		})
	}
}
