package webhook

import (
	"context"
	"time"

	"mockgatehub/internal/logger"

	"go.uber.org/zap"
)

const staleIdleDuration = 2 * time.Minute // Reclaim messages idle for >2 min

// Worker processes webhook jobs from the Redis Stream consumer group.
// Instead of polling on a ticker, it blocks on XREADGROUP so jobs are
// picked up immediately when they arrive.
type Worker struct {
	queue   *Queue
	manager *Manager
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewWorker creates a new webhook worker.
func NewWorker(queue *Queue, manager *Manager) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Worker{
		queue:   queue,
		manager: manager,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start begins processing webhook jobs (blocking).
// It first reclaims any stale messages left by a previous process, then
// enters a blocking read loop that wakes only when messages arrive.
func (w *Worker) Start() {
	logger.Info("starting webhook worker (stream mode)",
		zap.Duration("block_timeout", blockTimeout),
		zap.String("consumer", w.queue.consumerName),
	)

	// Recover messages that a crashed worker left unacknowledged.
	w.reclaimStale()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info("stopping webhook worker")
			return
		default:
			w.readAndProcess()
		}
	}
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	w.cancel()
}

// readAndProcess does a single blocking XREADGROUP and processes the batch.
func (w *Worker) readAndProcess() {
	jobs, err := w.queue.ReadJobs(w.ctx, batchSize)
	if err != nil {
		// Context canceled → shutdown in progress.
		if w.ctx.Err() != nil {
			return
		}
		logger.Error("failed to read jobs from stream", zap.Error(err))
		time.Sleep(1 * time.Second) // Back off briefly on transient errors
		return
	}

	for _, job := range jobs {
		w.processJob(job)
	}
}

// processJob sends a single webhook and acks or retries the stream message.
func (w *Worker) processJob(job StreamJob) {
	logger.Info("processing webhook job",
		zap.String("job_id", job.JobID),
		zap.String("event", job.EventType),
		zap.String("user", job.UserID),
		zap.Int("attempt", job.Attempts+1),
		zap.Int("max_attempts", maxAttempts),
	)

	err := w.manager.send(job.EventType, job.UserID, job.Data)

	if err != nil {
		if retryErr := w.queue.RetryLater(w.ctx, job, err.Error()); retryErr != nil {
			logger.Error("failed to schedule retry", zap.Error(retryErr))
		}
		logger.Warn("webhook job failed",
			zap.String("job_id", job.JobID),
			zap.String("error", err.Error()),
		)
		return
	}

	if ackErr := w.queue.Ack(w.ctx, job.MessageID); ackErr != nil {
		logger.Error("failed to ack completed job", zap.Error(ackErr))
		return
	}

	logger.Info("webhook job completed successfully", zap.String("job_id", job.JobID))
}

// reclaimStale uses XAUTOCLAIM to pick up messages that another consumer
// (or a previous incarnation of this one) left pending.
func (w *Worker) reclaimStale() {
	jobs, err := w.queue.ClaimStale(w.ctx, staleIdleDuration)
	if err != nil {
		logger.Warn("failed to reclaim stale messages", zap.Error(err))
		return
	}

	if len(jobs) == 0 {
		return
	}

	logger.Info("reclaimed stale webhook jobs", zap.Int("count", len(jobs)))
	for _, job := range jobs {
		w.processJob(job)
	}
}

// StartAsync starts the worker in a goroutine.
func (w *Worker) StartAsync() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in webhook worker", zap.Any("panic", r))
				time.Sleep(5 * time.Second)
				select {
				case <-w.ctx.Done():
					logger.Info("not restarting webhook worker after panic because context is canceled")
				default:
					logger.Info("restarting webhook worker after panic")
					w.StartAsync()
				}
			}
		}()
		w.Start()
	}()
}

// GetStats returns stream/consumer-group statistics for debugging.
func (w *Worker) GetStats(ctx context.Context) (map[string]int64, error) {
	return w.queue.GetStats(ctx)
}
