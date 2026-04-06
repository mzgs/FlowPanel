package googledrive

import (
	"errors"
	"strings"
	"testing"

	"flowpanel/internal/config"
)

const testOAuthCredentialsJSON = `{
  "web": {
    "client_id": "test-client-id.apps.googleusercontent.com",
    "project_id": "flowpanel-test",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
    "client_secret": "test-client-secret",
    "redirect_uris": ["http://localhost:8080/api/settings/google-drive/callback"]
  }
}`

func TestServiceCanSaveOAuthCredentialsJSON(t *testing.T) {
	t.Parallel()

	service := NewService(config.GoogleDriveConfig{
		CredentialsPath: t.TempDir() + "/google-drive-oauth-client.json",
	})

	if err := service.SaveOAuthCredentialsJSON(strings.NewReader(testOAuthCredentialsJSON)); err != nil {
		t.Fatalf("save oauth credentials JSON: %v", err)
	}
	if !service.Enabled() {
		t.Fatal("service should be enabled after saving credentials JSON")
	}

	authURL, err := service.AuthURL("state", "http://localhost:8080/api/settings/google-drive/callback")
	if err != nil {
		t.Fatalf("build auth url: %v", err)
	}
	if !strings.Contains(authURL, "client_id=test-client-id.apps.googleusercontent.com") {
		t.Fatalf("auth url = %q, want client id", authURL)
	}
}

func TestServiceRejectsInvalidOAuthCredentialsJSON(t *testing.T) {
	t.Parallel()

	service := NewService(config.GoogleDriveConfig{
		CredentialsPath: t.TempDir() + "/google-drive-oauth-client.json",
	})

	err := service.SaveOAuthCredentialsJSON(strings.NewReader(`{"web":{}}`))
	if !errors.Is(err, ErrInvalidOAuthConfigJSON) {
		t.Fatalf("save error = %v, want %v", err, ErrInvalidOAuthConfigJSON)
	}
}
