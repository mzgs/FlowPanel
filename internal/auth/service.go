package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLength = 8
	maxUsernameLength = 64
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

var ErrInvalidCredentials = errors.New("invalid username or password")

type ValidationErrors map[string]string

func (v ValidationErrors) Error() string {
	return "panel auth validation failed"
}

type CreateInitialAdminInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type PublicUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type Service struct {
	store *Store
	now   func() time.Time
}

func NewService(store *Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

func NormalizeUsername(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func PublicUserFromRecord(user User) PublicUser {
	return PublicUser{
		ID:       user.ID,
		Username: user.Username,
	}
}

func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}

	count, err := s.store.Count(ctx)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *Service) GetUser(ctx context.Context, id string) (PublicUser, bool, error) {
	id = strings.TrimSpace(id)
	if s == nil || s.store == nil || id == "" {
		return PublicUser{}, false, nil
	}

	user, err := s.store.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PublicUser{}, false, nil
		}
		return PublicUser{}, false, err
	}

	return PublicUserFromRecord(user), true, nil
}

func (s *Service) CreateInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (PublicUser, error) {
	if s == nil || s.store == nil {
		return PublicUser{}, errors.New("panel auth is not configured")
	}

	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return PublicUser{}, err
	}
	if hasUsers {
		return PublicUser{}, ValidationErrors{"username": "Panel setup is already complete."}
	}

	return s.createInitialAdmin(ctx, input)
}

func (s *Service) EnsureInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (PublicUser, bool, error) {
	if s == nil || s.store == nil {
		return PublicUser{}, false, errors.New("panel auth is not configured")
	}

	hasUsers, err := s.HasUsers(ctx)
	if err != nil {
		return PublicUser{}, false, err
	}
	if hasUsers {
		return PublicUser{}, false, nil
	}

	user, err := s.createInitialAdmin(ctx, input)
	if err != nil {
		return PublicUser{}, false, err
	}

	return user, true, nil
}

func (s *Service) createInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (PublicUser, error) {
	username, password, validation := validateCredentialsInput(input.Username, input.Password)
	if len(validation) > 0 {
		return PublicUser{}, validation
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return PublicUser{}, fmt.Errorf("hash panel password: %w", err)
	}

	now := s.now().UTC()
	user := User{
		ID:           fmt.Sprintf("panel-user-%d", now.UnixNano()),
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.InsertInitial(ctx, user); err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return PublicUser{}, ValidationErrors{"username": "Panel setup is already complete."}
		}
		return PublicUser{}, err
	}

	return PublicUserFromRecord(user), nil
}

func (s *Service) Authenticate(ctx context.Context, input LoginInput) (PublicUser, error) {
	if s == nil || s.store == nil {
		return PublicUser{}, errors.New("panel auth is not configured")
	}

	username := NormalizeUsername(input.Username)
	password := strings.TrimSpace(input.Password)
	if username == "" || password == "" {
		return PublicUser{}, ErrInvalidCredentials
	}

	user, err := s.store.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PublicUser{}, ErrInvalidCredentials
		}
		return PublicUser{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return PublicUser{}, ErrInvalidCredentials
	}

	return PublicUserFromRecord(user), nil
}

func validateCredentialsInput(usernameValue string, passwordValue string) (string, string, ValidationErrors) {
	username := NormalizeUsername(usernameValue)
	password := strings.TrimSpace(passwordValue)
	validation := ValidationErrors{}

	if username == "" {
		validation["username"] = "Username is required."
	} else if len(username) > maxUsernameLength || !usernamePattern.MatchString(username) {
		validation["username"] = "Username must start with a letter or number and use lowercase letters, numbers, dots, underscores, or hyphens."
	}

	if password == "" {
		validation["password"] = "Password is required."
	} else if len(password) < minPasswordLength {
		validation["password"] = "Password must be at least 8 characters."
	}

	return username, password, validation
}
