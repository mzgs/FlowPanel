package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultListLimit         = 100
	maxListLimit             = 250
	eventRetentionCheck      = 24 * time.Hour
	securityMaxEventsPerHost = 10_000
	activityMaxEvents        = 10_000
)

type Record struct {
	ID            string    `json:"id"`
	Actor         string    `json:"actor"`
	Category      string    `json:"category"`
	Action        string    `json:"action"`
	ResourceType  string    `json:"resource_type"`
	ResourceID    string    `json:"resource_id"`
	ResourceLabel string    `json:"resource_label"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	CreatedAt     time.Time `json:"created_at"`
}

type CreateInput struct {
	Actor         string
	Category      string
	Action        string
	ResourceType  string
	ResourceID    string
	ResourceLabel string
	Status        string
	Message       string
}

type SecurityInput struct {
	Action        string
	Hostname      string
	URI           string
	ClientIP      string
	TransactionID string
	ExpiresAt     time.Time
}

type SecurityRecord struct {
	ID            string     `json:"id"`
	Action        string     `json:"action"`
	Hostname      string     `json:"hostname"`
	URI           string     `json:"uri"`
	ClientIP      string     `json:"client_ip"`
	TransactionID string     `json:"transaction_id"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type securityDetails struct {
	URI           string `json:"uri"`
	ClientIP      string `json:"client_ip"`
	TransactionID string `json:"transaction_id"`
	ExpiresAt     string `json:"expires_at,omitempty"`
}

type Service struct {
	logger *zap.Logger
	store  *Store
}

func NewService(logger *zap.Logger, store *Store) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Service{
		logger: logger,
		store:  store,
	}
}

func (s *Service) Record(ctx context.Context, input CreateInput) (Record, error) {
	if s == nil {
		return Record{}, nil
	}

	record := Record{
		ID:            fmt.Sprintf("evt-%d", time.Now().UnixNano()),
		Actor:         normalizeValue(input.Actor, "system", 40),
		Category:      normalizeValue(input.Category, "system", 40),
		Action:        normalizeValue(input.Action, "updated", 40),
		ResourceType:  normalizeValue(input.ResourceType, "system", 40),
		ResourceID:    normalizeValue(input.ResourceID, "-", 160),
		ResourceLabel: normalizeValue(input.ResourceLabel, normalizeValue(input.ResourceID, "-", 160), 160),
		Status:        normalizeValue(input.Status, "succeeded", 20),
		Message:       normalizeValue(input.Message, "No details recorded.", 0),
		CreatedAt:     time.Now().UTC(),
	}

	if err := s.store.Insert(ctx, record); err != nil {
		s.logger.Error("record event failed",
			zap.String("category", record.Category),
			zap.String("action", record.Action),
			zap.String("resource_type", record.ResourceType),
			zap.String("resource_id", record.ResourceID),
			zap.Error(err),
		)
		return Record{}, err
	}

	return record, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]Record, error) {
	if s == nil {
		return []Record{}, nil
	}

	switch {
	case limit <= 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		limit = maxListLimit
	}

	return s.store.List(ctx, limit)
}

func (s *Service) StartRetention(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		s.pruneEvents(ctx)
		ticker := time.NewTicker(eventRetentionCheck)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pruneEvents(ctx)
			}
		}
	}()
}

func (s *Service) pruneEvents(ctx context.Context) {
	removed, err := s.store.Prune(ctx, securityMaxEventsPerHost, activityMaxEvents)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.Error("prune events failed", zap.Error(err))
		}
		return
	}
	if removed > 0 {
		s.logger.Info("pruned events", zap.Int64("removed", removed), zap.Int("security_max_per_domain", securityMaxEventsPerHost), zap.Int("activity_max", activityMaxEvents))
	}
}

func (s *Service) RecordSecurity(ctx context.Context, input SecurityInput) (SecurityRecord, error) {
	details := securityDetails{
		URI:           normalizeValue(input.URI, "-", 2000),
		ClientIP:      normalizeValue(input.ClientIP, "unknown", 160),
		TransactionID: normalizeValue(input.TransactionID, "-", 160),
	}
	if !input.ExpiresAt.IsZero() {
		details.ExpiresAt = input.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	message, err := json.Marshal(details)
	if err != nil {
		return SecurityRecord{}, fmt.Errorf("encode security event: %w", err)
	}

	record, err := s.Record(ctx, CreateInput{
		Actor:         "waf",
		Category:      "security",
		Action:        normalizeValue(input.Action, "waf_blocked", 40),
		ResourceType:  "domain",
		ResourceID:    input.Hostname,
		ResourceLabel: input.Hostname,
		Status:        "blocked",
		Message:       string(message),
	})
	if err != nil {
		return SecurityRecord{}, err
	}

	return securityRecord(record, details), nil
}

func (s *Service) ListSecurity(ctx context.Context, hostname string, limit int) ([]SecurityRecord, error) {
	if s == nil {
		return []SecurityRecord{}, nil
	}

	switch {
	case limit <= 0:
		limit = defaultListLimit
	case limit > maxListLimit:
		limit = maxListLimit
	}

	records, err := s.store.ListSecurity(ctx, strings.TrimSpace(hostname), limit)
	if err != nil {
		return nil, err
	}

	result := make([]SecurityRecord, 0, len(records))
	for _, record := range records {
		var details securityDetails
		if json.Unmarshal([]byte(record.Message), &details) == nil {
			result = append(result, securityRecord(record, details))
		}
	}
	return result, nil
}

func securityRecord(record Record, details securityDetails) SecurityRecord {
	var expiresAt *time.Time
	if parsed, err := time.Parse(time.RFC3339Nano, details.ExpiresAt); err == nil {
		expiresAt = &parsed
	}
	return SecurityRecord{
		ID:            record.ID,
		Action:        record.Action,
		Hostname:      record.ResourceID,
		URI:           details.URI,
		ClientIP:      details.ClientIP,
		TransactionID: details.TransactionID,
		ExpiresAt:     expiresAt,
		CreatedAt:     record.CreatedAt,
	}
}

func normalizeValue(value, fallback string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if maxLen > 0 && len(value) > maxLen {
		value = strings.TrimSpace(value[:maxLen])
	}
	if value == "" {
		return fallback
	}

	return value
}
