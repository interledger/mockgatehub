package webhook

import (
	"context"
	"fmt"
	"time"

	"mockgatehub/internal/logger"
	"mockgatehub/internal/utils"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	streamKey    = "webhooks:stream"
	groupName    = "webhooks:workers"
	maxAttempts  = 10              // Give up after 10 failed attempts
	retryDelay   = 3 * time.Second // Fixed 3-second backoff between retries
	blockTimeout = 5 * time.Second // Max time to block on XREADGROUP before rechecking ctx
	batchSize    = 10              // Max jobs to fetch in one read
	maxStreamLen = int64(10000)    // Approximate cap for XTRIM
)

// Queue manages webhook jobs using a Redis Stream with consumer groups.
// Workers block on XREADGROUP instead of polling, so jobs are picked up
// immediately when they arrive in the stream.
type Queue struct {
	client       *redis.Client
	minDelay     time.Duration
	consumerName string
}

// NewQueue creates a new stream-backed webhook queue and ensures the
// consumer group exists (creating the stream if necessary).
func NewQueue(client *redis.Client, minDelaySec float64) *Queue {
	if minDelaySec < 0 {
		minDelaySec = 0
	}

	q := &Queue{
		client:       client,
		minDelay:     time.Duration(minDelaySec * float64(time.Second)),
		consumerName: "worker-" + utils.GenerateUUID(),
	}

	ctx := context.Background()
	err := client.XGroupCreateMkStream(ctx, streamKey, groupName, "0").Err()
	if err != nil {
		// "BUSYGROUP" means the group already exists — that's fine.
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			logger.Error("failed to create consumer group", zap.Error(err))
		}
	}

	return q
}

// Enqueue adds a new webhook job. If minDelay + offsetDelaySeconds > 0 the
// message is added to the stream after that delay (via a goroutine timer);
// otherwise it is added immediately.
func (q *Queue) Enqueue(ctx context.Context, eventType, userID string, data any, offsetDelaySeconds float64) (string, error) {
	jobID := utils.GenerateUUID()
	dataMap := coerceToMap(data)

	job := &StreamJob{
		JobID:     jobID,
		EventType: eventType,
		UserID:    userID,
		Data:      dataMap,
		Attempts:  0,
		CreatedAt: time.Now().Format(time.RFC3339Nano),
	}

	fields, err := job.toStreamFields()
	if err != nil {
		return "", err
	}

	offset := time.Duration(offsetDelaySeconds * float64(time.Second))
	totalDelay := q.minDelay + offset

	if totalDelay > 0 {
		// Fire-and-forget: add to stream after the delay elapses.
		go func() {
			timer := time.NewTimer(totalDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
				if err := q.addToStream(context.Background(), fields); err != nil {
					logger.Error("failed to add delayed job to stream",
						zap.String("job_id", jobID), zap.Error(err))
				}
			case <-ctx.Done():
				logger.Warn("delayed job canceled before delivery",
					zap.String("job_id", jobID))
			}
		}()
	} else {
		if err := q.addToStream(ctx, fields); err != nil {
			return "", err
		}
	}

	logger.Info("enqueued webhook job",
		zap.String("job_id", jobID),
		zap.String("event", eventType),
		zap.String("user", userID),
		zap.Duration("delay", totalDelay),
	)
	return jobID, nil
}

// addToStream performs XADD and trims the stream to keep it bounded.
func (q *Queue) addToStream(ctx context.Context, fields map[string]interface{}) error {
	_, err := q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: fields,
	}).Result()
	if err != nil {
		return fmt.Errorf("XADD failed: %w", err)
	}

	// Best-effort trim to prevent unbounded growth.
	q.client.XTrimMaxLenApprox(ctx, streamKey, maxStreamLen, 0)
	return nil
}

