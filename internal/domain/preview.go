package domain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	xdraw "golang.org/x/image/draw"

	"flowpanel/internal/config"
)

const (
	// Generate previews at 4x the rendered panel size to keep them crisp when scaled down in the panel UI.
	defaultDomainPreviewWidth          = 1120
	defaultDomainPreviewHeight         = 840
	defaultDomainPreviewCaptureWidth   = 1440
	defaultDomainPreviewCaptureHeight  = 1080
	defaultDomainPreviewCaptureDelay   = 750 * time.Millisecond
	defaultDomainPreviewTimeout        = 30 * time.Second
	defaultDomainPreviewTTL            = 7 * 24 * time.Hour
	defaultDomainPreviewInstallTimeout = 10 * time.Minute
	domainPreviewMaxBytes              = 5 << 20
	previewChromeDebURL                = "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"
	previewChromeRPMURL                = "https://dl.google.com/linux/direct/google-chrome-stable_current_x86_64.rpm"
)

type PreviewGenerator interface {
	Capture(ctx context.Context, targetURL string) ([]byte, error)
}

type chromedpPreviewGenerator struct {
	mu       sync.Mutex
	execPath string
}

func defaultPreviewCachePath() string {
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_DOMAIN_PREVIEW_CACHE_PATH")); value != "" {
		return value
	}

	return filepath.Join(config.CachePath(), "domains")
}

func defaultPreviewTTL() time.Duration {
	value := strings.TrimSpace(os.Getenv("FLOWPANEL_DOMAIN_PREVIEW_TTL"))
	if value == "" {
		return defaultDomainPreviewTTL
	}

	ttl, err := time.ParseDuration(value)
	if err != nil || ttl <= 0 {
		return defaultDomainPreviewTTL
	}

	return ttl
}

func defaultPreviewChromePath() string {
	if resolved, ok := lookupPreviewChromePath(); ok {
		return resolved
	}

	return ""
}

func defaultPreviewGenerator() PreviewGenerator {
	return &chromedpPreviewGenerator{
		execPath: defaultPreviewChromePath(),
	}
}

func (g *chromedpPreviewGenerator) Capture(ctx context.Context, targetURL string) ([]byte, error) {
	execPath, err := g.resolveExecPath(ctx)
	if err != nil {
		return nil, err
	}

	screenshot, err := captureWebsiteScreenshot(ctx, execPath, targetURL)
	if err != nil {
		return nil, err
	}

	return thumbnailPreviewImage(screenshot)
}

func (g *chromedpPreviewGenerator) resolveExecPath(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if resolved, ok := lookupPreviewChromeCandidate(g.execPath); ok {
		g.execPath = resolved
		return resolved, nil
	}

	if resolved, ok := lookupPreviewChromePath(); ok {
		g.execPath = resolved
		return resolved, nil
	}

	if err := installPreviewChrome(ctx); err != nil {
		return "", err
	}

	if resolved, ok := lookupPreviewChromePath(); ok {
		g.execPath = resolved
		return resolved, nil
	}

	return "", errors.New("no supported Chrome or Chromium binary found after installation")
}

func lookupPreviewChromePath() (string, bool) {
	for _, candidate := range previewChromeCandidates() {
		if resolved, ok := lookupPreviewChromeCandidate(candidate); ok {
			return resolved, true
		}
	}

	return "", false
}

