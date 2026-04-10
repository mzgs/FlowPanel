package events

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultListLimit = 100
	maxListLimit     = 250
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