// ReadJobs blocks until new messages arrive or blockTimeout elapses.
// It returns parsed jobs together with their stream message IDs (needed
// for Ack / RetryLater).
func (q *Queue) ReadJobs(ctx context.Context, count int64) ([]StreamJob, error) {
	results, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    groupName,
		Consumer: q.consumerName,
		Streams:  []string{streamKey, ">"},
		Count:    count,
		Block:    blockTimeout,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Timeout — no new messages
		}
		return nil, fmt.Errorf("XREADGROUP failed: %w", err)
	}

	var jobs []StreamJob
	for _, stream := range results {
		for _, msg := range stream.Messages {
			job, parseErr := parseStreamMessage(msg)
			if parseErr != nil {
				logger.Error("corrupt stream message — acking to skip",
					zap.String("msg_id", msg.ID), zap.Error(parseErr))
				q.client.XAck(ctx, streamKey, groupName, msg.ID)
				continue
			}
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

// Ack acknowledges successful processing of a stream message.
func (q *Queue) Ack(ctx context.Context, messageID string) error {
	return q.client.XAck(ctx, streamKey, groupName, messageID).Err()
}

// RetryLater acknowledges the current message and re-enqueues the job
// with an incremented attempt counter. If max attempts are exhausted the
// job is dropped (logged at Error level).
func (q *Queue) RetryLater(ctx context.Context, job StreamJob, errMsg string) error {
	// ACK current delivery so it doesn't get re-delivered on restart.
	if err := q.Ack(ctx, job.MessageID); err != nil {
		return fmt.Errorf("failed to ack message for retry: %w", err)
	}

	newAttempts := job.Attempts + 1

	if newAttempts >= maxAttempts {
		logger.Error("job permanently failed — dropping after max attempts",
			zap.String("job_id", job.JobID),
			zap.Int("attempts", newAttempts),
			zap.String("error", errMsg),
		)
		return nil
	}

	// Build a new entry with updated metadata.
	retry := &StreamJob{
		JobID:     job.JobID,
		EventType: job.EventType,
		UserID:    job.UserID,
		Data:      job.Data,
		Attempts:  newAttempts,
		CreatedAt: job.CreatedAt,
		LastError: errMsg,
	}

	fields, err := retry.toStreamFields()
	if err != nil {
		return fmt.Errorf("failed to serialize retry: %w", err)
	}

	// Re-add to stream after retryDelay.
	go func() {
		timer := time.NewTimer(retryDelay)
		defer timer.Stop()
		<-timer.C

		if err := q.addToStream(context.Background(), fields); err != nil {
			logger.Error("failed to re-enqueue job for retry",
				zap.String("job_id", job.JobID),
				zap.Int("attempt", newAttempts),
				zap.Error(err),
			)
		} else {
			logger.Warn("rescheduled job for retry",
				zap.String("job_id", job.JobID),
				zap.Int("attempt", newAttempts),
				zap.Int("max_attempts", maxAttempts),
			)
		}
	}()

	return nil
}

// ClaimStale reclaims messages that another consumer read but never
// acknowledged (e.g. after a crash). Should be called once at startup.
func (q *Queue) ClaimStale(ctx context.Context, idleDuration time.Duration) ([]StreamJob, error) {
	messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   streamKey,
		Group:    groupName,
		Consumer: q.consumerName,
		MinIdle:  idleDuration,
		Start:    "0-0",
		Count:    int64(batchSize),
	}).Result()
	if err != nil {
		return nil, fmt.Errorf("XAUTOCLAIM failed: %w", err)
	}

	var jobs []StreamJob
	for _, msg := range messages {
		job, parseErr := parseStreamMessage(msg)
		if parseErr != nil {
			logger.Error("corrupt stale message — acking to skip",
				zap.String("msg_id", msg.ID), zap.Error(parseErr))
			q.client.XAck(ctx, streamKey, groupName, msg.ID)
			continue
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// GetStats returns stream/consumer-group statistics for debugging.
func (q *Queue) GetStats(ctx context.Context) (map[string]int64, error) {
	streamLen, err := q.client.XLen(ctx, streamKey).Result()
	if err != nil {
		return nil, fmt.Errorf("XLEN failed: %w", err)
	}

	pending, err := q.client.XPending(ctx, streamKey, groupName).Result()
	if err != nil {
		// Group may not exist yet — not fatal.
		return map[string]int64{
			"stream_length": streamLen,
			"pending":       0,
		}, nil
	}

	return map[string]int64{
		"stream_length": streamLen,
		"pending":       pending.Count,
	}, nil
}

// GetClient exposes the underlying Redis client (used by GetStats in worker).
func (q *Queue) GetClient() *redis.Client {
	return q.client
}

// StreamLength returns the current XLEN (for tests).
func (q *Queue) StreamLength(ctx context.Context) (int64, error) {
	return q.client.XLen(ctx, streamKey).Result()
}

// ConsumerName returns this queue instance's consumer name.
func (q *Queue) ConsumerName() string {
	return q.consumerName
}

// PendingCount returns the number of messages delivered but not yet ACK'd.
func (q *Queue) PendingCount(ctx context.Context) (int64, error) {
	pending, err := q.client.XPending(ctx, streamKey, groupName).Result()
	if err != nil {
		return 0, err
	}
	return pending.Count, nil
}

// FlushStream deletes the stream entirely (for tests).
func (q *Queue) FlushStream(ctx context.Context) error {
	return q.client.Del(ctx, streamKey).Err()
}

// MaxAttempts returns the configured maximum delivery attempts.
func MaxAttempts() int {
	return maxAttempts
}

// StreamKey returns the Redis key used for the webhook stream.
func StreamKey() string {
	return streamKey
}

// GroupName returns the Redis consumer group name.
func GroupName() string {
	return groupName
}

// RetryDelayDuration returns the configured retry delay.
func RetryDelayDuration() time.Duration {
	return retryDelay
}

// BatchSize returns the configured batch size per read.
func BatchSize() int64 {
	return int64(batchSize)
}

// BlockTimeoutDuration returns how long XREADGROUP blocks before returning.
func BlockTimeoutDuration() time.Duration {
	return blockTimeout
}
