package services

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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
		if err := p.genie.AnalyseTripJob(ctx, job.TargetID, job.ID); err != nil {
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
	if job.Type == domain.JobTypeGenerateStory {
		jobCtx, cancelJob := context.WithCancel(ctx)
		defer cancelJob()
		var cancelledByUser atomic.Bool
		stopCancelCheck := make(chan struct{})
		cancelCheckDone := make(chan struct{})
		go func() {
			defer close(cancelCheckDone)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopCancelCheck:
					return
				case <-ctx.Done():
					return
				case <-ticker.C:
					current, err := p.store.Jobs.Get(ctx, job.ID)
					if err == nil && current.Cancelled {
						cancelledByUser.Store(true)
						cancelJob()
						return
					}
				}
			}
		}()
		p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
			j.CurrentStep = "Preparing the trip for generation"
			j.Progress = 5
		})
		var progressMu sync.Mutex
		lastProgressUpdate := time.Time{}
		stageStartedAt := time.Time{}
		activeStage := ""
		lastCharacters := 0
		stopHeartbeat := make(chan struct{})
		heartbeatDone := make(chan struct{})
		go func() {
			defer close(heartbeatDone)
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopHeartbeat:
					return
				case now := <-ticker.C:
					progressMu.Lock()
					if activeStage == "Writing draft" && now.Sub(lastProgressUpdate) >= 5*time.Second {
						elapsed := now.Sub(stageStartedAt).Round(time.Second)
						step := fmt.Sprintf("Writing draft · waiting for the model · %s elapsed", elapsed)
						if lastCharacters > 0 {
							step = fmt.Sprintf("Writing draft · %s generated · waiting for the model · %s elapsed", formatCharacterCount(lastCharacters), elapsed)
						}
						p.store.Jobs.Update(jobCtx, job.ID, func(j *domain.Job) {
							j.CurrentStep = step
							j.Progress = 35
						})
						lastProgressUpdate = now
					}
					progressMu.Unlock()
				}
			}
		}()
		generationCtx := withModelProgress(jobCtx, func(stage string, characters int) {
			progressMu.Lock()
			defer progressMu.Unlock()
			now := time.Now()
			if stage != activeStage {
				activeStage = stage
				stageStartedAt = now
				lastCharacters = 0
			}
			if characters > 0 {
				lastCharacters = characters
			}
			if characters > 0 && now.Sub(lastProgressUpdate) < time.Second {
				return
			}
			lastProgressUpdate = now
			progress := 10
			step := stage
			switch stage {
			case "Researching destination":
				progress = 10
			case "Destination research complete":
				progress = 30
			case "Writing draft":
				progress = 35
			case "Saving draft":
				progress = 90
			}
			if characters > 0 && stage != "Saving draft" {
				step = fmt.Sprintf("%s · %s generated", stage, formatCharacterCount(characters))
			} else if characters == 0 && (stage == "Researching destination" || stage == "Writing draft") {
				step += " · waiting for the model"
			}
			p.store.Jobs.Update(jobCtx, job.ID, func(j *domain.Job) {
				j.CurrentStep = step
				j.Progress = progress
			})
		})
		generateErr := p.genie.GenerateArticle(generationCtx, job.TargetID)
		close(stopHeartbeat)
		<-heartbeatDone
		close(stopCancelCheck)
		<-cancelCheckDone
		if generateErr != nil {
			if cancelledByUser.Load() {
				p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
					j.State = domain.JobStateCancelled
					j.Cancelled = true
					j.Error = "Generation cancelled"
				})
				return
			}
			p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
				j.State = domain.JobStateFailed
				j.Error = generateErr.Error()
			})
			return
		}
		p.store.Jobs.Update(ctx, job.ID, func(j *domain.Job) {
			j.State = domain.JobStateSucceeded
			j.Step = 1
			j.Progress = 100
			j.CurrentStep = "Draft ready"
		})
	}
}

func formatCharacterCount(characters int) string {
	if characters < 1000 {
		return fmt.Sprintf("%d characters", characters)
	}
	return fmt.Sprintf("%.1fk characters", float64(characters)/1000)
}
