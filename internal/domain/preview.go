package domain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	xdraw "golang.org/x/image/draw"

	"flowpanel/internal/config"
)

const (
	// Generate previews at 4x the rendered panel size to keep them crisp when scaled down in the panel UI.
	defaultDomainPreviewWidth         = 1120
	defaultDomainPreviewHeight        = 840
	defaultDomainPreviewCaptureWidth  = 1440
	defaultDomainPreviewCaptureHeight = 1080
	defaultDomainPreviewCaptureDelay  = 750 * time.Millisecond
	defaultDomainPreviewTimeout       = 30 * time.Second
	defaultDomainPreviewTTL           = 7 * 24 * time.Hour
	domainPreviewMaxBytes             = 5 << 20
)

type PreviewGenerator interface {
	Capture(ctx context.Context, targetURL string) ([]byte, error)
}

type chromedpPreviewGenerator struct {
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
	if value := strings.TrimSpace(os.Getenv("FLOWPANEL_DOMAIN_PREVIEW_CHROME_PATH")); value != "" {
		return value
	}

	for _, candidate := range []string{"chromium", "google-chrome", "chromium-browser"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}

	return ""
}

func defaultPreviewGenerator() PreviewGenerator {
	return &chromedpPreviewGenerator{
		execPath: defaultPreviewChromePath(),
	}
}

func (g *chromedpPreviewGenerator) Capture(ctx context.Context, targetURL string) ([]byte, error) {
	screenshot, err := captureWebsiteScreenshot(ctx, g.execPath, targetURL)
	if err != nil {
		return nil, err
	}

	return thumbnailPreviewImage(screenshot)
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
	normalizedHostname := normalizeHostname(hostname)

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, record := range s.records {
		if record.Hostname == normalizedHostname {
			return record, true
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
