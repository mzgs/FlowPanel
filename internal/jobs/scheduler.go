package jobs

import (
	"context"
	"sync"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

type Scheduler struct {
	logger  *zap.Logger
	enabled bool
	cron    *cron.Cron

	mu      sync.Mutex
	started bool
}

func NewScheduler(logger *zap.Logger, enabled bool) *Scheduler {
	return &Scheduler{
		logger:  logger,
		enabled: enabled,
		cron:    cron.New(),
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		s.logger.Info("cron scheduler disabled")
		return
	}

	if s.started {
		return
	}

	s.cron.Start()
	s.started = true
	s.logger.Info("cron scheduler started")
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}

	stopCtx := s.cron.Stop()
	s.started = false
	s.mu.Unlock()

	select {
	case <-stopCtx.Done():
		s.logger.Info("cron scheduler stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
