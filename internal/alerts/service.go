package alerts

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	deliveryInterval = 5 * time.Second
	deliveryTimeout  = 15 * time.Second
	maxAttempts      = 5
)

type Config struct {
	Enabled                bool   `json:"enabled"`
	WebhookURL             string `json:"webhook_url"`
	WebhookSecret          string `json:"webhook_secret"`
	SMTPHost               string `json:"smtp_host"`
	SMTPPort               int    `json:"smtp_port"`
	SMTPEncryption         string `json:"smtp_encryption"`
	SMTPUsername           string `json:"smtp_username"`
	SMTPPassword           string `json:"smtp_password"`
	SMTPFrom               string `json:"smtp_from"`
	SMTPRecipients         string `json:"smtp_recipients"`
	DiskWarningPercent     int    `json:"disk_warning_percent"`
	DiskCriticalPercent    int    `json:"disk_critical_percent"`
	CertificateWarningDays int    `json:"certificate_warning_days"`
	LoginFailureThreshold  int    `json:"login_failure_threshold"`
	CooldownMinutes        int    `json:"cooldown_minutes"`
	NotifyRecovery         bool   `json:"notify_recovery"`
}

type PublicConfig struct {
	Enabled                 bool   `json:"enabled"`
	WebhookURL              string `json:"webhook_url"`
	WebhookSecretConfigured bool   `json:"webhook_secret_configured"`
	SMTPHost                string `json:"smtp_host"`
	SMTPPort                int    `json:"smtp_port"`
	SMTPEncryption          string `json:"smtp_encryption"`
	SMTPUsername            string `json:"smtp_username"`
	SMTPPasswordConfigured  bool   `json:"smtp_password_configured"`
	SMTPFrom                string `json:"smtp_from"`
	SMTPRecipients          string `json:"smtp_recipients"`
	DiskWarningPercent      int    `json:"disk_warning_percent"`
	DiskCriticalPercent     int    `json:"disk_critical_percent"`
	CertificateWarningDays  int    `json:"certificate_warning_days"`
	LoginFailureThreshold   int    `json:"login_failure_threshold"`
	CooldownMinutes         int    `json:"cooldown_minutes"`
	NotifyRecovery          bool   `json:"notify_recovery"`
}

type UpdateInput = Config
type ValidationErrors map[string]string

func (v ValidationErrors) Error() string { return "alert settings validation failed" }

