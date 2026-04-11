package httpx

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"flowpanel/internal/domain"
	"flowpanel/internal/settings"
)

const githubActionTimeout = 10 * time.Minute

var (
	errGitHubUnsupportedDomain        = errors.New("github integration is not available for this domain")
	errGitHubMissingToken             = errors.New("set a GitHub token in Settings before connecting a repository")
	errGitHubMissingRepositoryURL     = errors.New("repository URL is required")
	errGitHubInvalidRepositoryURL     = errors.New("enter a valid GitHub repository URL")
	errGitHubIntegrationNotConfigured = errors.New("github integration is not configured for this domain")
	errGitHubUnavailable              = errors.New("git is not installed on this server")
	errGitHubInvalidWebhookSignature  = errors.New("invalid GitHub webhook signature")
	errGitHubWebhookURLNotPublic      = errors.New("github webhooks require an HTTPS callback URL unless the host is localhost")
)

var githubAPIBaseURL = "https://api.github.com"

type domainGitHubIntegrationInput struct {
	RepositoryURL    string `json:"repository_url"`
	AutoDeployOnPush bool   `json:"auto_deploy_on_push"`
	PostFetchScript  string `json:"post_fetch_script"`
}

type gitHubRepositoryRef struct {
	Owner string
	Name  string
}

type gitHubRepositoryMetadata struct {
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
}

type gitHubWebhookResponse struct {
	ID int64 `json:"id"`
}

type gitHubWebhookRecord struct {
	ID     int64 `json:"id"`
	Config struct {
		URL string `json:"url"`
	} `json:"config"`
}

type gitHubWebhookPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		DefaultBranch string `json:"default_branch"`
		CloneURL      string `json:"clone_url"`
		HTMLURL       string `json:"html_url"`
	} `json:"repository"`
}

type domainGitHubDeployResult struct {
	Action string `json:"action"`
}

func ensureGitHubIntegrationSupported(record domain.Record) error {
	if !domain.SupportsManagedDocumentRoot(record.Kind) {
		return errGitHubUnsupportedDomain
	}

	return nil
}

func getGitHubToken(ctx context.Context, service *settings.Service) (string, error) {
	if service == nil {
		return "", errGitHubMissingToken
	}

	record, err := service.Get(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(record.GitHubToken) == "" {
		return "", errGitHubMissingToken
	}

	return strings.TrimSpace(record.GitHubToken), nil
}

func parseGitHubRepositoryURL(raw string) (gitHubRepositoryRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return gitHubRepositoryRef{}, errGitHubMissingRepositoryURL
	}

	if strings.HasPrefix(raw, "git@github.com:") {
		pathValue := strings.TrimPrefix(raw, "git@github.com:")
		return parseGitHubRepositoryPath(pathValue)
	}

	parsed, err := neturl.Parse(raw)
	if err != nil {
		return gitHubRepositoryRef{}, errGitHubInvalidRepositoryURL
	}
	if !strings.EqualFold(parsed.Hostname(), "github.com") {
		return gitHubRepositoryRef{}, errGitHubInvalidRepositoryURL
	}

	return parseGitHubRepositoryPath(strings.TrimPrefix(parsed.Path, "/"))
}

func parseGitHubRepositoryPath(pathValue string) (gitHubRepositoryRef, error) {
	pathValue = strings.TrimSpace(strings.Trim(pathValue, "/"))
	pathValue = strings.TrimSuffix(pathValue, ".git")
	parts := strings.Split(pathValue, "/")
	if len(parts) != 2 {
		return gitHubRepositoryRef{}, errGitHubInvalidRepositoryURL
	}

	owner := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if owner == "" || name == "" {
		return gitHubRepositoryRef{}, errGitHubInvalidRepositoryURL
	}

	return gitHubRepositoryRef{Owner: owner, Name: name}, nil
}

func sameGitHubRepository(left string, right string) bool {
	leftRef, leftErr := parseGitHubRepositoryURL(left)
	rightRef, rightErr := parseGitHubRepositoryURL(right)
	if leftErr != nil || rightErr != nil {
		return false
	}

	return strings.EqualFold(leftRef.Owner, rightRef.Owner) && strings.EqualFold(leftRef.Name, rightRef.Name)
}

