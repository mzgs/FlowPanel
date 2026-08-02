package systemmonitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"flowpanel/internal/alerts"
	"flowpanel/internal/systemstatus"

	"go.uber.org/zap"
)

const (
	sampleInterval = 1 * time.Minute
	sampleTimeout  = 15 * time.Second
	pruneInterval  = 30 * time.Minute
	retention      = 7 * 24 * time.Hour
)

type Service struct {
	logger       *zap.Logger
	store        *Store
	alerts       *alerts.Service
	diskBreaches int

	mu          sync.Mutex
	cancel      context.CancelFunc
	done        chan struct{}
	lastPruneAt time.Time
	started     bool
}

func NewService(logger *zap.Logger, store *Store, alertService *alerts.Service) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
		store:  store,
		alerts: alertService,
	}
}

func (s *Service) Start() {
	if s == nil || s.store == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	s.cancel = cancel
	s.done = done
	s.started = true

	go s.run(ctx, done)
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}

	cancel := s.cancel
	done := s.done
	s.cancel = nil
	s.done = nil
	s.started = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) ListRange(ctx context.Context, duration time.Duration) ([]Sample, error) {
	if s == nil || s.store == nil {
		return []Sample{}, nil
	}

	return s.store.ListSince(ctx, time.Now().Add(-duration))
}

func (s *Service) run(ctx context.Context, done chan struct{}) {
	defer close(done)

	s.capture()

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.capture()
		}
	}
}

func (s *Service) capture() {
	if s == nil || s.store == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
	defer cancel()

	now := time.Now().UTC().Truncate(time.Minute)
	status := systemstatus.Inspect(ctx)
	if err := s.store.Insert(ctx, NewSample(now, status)); err != nil {
		s.logger.Error("capture system monitor sample failed", zap.Error(err))
		return
	}
	s.evaluateDisk(ctx, status)

	s.pruneIfNeeded(now)
}

func (s *Service) evaluateDisk(ctx context.Context, status systemstatus.Status) {
	const key = "disk:root:pressure"
	if s.alerts == nil || status.DiskTotalBytes == nil || status.DiskUsedBytes == nil || *status.DiskTotalBytes == 0 {
		return
	}
	config, err := s.alerts.Config(ctx)
	if err != nil || !config.Enabled {
		return
	}
	usedPercent := float64(*status.DiskUsedBytes) / float64(*status.DiskTotalBytes) * 100
	if usedPercent < float64(config.DiskWarningPercent) {
		s.diskBreaches = 0
		if err := s.alerts.Resolve(ctx, key); err != nil {
			s.logger.Error("resolve disk pressure alert failed", zap.Error(err))
		}
		return
	}
	s.diskBreaches++
	if s.diskBreaches < 3 {
		return
	}
	severity, threshold := "warning", config.DiskWarningPercent
	if usedPercent >= float64(config.DiskCriticalPercent) {
		severity, threshold = "critical", config.DiskCriticalPercent
	}
	if err := s.alerts.Trigger(ctx, alerts.TriggerInput{
		Key: key, Severity: severity, Title: "Disk usage is high",
		Message: fmt.Sprintf("Root disk usage is %.1f%%, above the %d%% threshold.", usedPercent, threshold),
	}); err != nil {
		s.logger.Error("trigger disk pressure alert failed", zap.Error(err))
	}
}

func (s *Service) pruneIfNeeded(now time.Time) {
	s.mu.Lock()
	if !s.lastPruneAt.IsZero() && now.Sub(s.lastPruneAt) < pruneInterval {
		s.mu.Unlock()
		return
	}
	s.lastPruneAt = now
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
	defer cancel()

	if err := s.store.DeleteBefore(ctx, now.Add(-retention)); err != nil {
		s.logger.Error("prune system monitor samples failed", zap.Error(err))
	}
}
