package alerts

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

func (s *Service) checkCertificates(ctx context.Context) {
	config, err := s.Config(ctx)
	if err != nil || !config.Enabled {
		return
	}
	s.mu.Lock()
	domainSource, adminCertPath := s.domains, s.adminCertPath
	s.mu.Unlock()
	if adminCertPath != "" {
		s.checkCertificateFile(ctx, "admin", "FlowPanel admin certificate", adminCertPath, config.CertificateWarningDays)
	}
	if domainSource == nil {
		return
	}
	for _, hostname := range domainSource() {
		s.checkDomainCertificate(ctx, strings.TrimSpace(hostname), config.CertificateWarningDays)
	}
}

func (s *Service) checkCertificateFile(ctx context.Context, key, label, path string, warningDays int) {
	payload, err := os.ReadFile(path)
	if err != nil {
		_ = s.Trigger(ctx, TriggerInput{Key: "certificate:" + key, Severity: "critical", Title: label + " unavailable", Message: err.Error()})
		return
	}
	block, _ := pem.Decode(payload)
	if block == nil {
		_ = s.Trigger(ctx, TriggerInput{Key: "certificate:" + key, Severity: "critical", Title: label + " is invalid", Message: "The configured certificate file is not valid PEM."})
		return
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		_ = s.Trigger(ctx, TriggerInput{Key: "certificate:" + key, Severity: "critical", Title: label + " is invalid", Message: err.Error()})
		return
	}
	s.evaluateCertificate(ctx, "certificate:"+key, label, certificate, warningDays)
}

func (s *Service) checkDomainCertificate(ctx context.Context, hostname string, warningDays int) {
	if hostname == "" {
		return
	}
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	rawConnection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(hostname, "443"))
	if err != nil {
		_ = s.Trigger(ctx, TriggerInput{Key: "certificate:domain:" + hostname, Severity: "critical", Title: hostname + " TLS unavailable", Message: err.Error()})
		return
	}
	connection := tls.Client(rawConnection, &tls.Config{
		MinVersion: tls.VersionTLS12, ServerName: hostname, InsecureSkipVerify: true, // Verification is performed below so expired certificates can still be inspected.
	})
	key := "certificate:domain:" + hostname
	if err := connection.HandshakeContext(ctx); err != nil {
		_ = rawConnection.Close()
		_ = s.Trigger(ctx, TriggerInput{Key: key, Severity: "critical", Title: hostname + " TLS unavailable", Message: err.Error()})
		return
	}
	defer connection.Close()
	certificates := connection.ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		_ = s.Trigger(ctx, TriggerInput{Key: key, Severity: "critical", Title: hostname + " certificate unavailable", Message: "The TLS server did not return a certificate."})
		return
	}
	leaf := certificates[0]
	intermediates := x509.NewCertPool()
	for _, certificate := range certificates[1:] {
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: hostname, Intermediates: intermediates}); err != nil && time.Now().Before(leaf.NotAfter) {
		_ = s.Trigger(ctx, TriggerInput{Key: key, Severity: "critical", Title: hostname + " certificate is invalid", Message: err.Error()})
		return
	}
	s.evaluateCertificate(ctx, key, hostname+" certificate", leaf, warningDays)
}

func (s *Service) evaluateCertificate(ctx context.Context, key, label string, certificate *x509.Certificate, warningDays int) {
	remaining := time.Until(certificate.NotAfter)
	if remaining <= 0 {
		_ = s.Trigger(ctx, TriggerInput{Key: key, Severity: "critical", Title: label + " expired", Message: fmt.Sprintf("The certificate expired at %s.", certificate.NotAfter.UTC().Format(time.RFC3339))})
		return
	}
	if remaining <= time.Duration(warningDays)*24*time.Hour {
		severity := "warning"
		if remaining <= 7*24*time.Hour {
			severity = "critical"
		}
		_ = s.Trigger(ctx, TriggerInput{Key: key, Severity: severity, Title: label + " expires soon", Message: fmt.Sprintf("The certificate expires in %d days at %s.", int(remaining.Hours()/24), certificate.NotAfter.UTC().Format(time.RFC3339))})
		return
	}
	_ = s.Resolve(ctx, key)
}