func previewChromeCandidates() []string {
	candidates := make([]string, 0, 16)
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_DOMAIN_PREVIEW_CHROME_PATH")); value != "" {
		candidates = append(candidates, value)
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		)
	case "windows":
		candidates = append(candidates,
			"chrome",
			"chrome.exe",
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			filepath.Join(os.Getenv("USERPROFILE"), `AppData\Local\Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("USERPROFILE"), `AppData\Local\Chromium\Application\chrome.exe`),
		)
	default:
		candidates = append(candidates,
			"headless_shell",
			"headless-shell",
			"chromium",
			"chromium-browser",
			"google-chrome",
			"google-chrome-stable",
			"google-chrome-beta",
			"google-chrome-unstable",
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/local/bin/chrome",
			"/snap/bin/chromium",
			"chrome",
		)
	}

	return candidates
}

func lookupPreviewChromeCandidate(candidate string) (string, bool) {
	name := strings.TrimSpace(candidate)
	if name == "" {
		return "", false
	}

	if resolved, err := exec.LookPath(name); err == nil {
		return resolved, true
	}

	searchDirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/usr/sbin",
		"/snap/bin",
	}
	if filepath.IsAbs(name) {
		searchDirs = nil
	}

	for _, dir := range searchDirs {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		return path, true
	}

	info, err := os.Stat(name)
	if err != nil || info.IsDir() {
		return "", false
	}

	return name, true
}

func installPreviewChrome(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		return errors.New("no supported Chrome or Chromium binary found")
	}

	installCtx := ctx
	if installCtx == nil {
		installCtx = context.Background()
	}
	if _, ok := installCtx.Deadline(); !ok {
		var cancel context.CancelFunc
		installCtx, cancel = context.WithTimeout(installCtx, defaultDomainPreviewInstallTimeout)
		defer cancel()
	}

	switch {
	case hasPreviewCommand("apt-get"):
		return installPreviewChromeWithAPT(installCtx)
	case hasPreviewCommand("dnf"):
		return installPreviewChromeWithRPM(installCtx, "dnf")
	case hasPreviewCommand("yum"):
		return installPreviewChromeWithRPM(installCtx, "yum")
	default:
		return errors.New("automatic Chrome installation requires apt-get, dnf, or yum")
	}
}

func installPreviewChromeWithAPT(ctx context.Context) error {
	aptPath, _ := lookupPreviewChromeCandidate("apt-get")
	if _, err := runPreviewCommand(ctx, aptPath, "update"); err != nil {
		return fmt.Errorf("apt-get update: %w", err)
	}

	packages := []string{"chromium"}
	if isUbuntuLikeLinux() {
		packages = append([]string{"chromium-browser"}, packages...)
	}

	errs := make([]error, 0, len(packages)+1)
	for _, packageName := range packages {
		if _, err := runPreviewCommand(ctx, aptPath, "install", "-y", packageName); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Errorf("apt-get install %s: %w", packageName, err))
		}
	}

	if runtime.GOARCH == "amd64" {
		if err := installDownloadedPreviewChromePackage(ctx, aptPath, previewChromeDebURL, ".deb"); err == nil {
			return nil
		} else {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func installPreviewChromeWithRPM(ctx context.Context, manager string) error {
	managerPath, _ := lookupPreviewChromeCandidate(manager)
	if _, err := runPreviewCommand(ctx, managerPath, "install", "-y", "chromium"); err == nil {
		return nil
	} else if runtime.GOARCH != "amd64" {
		return fmt.Errorf("%s install chromium: %w", manager, err)
	}

	if err := installDownloadedPreviewChromePackage(ctx, managerPath, previewChromeRPMURL, ".rpm"); err != nil {
		return err
	}

	return nil
}

func installDownloadedPreviewChromePackage(ctx context.Context, managerPath string, downloadURL string, extension string) error {
	packagePath, err := downloadPreviewChromePackage(ctx, downloadURL, extension)
	if err != nil {
		return err
	}
	defer os.Remove(packagePath)

	switch extension {
	case ".deb":
		dpkgPath, ok := lookupPreviewChromeCandidate("dpkg")
		if !ok {
			return errors.New("dpkg is not available")
		}

		if _, err := runPreviewCommand(ctx, dpkgPath, "-i", packagePath); err == nil {
			return nil
		}

		if _, err := runPreviewCommand(ctx, managerPath, "install", "-f", "-y"); err != nil {
			return fmt.Errorf("apt-get install downloaded Chrome package: %w", err)
		}
		return nil
	case ".rpm":
		if _, err := runPreviewCommand(ctx, managerPath, "install", "-y", packagePath); err != nil {
			return fmt.Errorf("%s install downloaded Chrome package: %w", filepath.Base(managerPath), err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported preview Chrome package extension %q", extension)
	}
}

func downloadPreviewChromePackage(ctx context.Context, downloadURL string, extension string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("prepare Chrome download request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("download Chrome package: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("download Chrome package: unexpected status %s", response.Status)
	}

	file, err := os.CreateTemp("", "flowpanel-chrome-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create temporary Chrome package file: %w", err)
	}

	if _, err := io.Copy(file, response.Body); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", fmt.Errorf("write Chrome package: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(file.Name())
		return "", fmt.Errorf("close Chrome package file: %w", err)
	}

	return file.Name(), nil
}

func hasPreviewCommand(name string) bool {
	_, ok := lookupPreviewChromeCandidate(name)
	return ok
}

type previewOSReleaseInfo struct {
	id     string
	idLike string
}

func isUbuntuLikeLinux() bool {
	info := parsePreviewOSReleaseFile("/etc/os-release")
	if info.id == "ubuntu" {
		return true
	}

	for _, item := range strings.Fields(info.idLike) {
		if item == "ubuntu" {
			return true
		}
	}

	return false
}

func parsePreviewOSReleaseFile(path string) previewOSReleaseInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return previewOSReleaseInfo{}
	}

	var info previewOSReleaseInfo
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "ID":
			info.id = strings.ToLower(value)
		case "ID_LIKE":
			info.idLike = strings.ToLower(value)
		}
	}

	return info
}

func runPreviewCommand(ctx context.Context, name string, args ...string) (string, error) {
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}

	cmd := exec.CommandContext(runCtx, name, args...)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	combinedOutput := strings.TrimSpace(output.String())
	if err == nil {
		return combinedOutput, nil
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return combinedOutput, fmt.Errorf("%s timed out", filepath.Base(name))
	}
	if errors.Is(runCtx.Err(), context.Canceled) {
		return combinedOutput, fmt.Errorf("%s was canceled", filepath.Base(name))
	}
	if combinedOutput == "" {
		return combinedOutput, fmt.Errorf("%s failed: %w", filepath.Base(name), err)
	}

	return combinedOutput, fmt.Errorf("%s failed: %s", filepath.Base(name), combinedOutput)
}

func captureWebsiteScreenshot(parent context.Context, execPath string, targetURL string) ([]byte, error) {
	parsedURL, err := url.Parse(targetURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		if err == nil {
			err = errors.New("missing URL scheme or host")
		}
		return nil, fmt.Errorf("parse preview target URL: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(parent, defaultDomainPreviewTimeout)
	defer cancel()

	allocatorOptions := append(
		[]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...,
	)
	allocatorOptions = append(
		allocatorOptions,
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("mute-audio", true),
	)
	if execPath != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ExecPath(execPath))
	}

	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(timeoutCtx, allocatorOptions...)
	defer allocatorCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()

	var screenshot []byte
	if err := chromedp.Run(
		browserCtx,
		chromedp.EmulateViewport(defaultDomainPreviewCaptureWidth, defaultDomainPreviewCaptureHeight),
		chromedp.Navigate(parsedURL.String()),
		chromedp.Sleep(defaultDomainPreviewCaptureDelay),
		chromedp.CaptureScreenshot(&screenshot),
	); err != nil {
		return nil, fmt.Errorf("capture preview screenshot: %w", err)
	}

	if len(screenshot) == 0 {
		return nil, errors.New("empty screenshot image")
	}

	return screenshot, nil
}

func (s *Service) SetPreviewGenerator(generator PreviewGenerator) {
	if generator == nil {
		generator = defaultPreviewGenerator()
	}

	s.previewGenerator = generator
}

func (s *Service) FindByHostname(hostname string) (Record, bool) {
	if s == nil {
		return Record{}, false
	}

	normalizedHostname := normalizeHostname(hostname)

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, record := range s.records {
		if record.Hostname == normalizedHostname {
			return s.withTransientFields(record), true
		}
	}

	return Record{}, false
}

func (s *Service) FindByID(id string) (Record, bool) {
	if s == nil {
		return Record{}, false
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return Record{}, false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, record := range s.records {
		if record.ID == trimmedID {
			return s.withTransientFields(record), true
		}
	}

	return Record{}, false
}

func (s *Service) EnsurePreview(ctx context.Context, hostname string) (string, error) {
	return s.ensurePreview(ctx, hostname, false)
}

func (s *Service) RefreshPreview(ctx context.Context, hostname string) (string, error) {
	return s.ensurePreview(ctx, hostname, true)
}

func (s *Service) InvalidatePreview(hostname string) error {
	normalizedHostname := normalizeHostname(hostname)
	if normalizedHostname == "" {
		return ErrNotFound
	}

	cachePath := s.previewPath(normalizedHostname)
	if err := os.Remove(cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete domain preview cache: %w", err)
	}

	return nil
}

func (s *Service) ensurePreview(ctx context.Context, hostname string, force bool) (string, error) {
	normalizedHostname := normalizeHostname(hostname)
	if normalizedHostname == "" {
		return "", ErrNotFound
	}

	if _, ok := s.FindByHostname(normalizedHostname); !ok {
		return "", ErrNotFound
	}

	if !force {
		if cachedPath, ok := s.cachedPreviewPath(normalizedHostname, false); ok {
			return cachedPath, nil
		}
	}

	s.previewMu.Lock()
	defer s.previewMu.Unlock()

	if !force {
		if cachedPath, ok := s.cachedPreviewPath(normalizedHostname, false); ok {
			return cachedPath, nil
		}
	}

	cachePath := s.previewPath(normalizedHostname)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", fmt.Errorf("create domain preview cache path: %w", err)
	}

	if err := s.fetchAndCachePreview(ctx, normalizedHostname, cachePath, force); err != nil {
		if cachedPath, ok := s.cachedPreviewPath(normalizedHostname, true); ok {
			return cachedPath, nil
		}

		return "", err
	}

	return cachePath, nil
}

func (s *Service) cachedPreviewPath(hostname string, allowStale bool) (string, bool) {
	cachePath := s.previewPath(hostname)

	info, err := os.Stat(cachePath)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return "", false
	}
	if allowStale {
		return cachePath, true
	}
	if s.previewTTL <= 0 {
		return cachePath, true
	}
	if s.now().Sub(info.ModTime()) <= s.previewTTL {
		return cachePath, true
	}

	return "", false
}

func (s *Service) previewPath(hostname string) string {
	return filepath.Join(s.previewCachePath, hostname+".png")
}

func (s *Service) fetchAndCachePreview(ctx context.Context, hostname string, cachePath string, force bool) error {
	if s.previewGenerator == nil {
		return errors.New("preview generator is not configured")
	}

	var generationErrs []error

	for _, targetURL := range previewTargetURLs(hostname, force, s.now()) {
		data, err := s.previewGenerator.Capture(ctx, targetURL)
		if err != nil {
			generationErrs = append(generationErrs, fmt.Errorf("generate preview for %s from %s: %w", hostname, targetURL, err))
			continue
		}

		if err := writePreviewFile(cachePath, data); err != nil {
			return fmt.Errorf("cache preview for %s: %w", hostname, err)
		}

		return nil
	}

	return errors.Join(generationErrs...)
}

func previewTargetURLs(hostname string, force bool, now time.Time) []string {
	httpsURL := "https://" + hostname
	httpURL := "http://" + hostname
	if !force {
		return []string{httpsURL, httpURL}
	}

	return []string{
		withPreviewRefreshQuery(httpsURL, now),
		withPreviewRefreshQuery(httpURL, now),
	}
}

func withPreviewRefreshQuery(rawURL string, now time.Time) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	values := parsed.Query()
	values.Set("flowpanel_preview_refresh", fmt.Sprintf("%d", now.UnixNano()))
	parsed.RawQuery = values.Encode()

	return parsed.String()
}

func thumbnailPreviewImage(data []byte) ([]byte, error) {
	sourceImage, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot image: %w", err)
	}

	sourceBounds := sourceImage.Bounds()
	if sourceBounds.Dx() <= 0 || sourceBounds.Dy() <= 0 {
		return nil, errors.New("empty screenshot image")
	}

	scale := math.Min(
		float64(defaultDomainPreviewWidth)/float64(sourceBounds.Dx()),
		float64(defaultDomainPreviewHeight)/float64(sourceBounds.Dy()),
	)
	scaledWidth := maxInt(1, int(math.Round(float64(sourceBounds.Dx())*scale)))
	scaledHeight := maxInt(1, int(math.Round(float64(sourceBounds.Dy())*scale)))
	offsetX := (defaultDomainPreviewWidth - scaledWidth) / 2
	offsetY := (defaultDomainPreviewHeight - scaledHeight) / 2

	destination := image.NewRGBA(image.Rect(0, 0, defaultDomainPreviewWidth, defaultDomainPreviewHeight))
	xdraw.CatmullRom.Scale(
		destination,
		image.Rect(offsetX, offsetY, offsetX+scaledWidth, offsetY+scaledHeight),
		sourceImage,
		sourceBounds,
		xdraw.Over,
		nil,
	)

	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&encoded, destination); err != nil {
		return nil, fmt.Errorf("encode thumbnail image: %w", err)
	}

	thumbnail := encoded.Bytes()
	if len(thumbnail) == 0 {
		return nil, errors.New("empty thumbnail image")
	}
	if len(thumbnail) > domainPreviewMaxBytes {
		return nil, fmt.Errorf("image exceeds %d bytes", domainPreviewMaxBytes)
	}

	return thumbnail, nil
}

func writePreviewFile(cachePath string, data []byte) error {
	tempPath := cachePath + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	return nil
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}

	return b
}
