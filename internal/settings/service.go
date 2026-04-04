package settings

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const (
	defaultPanelName     = "FlowPanel"
	maxPanelNameLength   = 80
	maxGitHubTokenLength = 4096
)

type Record struct {
	PanelName   string `json:"panel_name"`
	GitHubToken string `json:"github_token"`
}

type UpdateInput struct {
	PanelName   string `json:"panel_name"`
	GitHubToken string `json:"github_token"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "settings validation failed"
}

type Service struct {
	store *Store
}

func NewService(store *Store) *Service {
	return &Service{store: store}
}

func (s *Service) Get(ctx context.Context) (Record, error) {
	if s == nil || s.store == nil {
		return defaultRecord(), nil
	}

	record, err := s.store.Get(ctx)
	if err == nil {
		return record, nil
	}
	if err == sql.ErrNoRows {
		return defaultRecord(), nil
	}

	return Record{}, err
}

func (s *Service) Update(ctx context.Context, input UpdateInput) (Record, error) {
	validation := validateUpdateInput(input)
	if len(validation) > 0 {
		return Record{}, validation
	}

	record := Record{
		PanelName:   normalizeSingleLine(input.PanelName, defaultPanelName, maxPanelNameLength),
		GitHubToken: normalizeOptionalSingleLine(input.GitHubToken, maxGitHubTokenLength),
	}

	if s == nil || s.store == nil {
		return record, nil
	}

	if err := s.store.Upsert(ctx, record); err != nil {
		return Record{}, err
	}

	return record, nil
}

func defaultRecord() Record {
	return Record{
		PanelName:   defaultPanelName,
		GitHubToken: "",
	}
}

func validateUpdateInput(input UpdateInput) ValidationErrors {
	validation := ValidationErrors{}

	panelName := strings.TrimSpace(input.PanelName)
	if panelName == "" {
		validation["panel_name"] = "Panel name is required."
	} else if len(panelName) > maxPanelNameLength {
		validation["panel_name"] = fmt.Sprintf("Panel name must be %d characters or fewer.", maxPanelNameLength)
	}

	if len(strings.TrimSpace(input.GitHubToken)) > maxGitHubTokenLength {
		validation["github_token"] = fmt.Sprintf("GitHub token must be %d characters or fewer.", maxGitHubTokenLength)
	}

	return validation
}

func normalizeSingleLine(value, fallback string, maxLen int) string {
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

func normalizeOptionalSingleLine(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen > 0 && len(value) > maxLen {
		value = strings.TrimSpace(value[:maxLen])
	}

	return value
}
