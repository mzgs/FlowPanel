package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowpanel/internal/config"
	"flowpanel/internal/tlsutil"
)

func adminTLSEnabled(cfg config.Config) bool {
	return strings.TrimSpace(cfg.AdminTLSCertFile) != ""
}

func adminHTTPClient() *http.Client {
	certPath := strings.TrimSpace(os.Getenv("FLOWPANEL_ADMIN_TLS_CERT_FILE"))
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return http.DefaultClient
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		return http.DefaultClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: tlsutil.VerificationName(certPEM),
	}
	return &http.Client{Transport: transport}
}

func ensureAdminTLSCertificate(cfg config.Config) error {
	if !adminTLSEnabled(cfg) {
		return nil
	}

	certExists, certErr := pathExistsClean(cfg.AdminTLSCertFile)
	keyExists, keyErr := pathExistsClean(cfg.AdminTLSKeyFile)
	if certErr != nil {
		return certErr
	}
	if keyErr != nil {
		return keyErr
	}
	if certExists && keyExists {
		return nil
	}
	if certExists != keyExists {
		return errors.New("admin TLS certificate and key must either both exist or both be absent")
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate admin TLS key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate admin TLS serial: %w", err)
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "FlowPanel Admin"},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     adminTLSDNSNames(),
		IPAddresses:  adminTLSIPAddresses(),
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("generate admin TLS certificate: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode admin TLS key: %w", err)
	}

	if err := writePrivateFile(cfg.AdminTLSKeyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	if err := writePrivateFile(cfg.AdminTLSCertFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		_ = os.Remove(cfg.AdminTLSKeyFile)
		return err
	}
	return nil
}

func pathExistsClean(path string) (bool, error) {
	_, err := os.Stat(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
}

func writePrivateFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create admin TLS directory: %w", err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func adminTLSDNSNames() []string {
	names := []string{"localhost"}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		names = append(names, strings.TrimSpace(hostname))
	}
	return names
}

func adminTLSIPAddresses() []net.IP {
	values := map[string]net.IP{
		"127.0.0.1": net.ParseIP("127.0.0.1"),
		"::1":       net.ParseIP("::1"),
	}
	for _, host := range []string{publicHostIP(), primaryHostIP()} {
		if ip := net.ParseIP(host); ip != nil {
			values[ip.String()] = ip
		}
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ip := addrIP(addr); ip != nil && !ip.IsUnspecified() {
				values[ip.String()] = ip
			}
		}
	}

	ips := make([]net.IP, 0, len(values))
	for _, ip := range values {
		ips = append(ips, ip)
	}
	return ips
}
