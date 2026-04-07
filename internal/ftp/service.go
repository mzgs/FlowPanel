package ftp

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
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
	generatedPassLength = 20
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type DomainStatus struct {
	Supported   bool   `json:"supported"`
	Enabled     bool   `json:"enabled"`
	Username    string `json:"username"`
	RootPath    string `json:"root_path"`
	HasPassword bool   `json:"has_password"`
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
	return kind == domain.KindStaticSite || kind == domain.KindPHP
}

func NormalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

	_, err := s.ensureAccount(ctx, record)
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
			"domain": "FTP is available only for Static site and Php site domains.",
		}
	}

	account, err := s.ensureAccount(ctx, record)
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

	return s.domainStatus(ctx, record)
}

func (s *Service) ResetPassword(ctx context.Context, domainID string) (DomainStatus, string, error) {
	record, ok := s.findDomain(domainID)
	if !ok {
		return DomainStatus{}, "", domain.ErrNotFound
	}
	if !IsSupportedKind(record.Kind) {
		return DomainStatus{}, "", ValidationErrors{
			"domain": "FTP is available only for Static site and Php site domains.",
		}
	}

	account, err := s.ensureAccount(ctx, record)
	if err != nil {
		return DomainStatus{}, "", err
	}

	password, err := generatePassword(generatedPassLength)
	if err != nil {
		return DomainStatus{}, "", fmt.Errorf("generate ftp password: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return DomainStatus{}, "", fmt.Errorf("hash ftp password: %w", err)
	}

	account.PasswordHash = string(hash)
	account.UpdatedAt = s.now().UTC()
	if err := s.store.Upsert(ctx, account); err != nil {
		return DomainStatus{}, "", err
	}

	status, err := s.domainStatus(ctx, record)
	if err != nil {
		return DomainStatus{}, "", err
	}

	return status, password, nil
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

	record, ok := s.findDomain(account.DomainID)
	if !ok || !IsSupportedKind(record.Kind) {
		return DomainStatus{}, false, nil
	}
	if err := bcrypt.CompareHashAndPassword([]byte(account.PasswordHash), []byte(password)); err != nil {
		return DomainStatus{}, false, nil
	}

	status := statusFromRecord(record, account)
	return status, true, nil
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

	record, ok := s.findDomain(account.DomainID)
	if !ok || !IsSupportedKind(record.Kind) {
		return DomainStatus{}, false, nil
	}

	return statusFromRecord(record, account), true, nil
}

func (s *Service) domainStatus(ctx context.Context, record domain.Record) (DomainStatus, error) {
	if !IsSupportedKind(record.Kind) {
		return DomainStatus{Supported: false}, nil
	}

	account, err := s.ensureAccount(ctx, record)
	if err != nil {
		return DomainStatus{}, err
	}

	return statusFromRecord(record, account), nil
}

func (s *Service) ensureAccount(ctx context.Context, record domain.Record) (Account, error) {
	if s == nil || s.store == nil {
		return Account{}, nil
	}

	account, err := s.store.GetByDomainID(ctx, record.ID)
	if err == nil {
		return account, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Account{}, err
	}

	now := s.now().UTC()
	account = Account{
		DomainID:     record.ID,
		Username:     NormalizeUsername(record.Hostname),
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

func (s *Service) findDomain(domainID string) (domain.Record, bool) {
	if s == nil || s.domains == nil {
		return domain.Record{}, false
	}

	return s.domains.FindByID(domainID)
}

func statusFromRecord(record domain.Record, account Account) DomainStatus {
	return DomainStatus{
		Supported:   true,
		Enabled:     account.Enabled,
		Username:    account.Username,
		RootPath:    record.Target,
		HasPassword: strings.TrimSpace(account.PasswordHash) != "",
	}
}

func generatePassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	if length <= 0 {
		length = generatedPassLength
	}

	buf := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}

	for i := range buf {
		buf[i] = alphabet[int(random[i])%len(alphabet)]
	}

	return string(buf), nil
}
