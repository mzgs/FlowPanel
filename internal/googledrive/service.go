package googledrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowpanel/internal/config"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	drive "google.golang.org/api/drive/v3"
	googleoauth "google.golang.org/api/oauth2/v2"
	"google.golang.org/api/option"
)

const (
	backupFolderName        = "FlowPanel Backups"
	appPropertyKey          = "flowpanel_kind"
	appPropertyBackup       = "backup"
	appPropertyBackupFolder = "backup_folder"
	userinfoEmailScope      = "https://www.googleapis.com/auth/userinfo.email"
	maxOAuthCredentialsSize = 1 << 20
)

var (
	ErrNotConfigured          = errors.New("google drive integration is not configured")
	ErrNotConnected           = errors.New("google drive is not connected")
	ErrNotFound               = errors.New("google drive backup not found")
	ErrInvalidOAuthConfigJSON = errors.New("google drive oauth credentials JSON is invalid")
)

type Service struct {
	clientID        string
	clientSecret    string
	credentialsPath string
}

type Connection struct {
	Email        string
	RefreshToken string
}

type File struct {
	ID        string
	Name      string
	Size      int64
	CreatedAt time.Time
}

type oauthCredentials struct {
	ClientID     string
	ClientSecret string
}

type oauthCredentialsFile struct {
	Web       *oauthCredentialsPayload `json:"web"`
	Installed *oauthCredentialsPayload `json:"installed"`
}

type oauthCredentialsPayload struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func NewService(cfg config.GoogleDriveConfig) *Service {
	return &Service{
		clientID:        strings.TrimSpace(cfg.ClientID),
		clientSecret:    strings.TrimSpace(cfg.ClientSecret),
		credentialsPath: filepath.Clean(strings.TrimSpace(cfg.CredentialsPath)),
	}
}

func (s *Service) Enabled() bool {
	_, err := s.credentials()
	return err == nil
}

func (s *Service) SaveOAuthCredentialsJSON(file io.Reader) error {
	if s == nil || strings.TrimSpace(s.credentialsPath) == "" {
		return ErrNotConfigured
	}
	if file == nil {
		return fmt.Errorf("%w: upload a JSON file", ErrInvalidOAuthConfigJSON)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxOAuthCredentialsSize+1))
	if err != nil {
		return fmt.Errorf("read google drive oauth credentials JSON: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("%w: upload a non-empty JSON file", ErrInvalidOAuthConfigJSON)
	}
	if len(data) > maxOAuthCredentialsSize {
		return fmt.Errorf("%w: file must be smaller than %d bytes", ErrInvalidOAuthConfigJSON, maxOAuthCredentialsSize)
	}

	if _, err := parseOAuthCredentialsJSON(data); err != nil {
		return err
	}

	dir := filepath.Dir(s.credentialsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create google drive credentials directory: %w", err)
	}

	tempFile, err := os.CreateTemp(dir, ".google-drive-oauth-*.json")
	if err != nil {
		return fmt.Errorf("create google drive temporary credentials file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write google drive oauth credentials JSON: %w", err)
	}
	if err := tempFile.Chmod(0o600); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set google drive oauth credentials permissions: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close google drive oauth credentials JSON: %w", err)
	}
	if err := os.Rename(tempPath, s.credentialsPath); err != nil {
		return fmt.Errorf("persist google drive oauth credentials JSON: %w", err)
	}

	return nil
}

func (s *Service) AuthURL(state string, redirectURI string) (string, error) {
	if !s.Enabled() {
		return "", ErrNotConfigured
	}
	if strings.TrimSpace(state) == "" {
		return "", errors.New("oauth state is required")
	}

	return s.oauthConfig(strings.TrimSpace(redirectURI)).AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"),
	), nil
}

func (s *Service) Exchange(ctx context.Context, redirectURI string, code string) (Connection, error) {
	if !s.Enabled() {
		return Connection{}, ErrNotConfigured
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return Connection{}, errors.New("google drive authorization code is required")
	}

	token, err := s.oauthConfig(strings.TrimSpace(redirectURI)).Exchange(ctx, code)
	if err != nil {
		return Connection{}, fmt.Errorf("exchange google drive authorization code: %w", err)
	}

	refreshToken := strings.TrimSpace(token.RefreshToken)
	if refreshToken == "" {
		return Connection{}, errors.New("google did not return a refresh token")
	}

	httpClient := s.oauthConfig(strings.TrimSpace(redirectURI)).Client(ctx, token)
	userService, err := googleoauth.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return Connection{}, fmt.Errorf("build google oauth client: %w", err)
	}

	userInfo, err := userService.Userinfo.Get().Do()
	if err != nil {
		return Connection{}, fmt.Errorf("load google account details: %w", err)
	}

	return Connection{
		Email:        strings.TrimSpace(userInfo.Email),
		RefreshToken: refreshToken,
	}, nil
}

