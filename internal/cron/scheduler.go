package cron

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	robfigcron "github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

var (
	ErrNotFound = errors.New("cron job not found")

	defaultParser = robfigcron.NewParser(
		robfigcron.Minute |
			robfigcron.Hour |
			robfigcron.Dom |
			robfigcron.Month |
			robfigcron.Dow |
			robfigcron.Descriptor,
	)
)

type Record struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Schedule  string    `json:"schedule"`
	Command   string    `json:"command"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "validation failed"
}

type Snapshot struct {
	Enabled bool     `json:"enabled"`
	Started bool     `json:"started"`
	Jobs    []Record `json:"jobs"`
}

type Scheduler struct {
	logger  *zap.Logger
	enabled bool
	store   *Store
	parser  robfigcron.Parser

	mu      sync.RWMutex
	started bool
	cron    *robfigcron.Cron
	jobs    []Record
	entries map[string]robfigcron.EntryID
}

func NewScheduler(logger *zap.Logger, enabled bool, store *Store) *Scheduler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Scheduler{
		logger:  logger,
		enabled: enabled,
		store:   store,
		parser:  defaultParser,
		jobs:    make([]Record, 0),
		entries: make(map[string]robfigcron.EntryID),
	}
}

func (s *Scheduler) Load(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}

	records, err := s.store.List(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = append([]Record(nil), records...)
	if s.jobs == nil {
		s.jobs = make([]Record, 0)
	}
	s.entries = make(map[string]robfigcron.EntryID)

	return nil
}

func (s *Scheduler) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := append([]Record(nil), s.jobs...)
	if jobs == nil {
		jobs = make([]Record, 0)
	}

	return Snapshot{
		Enabled: s.enabled,
		Started: s.started,
		Jobs:    jobs,
	}
}

func (s *Scheduler) List() []Record {
	return s.Snapshot().Jobs
}

func (s *Scheduler) Start() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		s.logger.Info("cron scheduler disabled")
		return
	}

	if s.started {
		return
	}

	s.cron = robfigcron.New()
	s.entries = make(map[string]robfigcron.EntryID, len(s.jobs))
	for _, job := range s.jobs {
		if err := s.registerJobLocked(job); err != nil {
			s.logger.Error("register persisted cron job failed",
				zap.String("job_id", job.ID),
				zap.String("schedule", job.Schedule),
				zap.Error(err),
			)
		}
	}

	s.cron.Start()
	s.started = true
	s.logger.Info("cron scheduler started", zap.Int("job_count", len(s.entries)))
}

func (s *Scheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	if !s.started || s.cron == nil {
		s.mu.Unlock()
		return nil
	}

	stopCtx := s.cron.Stop()
	s.started = false
	s.cron = nil
	s.entries = make(map[string]robfigcron.EntryID)
	s.mu.Unlock()

	select {
	case <-stopCtx.Done():
		s.logger.Info("cron scheduler stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) Create(ctx context.Context, input CreateInput) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("cron scheduler is not configured")
	}

	name, schedule, command, err := s.validateCreateInput(input)
	if err != nil {
		return Record{}, err
	}

	record := Record{
		ID:        fmt.Sprintf("cron-%d", time.Now().UnixNano()),
		Name:      name,
		Schedule:  schedule,
		Command:   command,
		CreatedAt: time.Now().UTC(),
	}

	if s.store != nil {
		if err := s.store.Insert(ctx, record); err != nil {
			return Record{}, err
		}
	}

	s.mu.Lock()
	s.jobs = append([]Record{record}, s.jobs...)

	var registerErr error
	if s.started && s.cron != nil {
		registerErr = s.registerJobLocked(record)
	}
	s.mu.Unlock()

	if registerErr != nil {
		if s.store != nil {
			if err := s.store.Delete(ctx, record.ID); err != nil {
				s.logger.Error("rollback cron job persistence failed",
					zap.String("job_id", record.ID),
					zap.Error(err),
				)
			}
		}

		s.mu.Lock()
		s.removeRecordLocked(record.ID)
		delete(s.entries, record.ID)
		s.mu.Unlock()

		return Record{}, fmt.Errorf("register cron job: %w", registerErr)
	}

	return record, nil
}

func (s *Scheduler) Delete(ctx context.Context, id string) (Record, bool, error) {
	if s == nil {
		return Record{}, false, fmt.Errorf("cron scheduler is not configured")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, record, ok := s.findJobLocked(id)
	if !ok {
		return Record{}, false, nil
	}

	if s.store != nil {
		if err := s.store.Delete(ctx, id); err != nil {
			return Record{}, false, err
		}
	}

	if entryID, exists := s.entries[id]; exists && s.cron != nil {
		s.cron.Remove(entryID)
		delete(s.entries, id)
	}

	s.jobs = append(s.jobs[:index], s.jobs[index+1:]...)

	return record, true, nil
}

func (s *Scheduler) validateCreateInput(input CreateInput) (string, string, string, error) {
	name := strings.TrimSpace(input.Name)
	schedule := strings.TrimSpace(input.Schedule)
	command := strings.TrimSpace(input.Command)

	validation := ValidationErrors{}
	if name == "" {
		validation["name"] = "Name is required."
	} else if len(name) > 120 {
		validation["name"] = "Name must be 120 characters or less."
	}

	if schedule == "" {
		validation["schedule"] = "Schedule is required."
	} else if _, err := s.parser.Parse(schedule); err != nil {
		validation["schedule"] = "Enter a valid 5-field cron expression or descriptor like @daily."
	}

	if command == "" {
		validation["command"] = "Command is required."
	}

	if len(validation) > 0 {
		return "", "", "", validation
	}

	return name, schedule, command, nil
}

func (s *Scheduler) registerJobLocked(job Record) error {
	if s.cron == nil {
		return nil
	}

	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.executeJob(job)
	})
	if err != nil {
		return err
	}

	s.entries[job.ID] = entryID
	return nil
}

func (s *Scheduler) executeJob(job Record) {
	startedAt := time.Now()
	s.logger.Info("running cron job",
		zap.String("job_id", job.ID),
		zap.String("name", job.Name),
		zap.String("schedule", job.Schedule),
	)

	commandName, commandArgs := shellCommand(job.Command)
	cmd := exec.Command(commandName, commandArgs...)
	output, err := cmd.CombinedOutput()
	duration := time.Since(startedAt)
	trimmedOutput := strings.TrimSpace(string(output))

	if err != nil {
		fields := []zap.Field{
			zap.String("job_id", job.ID),
			zap.String("name", job.Name),
			zap.Duration("duration", duration),
			zap.Error(err),
		}
		if trimmedOutput != "" {
			fields = append(fields, zap.String("output", truncate(trimmedOutput, 2000)))
		}
		s.logger.Error("cron job failed", fields...)
		return
	}

	fields := []zap.Field{
		zap.String("job_id", job.ID),
		zap.String("name", job.Name),
		zap.Duration("duration", duration),
	}
	if trimmedOutput != "" {
		fields = append(fields, zap.String("output", truncate(trimmedOutput, 2000)))
	}
	s.logger.Info("cron job finished", fields...)
}

func (s *Scheduler) findJobLocked(id string) (int, Record, bool) {
	for index, record := range s.jobs {
		if record.ID == id {
			return index, record, true
		}
	}

	return -1, Record{}, false
}

func (s *Scheduler) removeRecordLocked(id string) {
	index, _, ok := s.findJobLocked(id)
	if !ok {
		return
	}

	s.jobs = append(s.jobs[:index], s.jobs[index+1:]...)
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/C", command}
	}

	return "/bin/sh", []string{"-lc", command}
}

func truncate(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}

	return value[:limit] + "..."
}