func loadGitHubRepositoryMetadata(
	ctx context.Context,
	token string,
	ref gitHubRepositoryRef,
) (gitHubRepositoryMetadata, error) {
	requestURL := fmt.Sprintf("%s/repos/%s/%s", strings.TrimRight(githubAPIBaseURL, "/"), neturl.PathEscape(ref.Owner), neturl.PathEscape(ref.Name))
	response, err := sendGitHubAPIRequest(ctx, stdhttp.MethodGet, requestURL, token, nil)
	if err != nil {
		return gitHubRepositoryMetadata{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != stdhttp.StatusOK {
		return gitHubRepositoryMetadata{}, readGitHubAPIError(response, "load repository")
	}

	var metadata gitHubRepositoryMetadata
	if err := json.NewDecoder(response.Body).Decode(&metadata); err != nil {
		return gitHubRepositoryMetadata{}, fmt.Errorf("decode repository metadata: %w", err)
	}
	if strings.TrimSpace(metadata.CloneURL) == "" || strings.TrimSpace(metadata.DefaultBranch) == "" {
		return gitHubRepositoryMetadata{}, errors.New("repository metadata is incomplete")
	}

	return metadata, nil
}

func upsertGitHubWebhook(
	ctx context.Context,
	token string,
	ref gitHubRepositoryRef,
	webhookID int64,
	webhookURL string,
	secret string,
) (int64, error) {
	payload := map[string]any{
		"active": true,
		"events": []string{"push"},
		"config": map[string]string{
			"url":          webhookURL,
			"content_type": "json",
			"secret":       secret,
			"insecure_ssl": "0",
		},
	}

	method := stdhttp.MethodPost
	requestURL := fmt.Sprintf("%s/repos/%s/%s/hooks", strings.TrimRight(githubAPIBaseURL, "/"), neturl.PathEscape(ref.Owner), neturl.PathEscape(ref.Name))
	if webhookID > 0 {
		method = stdhttp.MethodPatch
		requestURL = fmt.Sprintf("%s/%d", requestURL, webhookID)
	}

	response, err := sendGitHubAPIRequest(ctx, method, requestURL, token, payload)
	if err != nil {
		return 0, err
	}

	if response.StatusCode == stdhttp.StatusNotFound && webhookID > 0 {
		response.Body.Close()
		return upsertGitHubWebhook(ctx, token, ref, 0, webhookURL, secret)
	}
	if response.StatusCode == stdhttp.StatusUnprocessableEntity && webhookID == 0 {
		response.Body.Close()
		existingWebhookID, err := findGitHubWebhookIDByURL(ctx, token, ref, webhookURL)
		if err != nil {
			return 0, err
		}
		if existingWebhookID > 0 {
			return upsertGitHubWebhook(ctx, token, ref, existingWebhookID, webhookURL, secret)
		}
		response, err = sendGitHubAPIRequest(ctx, method, requestURL, token, payload)
		if err != nil {
			return 0, err
		}
	}
	defer response.Body.Close()

	if response.StatusCode != stdhttp.StatusCreated && response.StatusCode != stdhttp.StatusOK {
		return 0, readGitHubAPIError(response, "configure webhook")
	}

	var webhook gitHubWebhookResponse
	if err := json.NewDecoder(response.Body).Decode(&webhook); err != nil {
		return 0, fmt.Errorf("decode webhook response: %w", err)
	}
	if webhook.ID <= 0 {
		return 0, errors.New("github webhook response did not include an id")
	}

	return webhook.ID, nil
}

func findGitHubWebhookIDByURL(
	ctx context.Context,
	token string,
	ref gitHubRepositoryRef,
	webhookURL string,
) (int64, error) {
	requestURL := fmt.Sprintf("%s/repos/%s/%s/hooks", strings.TrimRight(githubAPIBaseURL, "/"), neturl.PathEscape(ref.Owner), neturl.PathEscape(ref.Name))
	response, err := sendGitHubAPIRequest(ctx, stdhttp.MethodGet, requestURL, token, nil)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	if response.StatusCode != stdhttp.StatusOK {
		return 0, readGitHubAPIError(response, "list webhooks")
	}

	var hooks []gitHubWebhookRecord
	if err := json.NewDecoder(response.Body).Decode(&hooks); err != nil {
		return 0, fmt.Errorf("decode webhook list: %w", err)
	}

	for _, hook := range hooks {
		if strings.TrimSpace(hook.Config.URL) == strings.TrimSpace(webhookURL) {
			return hook.ID, nil
		}
	}

	return 0, nil
}

func deleteGitHubWebhook(
	ctx context.Context,
	token string,
	ref gitHubRepositoryRef,
	webhookID int64,
) error {
	if webhookID <= 0 {
		return nil
	}

	requestURL := fmt.Sprintf("%s/repos/%s/%s/hooks/%d", strings.TrimRight(githubAPIBaseURL, "/"), neturl.PathEscape(ref.Owner), neturl.PathEscape(ref.Name), webhookID)
	response, err := sendGitHubAPIRequest(ctx, stdhttp.MethodDelete, requestURL, token, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode == stdhttp.StatusNotFound {
		return nil
	}
	if response.StatusCode != stdhttp.StatusNoContent && response.StatusCode != stdhttp.StatusOK {
		return readGitHubAPIError(response, "delete webhook")
	}

	return nil
}

func sendGitHubAPIRequest(
	ctx context.Context,
	method string,
	requestURL string,
	token string,
	body any,
) (*stdhttp.Response, error) {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode GitHub API request: %w", err)
		}
		requestBody = bytes.NewReader(data)
	}

	request, err := stdhttp.NewRequestWithContext(ctx, method, requestURL, requestBody)
	if err != nil {
		return nil, fmt.Errorf("create GitHub API request: %w", err)
	}

	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := stdhttp.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send GitHub API request: %w", err)
	}

	return response, nil
}