func (s *Service) ListBackups(ctx context.Context, refreshToken string) ([]File, error) {
	driveService, err := s.driveService(ctx, refreshToken)
	if err != nil {
		return nil, err
	}

	folderID, err := s.lookupBackupFolder(ctx, driveService)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return []File{}, nil
	}

	response, err := driveService.Files.List().
		Q(fmt.Sprintf("'%s' in parents and trashed = false and appProperties has { key='%s' and value='%s' }", folderID, appPropertyKey, appPropertyBackup)).
		OrderBy("createdTime desc").
		Fields("files(id,name,size,createdTime)").
		PageSize(200).
		Context(ctx).
		Do()
	if err != nil {
		return nil, fmt.Errorf("list google drive backups: %w", err)
	}

	records := make([]File, 0, len(response.Files))
	for _, file := range response.Files {
		record, err := driveFileRecord(file)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}

	return records, nil
}

func (s *Service) UploadBackup(ctx context.Context, refreshToken string, name string, archive io.Reader) (File, error) {
	driveService, err := s.driveService(ctx, refreshToken)
	if err != nil {
		return File{}, err
	}

	folderID, err := s.ensureBackupFolder(ctx, driveService)
	if err != nil {
		return File{}, err
	}

	file := &drive.File{
		Name:          strings.TrimSpace(name),
		Parents:       []string{folderID},
		AppProperties: map[string]string{appPropertyKey: appPropertyBackup},
	}

	created, err := driveService.Files.Create(file).
		Media(archive).
		Fields("id,name,size,createdTime").
		Do()
	if err != nil {
		return File{}, fmt.Errorf("upload google drive backup: %w", err)
	}

	return driveFileRecord(created)
}

func (s *Service) LookupBackup(ctx context.Context, refreshToken string, fileID string) (File, error) {
	driveService, err := s.driveService(ctx, refreshToken)
	if err != nil {
		return File{}, err
	}

	file, err := s.lookupBackupFile(ctx, driveService, fileID)
	if err != nil {
		return File{}, err
	}

	return driveFileRecord(file)
}

func (s *Service) DownloadBackup(ctx context.Context, refreshToken string, fileID string) (io.ReadCloser, File, error) {
	driveService, err := s.driveService(ctx, refreshToken)
	if err != nil {
		return nil, File{}, err
	}

	file, err := s.lookupBackupFile(ctx, driveService, fileID)
	if err != nil {
		return nil, File{}, err
	}

	response, err := driveService.Files.Get(strings.TrimSpace(fileID)).Download()
	if err != nil {
		return nil, File{}, fmt.Errorf("download google drive backup: %w", err)
	}

	record, err := driveFileRecord(file)
	if err != nil {
		_ = response.Body.Close()
		return nil, File{}, err
	}

	return response.Body, record, nil
}

func (s *Service) DeleteBackup(ctx context.Context, refreshToken string, fileID string) error {
	driveService, err := s.driveService(ctx, refreshToken)
	if err != nil {
		return err
	}

	if _, err := s.lookupBackupFile(ctx, driveService, fileID); err != nil {
		return err
	}

	if err := driveService.Files.Delete(strings.TrimSpace(fileID)).Do(); err != nil {
		return fmt.Errorf("delete google drive backup: %w", err)
	}

	return nil
}

func (s *Service) oauthConfig(redirectURI string) *oauth2.Config {
	credentials, _ := s.credentials()
	return &oauth2.Config{
		ClientID:     credentials.ClientID,
		ClientSecret: credentials.ClientSecret,
		RedirectURL:  strings.TrimSpace(redirectURI),
		Endpoint:     google.Endpoint,
		Scopes:       []string{drive.DriveFileScope, userinfoEmailScope},
	}
}

