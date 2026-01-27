package webhook

import (
	"context"
	"fmt"
	"time"

	"mockgatehub/internal/logger"
)

// Worker processes webhook jobs from the queue
type Worker struct {
	queue   *Queue
	manager *Manager
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewWorker creates a new webhook worker
func NewWorker(queue *Queue, manager *Manager) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		queue:   queue,
		manager: manager,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins processing webhook jobs (blocking)
func (w *Worker) Start() {
	logger.Info.Printf("[WORKER] Starting webhook worker (poll interval: %v)", pollInterval)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info.Printf("[WORKER] Stopping webhook worker")
			return
		case <-ticker.C:
			w.processReadyJobs()
		}
	}
}

// Stop gracefully stops the worker
func (w *Worker) Stop() {
	w.cancel()
}

// processReadyJobs fetches and processes all ready jobs sequentially
func (w *Worker) processReadyJobs() {
	jobs, err := w.queue.GetReadyJobs(w.ctx, batchSize)
	if err != nil {
		logger.Error.Printf("[WORKER] Failed to fetch ready jobs: %v", err)
		return
	}

	if len(jobs) == 0 {
		return // No jobs ready
	}

	logger.Info.Printf("[WORKER] Found %d ready job(s) to process", len(jobs))

	for _, job := range jobs {
		w.processJob(job)
	}
}

// processJob processes a single webhook job
func (w *Worker) processJob(job *Job) {
	logger.Info.Printf("[WORKER] Processing job: id=%s, event=%s, user=%s, attempt=%d/%d",
		job.ID, job.EventType, job.UserID, job.Attempts+1, maxAttempts)

	// Send webhook using manager's send method
	err := w.manager.send(job.EventType, job.UserID, job.Data)

	if err != nil {
		// Mark as failed and reschedule
		errMsg := err.Error()
		if markErr := w.queue.MarkFailed(w.ctx, job.ID, errMsg); markErr != nil {
			logger.Error.Printf("[WORKER] Failed to mark job as failed: %v", markErr)
		}
		logger.Warn.Printf("[WORKER] Job failed: id=%s, error=%s", job.ID, errMsg)
		return
	}

	// Mark as completed
	if err := w.queue.MarkCompleted(w.ctx, job.ID); err != nil {
		logger.Error.Printf("[WORKER] Failed to mark job as completed: %v", err)
		return
	}

	logger.Info.Printf("[WORKER] ✅ Job completed successfully: id=%s", job.ID)
}

// StartAsync starts the worker in a goroutine
func (w *Worker) StartAsync() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error.Printf("[WORKER] Panic in webhook worker: %v", r)
				// Restart worker after panic
				time.Sleep(5 * time.Second)
				logger.Info.Printf("[WORKER] Restarting webhook worker after panic")
				w.StartAsync()
			}
		}()
		w.Start()
	}()
}

// GetStats returns queue statistics (for debugging/monitoring)
func (w *Worker) GetStats(ctx context.Context) (map[string]int64, error) {
	// Count total jobs in queue
	totalInQueue, err := w.queue.client.ZCard(ctx, queueKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to count queue size: %w", err)
	}

	// Count ready jobs
	now := time.Now().Unix()
	readyCount, err := w.queue.client.ZCount(ctx, queueKey, "-inf", fmt.Sprintf("%d", now)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to count ready jobs: %w", err)
	}

	return map[string]int64{
		"total_in_queue": totalInQueue,
		"ready_now":      readyCount,
		"scheduled":      totalInQueue - readyCount,
	}, nil
}