func readGitHubAPIError(response *stdhttp.Response, action string) error {
	message := fmt.Sprintf("GitHub API %s request failed with status %d", action, response.StatusCode)
	var payload struct {
		Message string `json:"message"`
		Errors  []struct {
			Resource string `json:"resource"`
			Field    string `json:"field"`
			Code     string `json:"code"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err == nil {
		if strings.TrimSpace(payload.Message) != "" {
			message = payload.Message
		}
		details := make([]string, 0, len(payload.Errors))
		for _, item := range payload.Errors {
			if strings.TrimSpace(item.Message) != "" {
				details = append(details, strings.TrimSpace(item.Message))
				continue
			}

			parts := make([]string, 0, 3)
			if strings.TrimSpace(item.Resource) != "" {
				parts = append(parts, strings.TrimSpace(item.Resource))
			}
			if strings.TrimSpace(item.Field) != "" {
				parts = append(parts, strings.TrimSpace(item.Field))
			}
			if strings.TrimSpace(item.Code) != "" {
				parts = append(parts, strings.TrimSpace(item.Code))
			}
			if len(parts) > 0 {
				details = append(details, strings.Join(parts, " "))
			}
		}
		if len(details) > 0 {
			message = message + ": " + strings.Join(details, "; ")
		}
	}

	return errors.New(message)
}

func buildGitHubWebhookURL(r *stdhttp.Request, hostname string, panelURL string) (string, error) {
	baseURL := strings.TrimSpace(panelURL)
	if baseURL == "" {
		baseURL = requestBaseURL(r)
	}
	parsedBaseURL, err := neturl.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook base URL: %w", err)
	}
	if !isWebhookURLAllowed(parsedBaseURL) {
		return "", fmt.Errorf("%w: %s", errGitHubWebhookURLNotPublic, baseURL)
	}

	return fmt.Sprintf("%s/api/domains/%s/github/webhook", strings.TrimRight(baseURL, "/"), neturl.PathEscape(strings.TrimSpace(hostname))), nil
}

func requestBaseURL(r *stdhttp.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func isWebhookURLAllowed(value *neturl.URL) bool {
	if value == nil {
		return false
	}
	if strings.EqualFold(value.Scheme, "https") {
		return true
	}

	host := strings.Trim(strings.ToLower(strings.TrimSpace(value.Hostname())), "[]")
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func generateGitHubWebhookSecret() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func runDomainGitHubDeploy(
	ctx context.Context,
	basePath string,
	record domain.Record,
	integration domain.GitHubIntegration,
	token string,
) (domainGitHubDeployResult, error) {
	if err := ensureGitHubIntegrationSupported(record); err != nil {
		return domainGitHubDeployResult{}, err
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return domainGitHubDeployResult{}, errGitHubUnavailable
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, ok := runCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, githubActionTimeout)
		defer cancel()
	}

	targetPath, err := domain.ResolveDocumentRoot(basePath, record)
	if err != nil {
		return domainGitHubDeployResult{}, fmt.Errorf("resolve domain document root: %w", err)
	}
	branch := branchForIntegration(integration)
	if branch == "" {
		return domainGitHubDeployResult{}, errors.New("github default branch is not configured")
	}
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return domainGitHubDeployResult{}, fmt.Errorf("create domain document root: %w", err)
	}

	gitDir := filepath.Join(targetPath, ".git")
	if _, err := os.Stat(gitDir); errors.Is(err, os.ErrNotExist) {
		if err := initializeGitRepositoryInPlace(runCtx, gitPath, targetPath, integration.RepositoryURL, branch, token, integration.PostFetchScript); err != nil {
			return domainGitHubDeployResult{}, err
		}
		return domainGitHubDeployResult{Action: "initialized"}, nil
	} else if err != nil {
		return domainGitHubDeployResult{}, fmt.Errorf("inspect git directory: %w", err)
	} else {
		remoteURL, err := runGitCommand(runCtx, gitPath, targetPath, token, "remote", "get-url", "origin")
		switch {
		case err == nil:
			if strings.TrimSpace(remoteURL) != strings.TrimSpace(integration.RepositoryURL) {
				if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "remote", "set-url", "origin", integration.RepositoryURL); err != nil {
					return domainGitHubDeployResult{}, err
				}
			}
		case strings.Contains(strings.ToLower(err.Error()), "no such remote"):
			if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "remote", "add", "origin", integration.RepositoryURL); err != nil {
				return domainGitHubDeployResult{}, err
			}
		default:
			return domainGitHubDeployResult{}, err
		}
	}

	if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "fetch", "--depth", "1", "origin", branch); err != nil {
		return domainGitHubDeployResult{}, err
	}

	if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "checkout", "--force", "-B", branch, "origin/"+branch); err != nil {
		return domainGitHubDeployResult{}, err
	}
	if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "reset", "--hard", "origin/"+branch); err != nil {
		return domainGitHubDeployResult{}, err
	}
	if _, err := runGitCommand(runCtx, gitPath, targetPath, token, "clean", "-fd"); err != nil {
		return domainGitHubDeployResult{}, err
	}
	if err := runGitHubPostFetchScript(runCtx, targetPath, integration.PostFetchScript); err != nil {
		return domainGitHubDeployResult{}, err
	}

	return domainGitHubDeployResult{Action: "updated"}, nil
}

func branchForIntegration(integration domain.GitHubIntegration) string {
	return strings.TrimSpace(integration.DefaultBranch)
}

func initializeGitRepositoryInPlace(
	ctx context.Context,
	gitPath string,
	targetPath string,
	repositoryURL string,
	branch string,
	token string,
	postFetchScript string,
) error {
	if err := clearDirectoryContents(targetPath); err != nil {
		return err
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "init"); err != nil {
		return err
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "remote", "get-url", "origin"); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such remote") {
			if _, err := runGitCommand(ctx, gitPath, targetPath, token, "remote", "add", "origin", repositoryURL); err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if _, err := runGitCommand(ctx, gitPath, targetPath, token, "remote", "set-url", "origin", repositoryURL); err != nil {
			return err
		}
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "fetch", "--depth", "1", "origin", branch); err != nil {
		return err
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "checkout", "--force", "-B", branch, "origin/"+branch); err != nil {
		return err
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "reset", "--hard", "origin/"+branch); err != nil {
		return err
	}
	if _, err := runGitCommand(ctx, gitPath, targetPath, token, "clean", "-fd"); err != nil {
		return err
	}
	if err := runGitHubPostFetchScript(ctx, targetPath, postFetchScript); err != nil {
		return err
	}

	return nil
}

func runGitHubPostFetchScript(ctx context.Context, targetPath string, script string) error {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil
	}

	commandName, commandArgs := gitHubShellCommand(script)
	cmd := exec.CommandContext(ctx, commandName, commandArgs...)
	cmd.Dir = targetPath

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return errors.New("after fetch script timed out")
		case errors.Is(ctx.Err(), context.Canceled):
			return errors.New("after fetch script was canceled")
		case message != "":
			return fmt.Errorf("after fetch script failed: %s", message)
		default:
			return fmt.Errorf("after fetch script failed: %w", err)
		}
	}

	return nil
}

func gitHubShellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd.exe", []string{"/C", command}
	}

	return "/bin/sh", []string{"-lc", command}
}

func clearDirectoryContents(targetPath string) error {
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return fmt.Errorf("read target directory: %w", err)
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(targetPath, entry.Name())); err != nil {
			return fmt.Errorf("remove target entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}

func runGitCommand(
	ctx context.Context,
	gitPath string,
	dir string,
	token string,
	args ...string,
) (string, error) {
	commandArgs := make([]string, 0, len(args)+2)
	if strings.TrimSpace(token) != "" {
		authValue := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + strings.TrimSpace(token)))
		commandArgs = append(commandArgs, "-c", "http.extraHeader=Authorization: Basic "+authValue)
		commandArgs = append(commandArgs, "-c", buildGitHubTokenRewriteConfig(strings.TrimSpace(token)))
	}
	commandArgs = append(commandArgs, args...)

	cmd := exec.CommandContext(ctx, gitPath, commandArgs...)
	cmd.Dir = dir

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return "", errors.New("git command timed out")
		case errors.Is(ctx.Err(), context.Canceled):
			return "", errors.New("git command was canceled")
		case message != "":
			return "", errors.New(message)
		default:
			return "", fmt.Errorf("run git command %q: %w", strings.Join(args, " "), err)
		}
	}

	return strings.TrimSpace(output.String()), nil
}

func buildGitHubTokenRewriteConfig(token string) string {
	escapedToken := strings.ReplaceAll(token, "@", "%40")
	return fmt.Sprintf("url.https://x-access-token:%s@github.com/.insteadOf=https://github.com/", escapedToken)
}

func verifyGitHubWebhookSignature(secret string, body []byte, signature string) bool {
	if secret == "" || len(body) == 0 {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(strings.TrimSpace(signature)))
}