func (s *Service) credentials() (oauthCredentials, error) {
	if s == nil {
		return oauthCredentials{}, ErrNotConfigured
	}

	if strings.TrimSpace(s.credentialsPath) != "" {
		data, err := os.ReadFile(s.credentialsPath)
		switch {
		case err == nil:
			return parseOAuthCredentialsJSON(data)
		case errors.Is(err, os.ErrNotExist):
		default:
			return oauthCredentials{}, fmt.Errorf("read google drive oauth credentials JSON: %w", err)
		}
	}

	clientID := strings.TrimSpace(s.clientID)
	clientSecret := strings.TrimSpace(s.clientSecret)
	if clientID == "" || clientSecret == "" {
		return oauthCredentials{}, ErrNotConfigured
	}

	return oauthCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

func parseOAuthCredentialsJSON(data []byte) (oauthCredentials, error) {
	var payload oauthCredentialsFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return oauthCredentials{}, fmt.Errorf("%w: %v", ErrInvalidOAuthConfigJSON, err)
	}

	source := payload.Web
	if source == nil {
		source = payload.Installed
	}
	if source == nil {
		return oauthCredentials{}, fmt.Errorf("%w: expected a web or installed credentials object", ErrInvalidOAuthConfigJSON)
	}

	clientID := strings.TrimSpace(source.ClientID)
	clientSecret := strings.TrimSpace(source.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return oauthCredentials{}, fmt.Errorf("%w: client_id and client_secret are required", ErrInvalidOAuthConfigJSON)
	}

	return oauthCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

func (s *Service) driveService(ctx context.Context, refreshToken string) (*drive.Service, error) {
	if !s.Enabled() {
		return nil, ErrNotConfigured
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, ErrNotConnected
	}

	tokenSource := s.oauthConfig("").TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	httpClient := oauth2.NewClient(ctx, tokenSource)
	service, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("build google drive client: %w", err)
	}

	return service, nil
}

func (s *Service) ensureBackupFolder(ctx context.Context, driveService *drive.Service) (string, error) {
	folderID, err := s.lookupBackupFolder(ctx, driveService)
	if err != nil {
		return "", err
	}
	if folderID != "" {
		return folderID, nil
	}

	folder, err := driveService.Files.Create(&drive.File{
		Name:          backupFolderName,
		MimeType:      "application/vnd.google-apps.folder",
		AppProperties: map[string]string{appPropertyKey: appPropertyBackupFolder},
	}).
		Fields("id").
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("create google drive backup folder: %w", err)
	}

	return strings.TrimSpace(folder.Id), nil
}

func (s *Service) lookupBackupFolder(ctx context.Context, driveService *drive.Service) (string, error) {
	response, err := driveService.Files.List().
		Q(fmt.Sprintf("mimeType = 'application/vnd.google-apps.folder' and trashed = false and appProperties has { key='%s' and value='%s' }", appPropertyKey, appPropertyBackupFolder)).
		OrderBy("createdTime desc").
		Fields("files(id)").
		PageSize(1).
		Context(ctx).
		Do()
	if err != nil {
		return "", fmt.Errorf("lookup google drive backup folder: %w", err)
	}
	if len(response.Files) == 0 {
		return "", nil
	}

	return strings.TrimSpace(response.Files[0].Id), nil
}

func (s *Service) lookupBackupFile(ctx context.Context, driveService *drive.Service, fileID string) (*drive.File, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, ErrNotFound
	}

	file, err := driveService.Files.Get(fileID).
		Fields("id,name,size,createdTime,trashed,appProperties").
		Context(ctx).
		Do()
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load google drive backup: %w", err)
	}
	if file == nil || file.Trashed || file.AppProperties[appPropertyKey] != appPropertyBackup {
		return nil, ErrNotFound
	}

	return file, nil
}

func driveFileRecord(file *drive.File) (File, error) {
	if file == nil {
		return File{}, errors.New("google drive file is required")
	}

	createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(file.CreatedTime))
	if err != nil {
		return File{}, fmt.Errorf("parse google drive backup created time: %w", err)
	}

	return File{
		ID:        strings.TrimSpace(file.Id),
		Name:      strings.TrimSpace(file.Name),
		Size:      file.Size,
		CreatedAt: createdAt.UTC(),
	}, nil
}

func isNotFound(err error) bool {
	var googleErr interface{ Error() string }
	if errors.As(err, &googleErr) && strings.Contains(strings.ToLower(googleErr.Error()), "404") {
		return true
	}
	return false
}
