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

const (
	maxExecutionLogsPerJob = 10
	maxExecutionOutputSize = 8000
)

type Record struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Schedule   string         `json:"schedule"`
	Command    string         `json:"command"`
	CreatedAt  time.Time      `json:"created_at"`
	Executions []ExecutionLog `json:"executions"`
}

type ExecutionLog struct {
	ID         string    `json:"id"`
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	DurationMS int64     `json:"duration_ms"`
	Output     string    `json:"output"`
	Error      string    `json:"error"`
}

type CreateInput struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type UpdateInput = CreateInput

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

	executionLogs, err := s.store.ListExecutionLogs(ctx, maxExecutionLogsPerJob)
	if err != nil {
		return err
	}

	for index := range records {
		records[index].Executions = cloneExecutionLogs(executionLogs[records[index].ID])
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = cloneRecords(records)
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

	jobs := cloneRecords(s.jobs)
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

	name, schedule, command, err := s.validateInput(input.Name, input.Schedule, input.Command)
	if err != nil {
		return Record{}, err
	}

	record := Record{
		ID:         fmt.Sprintf("cron-%d", time.Now().UnixNano()),
		Name:       name,
		Schedule:   schedule,
		Command:    command,
		CreatedAt:  time.Now().UTC(),
		Executions: make([]ExecutionLog, 0),
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

	return cloneRecord(record), nil
}

func (s *Scheduler) Update(ctx context.Context, id string, input UpdateInput) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("cron scheduler is not configured")
	}

	name, schedule, command, err := s.validateInput(input.Name, input.Schedule, input.Command)
	if err != nil {
		return Record{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, current, ok := s.findJobLocked(id)
	if !ok {
		return Record{}, ErrNotFound
	}

	updated := current
	updated.Name = name
	updated.Schedule = schedule
	updated.Command = command

	if s.store != nil {
		if err := s.store.Update(ctx, updated); err != nil {
			return Record{}, err
		}
	}

	var (
		newEntryID robfigcron.EntryID
		oldEntryID robfigcron.EntryID
		hadEntry   bool
	)
	if s.started && s.cron != nil {
		newEntryID, err = s.addEntryLocked(updated)
		if err != nil {
			if s.store != nil {
				if rollbackErr := s.store.Update(ctx, current); rollbackErr != nil {
					s.logger.Error("rollback cron job update failed",
						zap.String("job_id", id),
						zap.Error(rollbackErr),
					)
				}
			}

			return Record{}, fmt.Errorf("register cron job: %w", err)
		}

		oldEntryID, hadEntry = s.entries[id]
	}

	s.jobs[index] = updated
	if s.started && s.cron != nil {
		if hadEntry {
			s.cron.Remove(oldEntryID)
		}
		s.entries[id] = newEntryID
	}

	return cloneRecord(updated), nil
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

func (s *Scheduler) RunNow(id string) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("cron scheduler is not configured")
	}

	s.mu.RLock()
	_, record, ok := s.findJobLocked(id)
	s.mu.RUnlock()
	if !ok {
		return Record{}, ErrNotFound
	}

	go s.executeJob(record)

	return record, nil
}

func (s *Scheduler) validateInput(nameValue string, scheduleValue string, commandValue string) (string, string, string, error) {
	name := strings.TrimSpace(nameValue)
	schedule := strings.TrimSpace(scheduleValue)
	command := strings.TrimSpace(commandValue)

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
	entryID, err := s.addEntryLocked(job)
	if err != nil {
		return err
	}

	if s.cron != nil {
		s.entries[job.ID] = entryID
	}

	return nil
}

func (s *Scheduler) addEntryLocked(job Record) (robfigcron.EntryID, error) {
	if s.cron == nil {
		return 0, nil
	}

	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.executeJob(job)
	})
	if err != nil {
		return 0, err
	}

	return entryID, nil
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
	finishedAt := time.Now()
	duration := time.Since(startedAt)
	trimmedOutput := strings.TrimSpace(string(output))
	execution := ExecutionLog{
		ID:         fmt.Sprintf("cron-run-%d", finishedAt.UnixNano()),
		JobID:      job.ID,
		StartedAt:  startedAt.UTC(),
		FinishedAt: finishedAt.UTC(),
		DurationMS: duration.Milliseconds(),
		Output:     truncate(trimmedOutput, maxExecutionOutputSize),
	}

	if err != nil {
		execution.Status = "failed"
		execution.Error = err.Error()
	} else {
		execution.Status = "success"
	}

	if s.store != nil {
		if storeErr := s.store.InsertExecutionLog(context.Background(), execution); storeErr != nil {
			s.logger.Error("persist cron execution log failed",
				zap.String("job_id", job.ID),
				zap.Error(storeErr),
			)
		}
	}

	s.mu.Lock()
	s.prependExecutionLocked(job.ID, execution)
	s.mu.Unlock()

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

func (s *Scheduler) prependExecutionLocked(id string, execution ExecutionLog) {
	index, record, ok := s.findJobLocked(id)
	if !ok {
		return
	}

	record.Executions = append([]ExecutionLog{execution}, record.Executions...)
	if len(record.Executions) > maxExecutionLogsPerJob {
		record.Executions = record.Executions[:maxExecutionLogsPerJob]
	}

	s.jobs[index] = record
}

func cloneRecords(records []Record) []Record {
	if len(records) == 0 {
		return []Record{}
	}

	cloned := make([]Record, len(records))
	for index, record := range records {
		cloned[index] = cloneRecord(record)
	}

	return cloned
}

func cloneRecord(record Record) Record {
	record.Executions = cloneExecutionLogs(record.Executions)
	return record
}

func cloneExecutionLogs(executions []ExecutionLog) []ExecutionLog {
	if len(executions) == 0 {
		return []ExecutionLog{}
	}

	return append([]ExecutionLog(nil), executions...)
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
