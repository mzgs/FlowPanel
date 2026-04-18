package ftp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"flowpanel/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultPort         = 2121
	defaultHost         = "0.0.0.0"
	defaultPassivePorts = "30000-30100"
	maxUsernameLength   = 64
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type DomainStatus struct {
	Supported   bool   `json:"supported"`
	Enabled     bool   `json:"enabled"`
	Username    string `json:"username"`
	RootPath    string `json:"root_path"`
	HasPassword bool   `json:"has_password"`
}

type AccountRecord struct {
	ID          string    `json:"id"`
	DomainID    string    `json:"domain_id"`
	DomainName  string    `json:"domain_name,omitempty"`
	Username    string    `json:"username"`
	RootPath    string    `json:"root_path"`
	Enabled     bool      `json:"enabled"`
	HasPassword bool      `json:"has_password"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateAccountInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	RootPath string `json:"root_path"`
	DomainID string `json:"domain_id"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type UpdateAccountInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	RootPath string `json:"root_path"`
	DomainID string `json:"domain_id"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

type UpdateInput struct {
	Username string `json:"username"`
	Enabled  bool   `json:"enabled"`
	Password string `json:"password"`
}

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "ftp validation failed"
}

type Service struct {
	store   *Store
	domains *domain.Service
	now     func() time.Time
}

func NewService(store *Store, domains *domain.Service) *Service {
	return &Service{
		store:   store,
		domains: domains,
		now:     time.Now,
	}
}

func DefaultHost() string {
	return defaultHost
}

func DefaultPort() int {
	return defaultPort
}

func DefaultPassivePorts() string {
	return defaultPassivePorts
}

func IsSupportedKind(kind domain.Kind) bool {
	return domain.SupportsManagedDocumentRoot(kind)
}

func NormalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (s *Service) ListAccounts(ctx context.Context) ([]AccountRecord, error) {
	if s == nil || s.store == nil {
		return nil, nil
	}

	accounts, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}

	records := make([]AccountRecord, 0, len(accounts))
	for _, account := range accounts {
		records = append(records, s.accountRecord(account))
	}

	return records, nil
}

func (s *Service) CreateAccount(ctx context.Context, input CreateAccountInput) (AccountRecord, error) {
	if s == nil || s.store == nil {
		return AccountRecord{}, errors.New("ftp accounts are not configured")
	}

	now := s.now().UTC()
	defaultEnabled := true
	if input.Enabled != nil {
		defaultEnabled = *input.Enabled
	}
	account := Account{
		ID:        fmt.Sprintf("ftp-%d", now.UnixNano()),
		Enabled:   defaultEnabled,
		CreatedAt: now,
		UpdatedAt: now,
	}

	account, validation, err := s.applyManagedAccountInput(account, input.Username, input.Password, input.RootPath, input.DomainID, input.Enabled, true)
	if err != nil {
		return AccountRecord{}, err
	}
	if len(validation) > 0 {
		return AccountRecord{}, validation
	}

	if err := s.store.Upsert(ctx, account); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return AccountRecord{}, ValidationErrors{
				"username": "This FTP username is already in use.",
			}
		}
		return AccountRecord{}, err
	}

	return s.accountRecord(account), nil
}

func (s *Service) UpdateAccount(ctx context.Context, accountID string, input UpdateAccountInput) (AccountRecord, error) {
	if s == nil || s.store == nil {
		return AccountRecord{}, errors.New("ftp accounts are not configured")
	}

	account, err := s.store.GetByID(ctx, accountID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AccountRecord{}, sql.ErrNoRows
		}
		return AccountRecord{}, err
	}

	account, validation, err := s.applyManagedAccountInput(account, input.Username, input.Password, input.RootPath, input.DomainID, input.Enabled, false)
	if err != nil {
		return AccountRecord{}, err
	}
	if len(validation) > 0 {
		return AccountRecord{}, validation
	}

	if err := s.store.Upsert(ctx, account); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return AccountRecord{}, ValidationErrors{
				"username": "This FTP username is already in use.",
			}
		}
		return AccountRecord{}, err
	}

	return s.accountRecord(account), nil
}

func (s *Service) DeleteAccount(ctx context.Context, accountID string) error {
	if s == nil || s.store == nil {
		return nil
	}

	return s.store.DeleteByID(ctx, accountID)
}

func (s *Service) GetDomainStatus(ctx context.Context, domainID string) (DomainStatus, error) {
	record, ok := s.findDomain(domainID)
	if !ok {
		return DomainStatus{}, domain.ErrNotFound
	}

	return s.domainStatus(ctx, record)
}

func (s *Service) ReconcileDomain(ctx context.Context, record domain.Record) error {
	if s == nil || s.store == nil {
		return nil
	}

	if !IsSupportedKind(record.Kind) {
		return s.store.DeleteByDomainID(ctx, record.ID)
	}

	_, err := s.ensurePrimaryDomainAccount(ctx, record)
	return err
}

func (s *Service) DeleteDomain(ctx context.Context, domainID string) error {
	if s == nil || s.store == nil {
		return nil
	}

	return s.store.DeleteByDomainID(ctx, domainID)
}

func (s *Service) UpdateDomain(ctx context.Context, domainID string, input UpdateInput) (DomainStatus, error) {
	record, ok := s.findDomain(domainID)
	if !ok {
		return DomainStatus{}, domain.ErrNotFound
	}
	if !IsSupportedKind(record.Kind) {
		return DomainStatus{}, ValidationErrors{
			"domain": "FTP is not available for this domain.",
		}
	}

	account, err := s.ensurePrimaryDomainAccount(ctx, record)
	if err != nil {
		return DomainStatus{}, err
	}

	username := NormalizeUsername(input.Username)
	password := input.Password
	passwordProvided := strings.TrimSpace(password) != ""
	validation := ValidationErrors{}
	if username == "" {
		validation["username"] = "FTP username is required."
	} else if len(username) > maxUsernameLength || !usernamePattern.MatchString(username) {
		validation["username"] = "FTP username must start with a letter or number and use only lowercase letters, numbers, dots, underscores, or hyphens."
	}
	if input.Enabled && strings.TrimSpace(account.PasswordHash) == "" && !passwordProvided {
		validation["password"] = "Enter a password or generate one before enabling FTP."
	}
	if len(validation) > 0 {
		return DomainStatus{}, validation
	}

	if passwordProvided {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return DomainStatus{}, fmt.Errorf("hash ftp password: %w", err)
		}
		account.PasswordHash = string(hash)
	}

	account.Username = username
	account.Enabled = input.Enabled
	account.UpdatedAt = s.now().UTC()

	if err := s.store.Upsert(ctx, account); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return DomainStatus{}, ValidationErrors{
				"username": "This FTP username is already in use.",
			}
		}
		return DomainStatus{}, err
	}

	return statusFromAccount(account), nil
}

func (s *Service) Authenticate(ctx context.Context, username string, password string) (DomainStatus, bool, error) {
	if s == nil || s.store == nil {
		return DomainStatus{}, false, nil
	}

	account, err := s.store.GetByUsername(ctx, NormalizeUsername(username))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DomainStatus{}, false, nil
		}
		return DomainStatus{}, false, err
	}
	if !account.Enabled || strings.TrimSpace(account.PasswordHash) == "" {
		return DomainStatus{}, false, nil
	}

	if account.DomainID != "" {
		record, ok := s.findDomain(account.DomainID)
		if !ok || !IsSupportedKind(record.Kind) {
			return DomainStatus{}, false, nil
		}
	}
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)); err != nil {
		return DomainStatus{}, false, nil
	}

	return statusFromAccount(account), true, nil
}

func (s *Service) StatusForUsername(ctx context.Context, username string) (DomainStatus, bool, error) {
	if s == nil || s.store == nil {
		return DomainStatus{}, false, nil
	}

	account, err := s.store.GetByUsername(ctx, NormalizeUsername(username))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DomainStatus{}, false, nil
		}
		return DomainStatus{}, false, err
	}

	if account.DomainID != "" {
		record, ok := s.findDomain(account.DomainID)
		if !ok || !IsSupportedKind(record.Kind) {
			return DomainStatus{}, false, nil
		}
	}

	return statusFromAccount(account), true, nil
}

func (s *Service) domainStatus(ctx context.Context, record domain.Record) (DomainStatus, error) {
	if !IsSupportedKind(record.Kind) {
		return DomainStatus{Supported: false}, nil
	}

	account, err := s.ensurePrimaryDomainAccount(ctx, record)
	if err != nil {
		return DomainStatus{}, err
	}

	return statusFromAccount(account), nil
}

func (s *Service) ensurePrimaryDomainAccount(ctx context.Context, record domain.Record) (Account, error) {
	if s == nil || s.store == nil {
		return Account{}, nil
	}

	account, err := s.store.GetPrimaryByDomainID(ctx, record.ID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Account{}, err
	}

	now := s.now().UTC()
	rootPath, err := domain.ResolveDocumentRoot(s.basePath(), record)
	if err != nil {
		return Account{}, err
	}
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		return Account{}, fmt.Errorf("create ftp root path: %w", err)
	}

	account = Account{
		ID:           fmt.Sprintf("ftp-%d", now.UnixNano()),
		DomainID:     record.ID,
		Username:     NormalizeUsername(record.Hostname),
		RootPath:     rootPath,
		Enabled:      false,
		PasswordHash: "",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.Upsert(ctx, account); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return Account{}, ValidationErrors{
				"username": "The default FTP username is already in use.",
			}
		}
		return Account{}, err
	}

	return account, nil
}

func (s *Service) applyManagedAccountInput(
	account Account,
	username string,
	password string,
	rootPath string,
	domainID string,
	enabled *bool,
	passwordRequired bool,
) (Account, ValidationErrors, error) {
	normalizedUsername := NormalizeUsername(username)
	trimmedPassword := strings.TrimSpace(password)
	normalizedDomainID, validation, err := s.validateDomainID(domainID)
	if err != nil {
		return Account{}, nil, err
	}

	normalizedRootPath, rootValidation, err := s.normalizeRootPath(rootPath)
	if err != nil {
		return Account{}, nil, err
	}
	if rootValidation != "" {
		validation["root_path"] = rootValidation
	}

	if normalizedUsername == "" {
		validation["username"] = "FTP username is required."
	} else if len(normalizedUsername) > maxUsernameLength || !usernamePattern.MatchString(normalizedUsername) {
		validation["username"] = "FTP username must start with a letter or number and use only lowercase letters, numbers, dots, underscores, or hyphens."
	}
	if passwordRequired && trimmedPassword == "" {
		validation["password"] = "FTP password is required."
	}

	nextEnabled := account.Enabled
	if enabled != nil {
		nextEnabled = *enabled
	}
	if nextEnabled && strings.TrimSpace(account.PasswordHash) == "" && trimmedPassword == "" {
		validation["password"] = "Enter a password to enable this FTP account."
	}
	if len(validation) > 0 {
		return Account{}, validation, nil
	}

	account.DomainID = normalizedDomainID
	account.Username = normalizedUsername
	account.RootPath = normalizedRootPath
	account.Enabled = nextEnabled
	account.UpdatedAt = s.now().UTC()

	if trimmedPassword != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(trimmedPassword), bcrypt.DefaultCost)
		if err != nil {
			return Account{}, nil, fmt.Errorf("hash ftp password: %w", err)
		}
		account.PasswordHash = string(hash)
	}

	return account, nil, nil
}

func (s *Service) validateDomainID(domainID string) (string, ValidationErrors, error) {
	normalizedDomainID := strings.TrimSpace(domainID)
	validation := ValidationErrors{}
	if normalizedDomainID == "" {
		return "", validation, nil
	}

	record, ok := s.findDomain(normalizedDomainID)
	if !ok {
		validation["domain_id"] = "Select a valid domain."
		return "", validation, nil
	}
	if !IsSupportedKind(record.Kind) {
		validation["domain_id"] = "Select a valid domain."
		return "", validation, nil
	}

	return record.ID, validation, nil
}

func (s *Service) normalizeRootPath(value string) (string, string, error) {
	basePath := s.basePath()
	if basePath == "" {
		return "", "Document root base path is not configured.", nil
	}

	normalizedBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return "", "", fmt.Errorf("resolve ftp base path: %w", err)
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", "Document root is required.", nil
	}

	if !filepath.IsAbs(trimmed) {
		trimmed = filepath.Join(normalizedBasePath, trimmed)
	}

	normalizedRootPath, err := filepath.Abs(filepath.Clean(trimmed))
	if err != nil {
		return "", "", fmt.Errorf("resolve ftp root path: %w", err)
	}

	relativePath, err := filepath.Rel(normalizedBasePath, normalizedRootPath)
	if err != nil {
		return "", "", fmt.Errorf("resolve ftp root path: %w", err)
	}
	if relativePath == "." || relativePath == "" {
		return "", fmt.Sprintf("Document root must stay inside %s.", normalizedBasePath), nil
	}
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Sprintf("Document root must stay inside %s.", normalizedBasePath), nil
	}

	return normalizedRootPath, "", nil
}

func (s *Service) accountRecord(account Account) AccountRecord {
	record := AccountRecord{
		ID:          account.ID,
		DomainID:    strings.TrimSpace(account.DomainID),
		Username:    account.Username,
		RootPath:    account.RootPath,
		Enabled:     account.Enabled,
		HasPassword: strings.TrimSpace(account.PasswordHash) != "",
		CreatedAt:   account.CreatedAt,
		UpdatedAt:   account.UpdatedAt,
	}

	if record.DomainID != "" {
		if domainRecord, ok := s.findDomain(record.DomainID); ok {
			record.DomainName = domainRecord.Hostname
		}
	}

	return record
}

func (s *Service) findDomain(domainID string) (domain.Record, bool) {
	if s == nil || s.domains == nil {
		return domain.Record{}, false
	}

	return s.domains.FindByID(domainID)
}

func (s *Service) basePath() string {
	if s == nil || s.domains == nil {
		return ""
	}

	return strings.TrimSpace(s.domains.BasePath())
}

func statusFromAccount(account Account) DomainStatus {
	return DomainStatus{
		Supported:   true,
		Enabled:     account.Enabled,
		Username:    account.Username,
		RootPath:    account.RootPath,
		HasPassword: strings.TrimSpace(account.PasswordHash) != "",
	}
}