type TriggerInput struct {
	Key      string            `json:"key"`
	Severity string            `json:"severity"`
	Title    string            `json:"title"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type notification struct {
	Event     string       `json:"event"`
	Alert     TriggerInput `json:"alert"`
	Timestamp time.Time    `json:"timestamp"`
}

type Service struct {
	logger *zap.Logger
	store  *Store
	client *http.Client
	now    func() time.Time

	mu            sync.Mutex
	cancel        context.CancelFunc
	done          chan struct{}
	domains       func() []string
	adminCertPath string
	certificateWG sync.WaitGroup
	lastPrune     time.Time
}

func DefaultConfig() Config {
	return Config{
		SMTPPort:               587,
		SMTPEncryption:         "starttls",
		DiskWarningPercent:     85,
		DiskCriticalPercent:    95,
		CertificateWarningDays: 7,
		LoginFailureThreshold:  10,
		CooldownMinutes:        360,
		NotifyRecovery:         true,
	}
}

func NewService(logger *zap.Logger, store *Store) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		logger: logger,
		store:  store,
		client: &http.Client{Timeout: deliveryTimeout},
		now:    time.Now,
	}
}

func (s *Service) Start() {
	if s == nil || s.store == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	go s.run(ctx, done)
}

func (s *Service) SetCertificateSources(domains func() []string, adminCertPath string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.domains, s.adminCertPath = domains, strings.TrimSpace(adminCertPath)
	s.mu.Unlock()
}

func (s *Service) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	certificatesDone := make(chan struct{})
	go func() {
		s.certificateWG.Wait()
		close(certificatesDone)
	}()
	select {
	case <-certificatesDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Config(ctx context.Context) (Config, error) {
	if s == nil || s.store == nil {
		return DefaultConfig(), nil
	}
	return s.store.GetConfig(ctx)
}

func (s *Service) Ensure(ctx context.Context) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Ensure(ctx)
}

func (s *Service) PublicConfig(ctx context.Context) (PublicConfig, error) {
	config, err := s.Config(ctx)
	if err != nil {
		return PublicConfig{}, err
	}
	return publicConfig(config), nil
}

func (s *Service) UpdateConfig(ctx context.Context, input UpdateInput) (PublicConfig, error) {
	current, err := s.Config(ctx)
	if err != nil {
		return PublicConfig{}, err
	}
	input = normalizeConfig(input)
	if input.WebhookSecret == "" {
		input.WebhookSecret = current.WebhookSecret
	}
	if input.SMTPPassword == "" {
		input.SMTPPassword = current.SMTPPassword
	}
	if validation := validateConfig(input); len(validation) > 0 {
		return PublicConfig{}, validation
	}
	if err := s.store.SaveConfig(ctx, input); err != nil {
		return PublicConfig{}, err
	}
	return publicConfig(input), nil
}

func (s *Service) Trigger(ctx context.Context, input TriggerInput) error {
	if s == nil || s.store == nil {
		return nil
	}
	input = normalizeTrigger(input)
	if input.Key == "" {
		return errors.New("alert key is required")
	}
	config, err := s.Config(ctx)
	if err != nil || !config.Enabled {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	state, found, err := s.store.GetState(ctx, input.Key)
	if err != nil {
		return err
	}
	shouldNotify := !found || state.Status != "firing" || now.Sub(state.LastNotifiedAt) >= time.Duration(config.CooldownMinutes)*time.Minute
	if !found || state.Status != "firing" {
		state.FirstSeenAt, state.OccurrenceCount = now, 0
	}
	state.Key, state.Severity, state.Title, state.Message, state.Status = input.Key, input.Severity, input.Title, input.Message, "firing"
	state.LastSeenAt, state.OccurrenceCount = now, state.OccurrenceCount+1
	if shouldNotify {
		state.LastNotifiedAt = now
	}
	if err := s.store.SaveState(ctx, state); err != nil {
		return err
	}
	if shouldNotify {
		return s.enqueue(ctx, config, "alert.firing", input)
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, key string) error {
	if s == nil || s.store == nil || strings.TrimSpace(key) == "" {
		return nil
	}
	config, err := s.Config(ctx)
	if err != nil || !config.Enabled {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, found, err := s.store.GetState(ctx, strings.TrimSpace(key))
	if err != nil || !found || state.Status != "firing" {
		return err
	}
	state.Status, state.LastSeenAt = "resolved", s.now().UTC()
	if err := s.store.SaveState(ctx, state); err != nil {
		return err
	}
	if !config.NotifyRecovery {
		return nil
	}
	return s.enqueue(ctx, config, "alert.resolved", TriggerInput{
		Key: state.Key, Severity: "info", Title: state.Title + " resolved", Message: "The alert condition has recovered.",
	})
}

func (s *Service) SendTest(ctx context.Context) error {
	config, err := s.Config(ctx)
	if err != nil {
		return err
	}
	if !config.Enabled {
		return ValidationErrors{"enabled": "Enable notifications before sending a test."}
	}
	if validation := validateConfig(config); len(validation) > 0 {
		return validation
	}
	if !hasChannel(config) {
		return ValidationErrors{"channels": "Configure a webhook or SMTP recipient first."}
	}
	payload, err := json.Marshal(notification{Event: "notification.test", Alert: TriggerInput{
		Key: "notification:test", Severity: "info", Title: "FlowPanel test notification", Message: "Your alert channel is configured correctly.",
	}, Timestamp: s.now().UTC()})
	if err != nil {
		return fmt.Errorf("encode test notification: %w", err)
	}
	channels := []string{}
	if config.WebhookURL != "" {
		channels = append(channels, "webhook")
	}
	if config.SMTPHost != "" && config.SMTPRecipients != "" {
		channels = append(channels, "email")
	}
	for _, channel := range channels {
		if err := s.send(ctx, config, Delivery{Channel: channel, Payload: payload}); err != nil {
			return fmt.Errorf("%s test delivery failed: %w", channel, err)
		}
	}
	return nil
}

func (s *Service) enqueue(ctx context.Context, config Config, event string, input TriggerInput) error {
	payload, err := json.Marshal(notification{Event: event, Alert: input, Timestamp: s.now().UTC()})
	if err != nil {
		return fmt.Errorf("encode alert notification: %w", err)
	}
	channels := make([]string, 0, 2)
	if config.WebhookURL != "" {
		channels = append(channels, "webhook")
	}
	if config.SMTPHost != "" && config.SMTPRecipients != "" {
		channels = append(channels, "email")
	}
	for _, channel := range channels {
		id := fmt.Sprintf("notification-%d-%s", s.now().UnixNano(), channel)
		if err := s.store.Enqueue(ctx, Delivery{ID: id, AlertKey: input.Key, Channel: channel, Payload: payload}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(deliveryInterval)
	certificateTicker := time.NewTicker(12 * time.Hour)
	defer ticker.Stop()
	defer certificateTicker.Stop()
	s.deliver(ctx)
	s.launchCertificateCheck(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.deliver(ctx)
		case <-certificateTicker.C:
			s.launchCertificateCheck(ctx)
		}
	}
}

func (s *Service) launchCertificateCheck(ctx context.Context) {
	s.certificateWG.Add(1)
	go func() {
		defer s.certificateWG.Done()
		s.checkCertificates(ctx)
	}()
}

func (s *Service) deliver(ctx context.Context) {
	config, err := s.Config(ctx)
	if err != nil || !config.Enabled {
		return
	}
	if now := s.now().UTC(); s.lastPrune.IsZero() || now.Sub(s.lastPrune) >= 24*time.Hour {
		if err := s.store.PruneDeliveries(ctx, now.Add(-30*24*time.Hour)); err != nil {
			s.logger.Error("prune alert deliveries failed", zap.Error(err))
		} else {
			s.lastPrune = now
		}
	}
	deliveries, err := s.store.Due(ctx, 20)
	if err != nil {
		s.logger.Error("load alert deliveries failed", zap.Error(err))
		return
	}
	for _, delivery := range deliveries {
		deliveryCtx, cancel := context.WithTimeout(ctx, deliveryTimeout)
		err := s.send(deliveryCtx, config, delivery)
		cancel()
		if err == nil {
			if markErr := s.store.MarkSent(ctx, delivery.ID); markErr != nil {
				s.logger.Error("mark alert delivery sent failed", zap.Error(markErr))
			}
			continue
		}
		attempt := delivery.Attempts + 1
		delay := time.Duration(1<<min(attempt-1, 6)) * time.Minute
		if markErr := s.store.MarkFailed(ctx, delivery, s.now().Add(delay), truncateError(err), attempt >= maxAttempts); markErr != nil {
			s.logger.Error("record alert delivery failure failed", zap.Error(markErr))
		}
		s.logger.Warn("alert delivery failed", zap.String("channel", delivery.Channel), zap.Int("attempt", attempt), zap.Error(err))
	}
}

func (s *Service) send(ctx context.Context, config Config, delivery Delivery) error {
	switch delivery.Channel {
	case "webhook":
		return s.sendWebhook(ctx, config, delivery.Payload)
	case "email":
		return sendEmail(ctx, config, delivery.Payload)
	default:
		return fmt.Errorf("unsupported notification channel %q", delivery.Channel)
	}
}

func (s *Service) sendWebhook(ctx context.Context, config Config, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.WebhookURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "FlowPanel-Alerts/1")
	if config.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(config.WebhookSecret))
		_, _ = mac.Write(payload)
		req.Header.Set("X-FlowPanel-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	response, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("webhook returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func sendEmail(ctx context.Context, config Config, payload []byte) error {
	var event notification
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	address := net.JoinHostPort(config.SMTPHost, strconv.Itoa(config.SMTPPort))
	dialer := &net.Dialer{Timeout: deliveryTimeout}
	var conn net.Conn
	var err error
	if config.SMTPEncryption == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.SMTPHost})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(deliveryTimeout))
	client, err := smtp.NewClient(conn, config.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if config.SMTPEncryption == "starttls" {
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: config.SMTPHost}); err != nil {
			return err
		}
	}
	if config.SMTPUsername != "" {
		if err := client.Auth(smtp.PlainAuth("", config.SMTPUsername, config.SMTPPassword, config.SMTPHost)); err != nil {
			return err
		}
	}
	recipients := splitRecipients(config.SMTPRecipients)
	from, _ := mail.ParseAddress(config.SMTPFrom)
	if err := client.Mail(from.Address); err != nil {
		return err
	}
	for _, recipient := range recipients {
		address, _ := mail.ParseAddress(recipient)
		if err := client.Rcpt(address.Address); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("[FlowPanel] %s: %s", strings.ToUpper(event.Alert.Severity), event.Alert.Title)
	body := fmt.Sprintf("%s\r\n\r\nAlert: %s\r\nStatus: %s\r\nTime: %s\r\n", event.Alert.Message, event.Alert.Key, event.Event, event.Timestamp.Format(time.RFC3339))
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s", config.SMTPFrom, strings.Join(recipients, ", "), subject, body)
	if _, err := writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func normalizeConfig(config Config) Config {
	config.WebhookURL = strings.TrimSpace(config.WebhookURL)
	config.WebhookSecret = strings.TrimSpace(config.WebhookSecret)
	config.SMTPHost = strings.TrimSpace(config.SMTPHost)
	config.SMTPEncryption = strings.ToLower(strings.TrimSpace(config.SMTPEncryption))
	config.SMTPUsername = strings.TrimSpace(config.SMTPUsername)
	config.SMTPPassword = strings.TrimSpace(config.SMTPPassword)
	config.SMTPFrom = strings.TrimSpace(config.SMTPFrom)
	config.SMTPRecipients = strings.Join(splitRecipients(config.SMTPRecipients), ", ")
	if config.SMTPPort == 0 {
		config.SMTPPort = 587
	}
	if config.SMTPEncryption == "" {
		config.SMTPEncryption = "starttls"
	}
	return config
}

func validateConfig(config Config) ValidationErrors {
	validation := ValidationErrors{}
	if config.Enabled && !hasChannel(config) {
		validation["channels"] = "Configure a webhook URL or SMTP recipient before enabling notifications."
	}
	if config.WebhookURL != "" {
		parsed, err := url.Parse(config.WebhookURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			validation["webhook_url"] = "Enter a valid HTTP or HTTPS webhook URL."
		}
	}
	if config.SMTPHost != "" || config.SMTPRecipients != "" {
		if config.SMTPHost == "" {
			validation["smtp_host"] = "SMTP host is required."
		}
		if config.SMTPPort < 1 || config.SMTPPort > 65535 {
			validation["smtp_port"] = "SMTP port must be between 1 and 65535."
		}
		if config.SMTPEncryption != "none" && config.SMTPEncryption != "starttls" && config.SMTPEncryption != "tls" {
			validation["smtp_encryption"] = "Select none, STARTTLS, or TLS."
		}
		if _, err := mail.ParseAddress(config.SMTPFrom); err != nil {
			validation["smtp_from"] = "Enter a valid sender email address."
		}
		if len(splitRecipients(config.SMTPRecipients)) == 0 {
			validation["smtp_recipients"] = "Enter at least one recipient."
		} else {
			for _, recipient := range splitRecipients(config.SMTPRecipients) {
				if _, err := mail.ParseAddress(recipient); err != nil {
					validation["smtp_recipients"] = "Enter valid comma-separated email addresses."
					break
				}
			}
		}
	}
	if config.DiskWarningPercent < 1 || config.DiskWarningPercent >= 100 {
		validation["disk_warning_percent"] = "Disk warning threshold must be between 1 and 99."
	}
	if config.DiskCriticalPercent <= config.DiskWarningPercent || config.DiskCriticalPercent > 100 {
		validation["disk_critical_percent"] = "Critical threshold must be greater than the warning threshold and at most 100."
	}
	if config.CertificateWarningDays < 1 || config.CertificateWarningDays > 365 {
		validation["certificate_warning_days"] = "Certificate warning must be between 1 and 365 days."
	}
	if config.LoginFailureThreshold < 1 || config.LoginFailureThreshold > 10 {
		validation["login_failure_threshold"] = "Login failure threshold must be between 1 and 10."
	}
	if config.CooldownMinutes < 5 || config.CooldownMinutes > 10080 {
		validation["cooldown_minutes"] = "Cooldown must be between 5 minutes and 7 days."
	}
	return validation
}

func publicConfig(config Config) PublicConfig {
	return PublicConfig{
		Enabled: config.Enabled, WebhookURL: config.WebhookURL, WebhookSecretConfigured: config.WebhookSecret != "",
		SMTPHost: config.SMTPHost, SMTPPort: config.SMTPPort, SMTPEncryption: config.SMTPEncryption,
		SMTPUsername: config.SMTPUsername, SMTPPasswordConfigured: config.SMTPPassword != "", SMTPFrom: config.SMTPFrom, SMTPRecipients: config.SMTPRecipients,
		DiskWarningPercent: config.DiskWarningPercent, DiskCriticalPercent: config.DiskCriticalPercent,
		CertificateWarningDays: config.CertificateWarningDays, LoginFailureThreshold: config.LoginFailureThreshold,
		CooldownMinutes: config.CooldownMinutes, NotifyRecovery: config.NotifyRecovery,
	}
}

func normalizeTrigger(input TriggerInput) TriggerInput {
	input.Key = strings.TrimSpace(input.Key)
	input.Severity = strings.ToLower(strings.TrimSpace(input.Severity))
	if input.Severity != "info" && input.Severity != "warning" && input.Severity != "critical" {
		input.Severity = "warning"
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Message = strings.TrimSpace(input.Message)
	return input
}

func splitRecipients(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	recipients := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			recipients = append(recipients, part)
		}
	}
	return recipients
}

func hasChannel(config Config) bool {
	return config.WebhookURL != "" || (config.SMTPHost != "" && config.SMTPRecipients != "")
}

func truncateError(err error) string {
	message := err.Error()
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
