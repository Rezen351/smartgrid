package cron

import (
	"context"
	"log"

	"github.com/almuzky/iot/services/auth/internal/repository"
	"github.com/robfig/cron/v3"
)

// RetentionCron runs periodic cleanup jobs for data retention policy.
type RetentionCron struct {
	repo *repository.UserRepository
	c    *cron.Cron
}

func NewRetentionCron(repo *repository.UserRepository) *RetentionCron {
	return &RetentionCron{
		repo: repo,
		c:    cron.New(),
	}
}

// Start registers cron jobs and begins the scheduler.
// Schedules:
//   - Every day at 02:00 — delete expired refresh tokens
//   - Every Sunday at 03:00 — soft-delete inactive users (365+ days no login)
func (rc *RetentionCron) Start() {
	// Delete expired refresh tokens — daily at 02:00
	_, _ = rc.c.AddFunc("0 2 * * *", func() {
		ctx := context.Background()
		n, err := rc.repo.DeleteExpiredRefreshTokens(ctx)
		if err != nil {
			log.Printf("[retention] delete expired tokens error: %v", err)
			return
		}
		log.Printf("[retention] deleted %d expired refresh tokens", n)
	})

	// Soft-delete inactive users — every Sunday at 03:00
	_, _ = rc.c.AddFunc("0 3 * * 0", func() {
		ctx := context.Background()
		n, err := rc.repo.SoftDeleteInactiveUsers(ctx)
		if err != nil {
			log.Printf("[retention] soft-delete inactive users error: %v", err)
			return
		}
		log.Printf("[retention] soft-deleted %d inactive users", n)
	})

	rc.c.Start()
	log.Println("[retention] cron scheduler started")
}

// Stop gracefully shuts down the cron scheduler.
func (rc *RetentionCron) Stop() {
	rc.c.Stop()
	log.Println("[retention] cron scheduler stopped")
}
