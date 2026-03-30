package phpenv

import "testing"

func TestParsePHPVersion(t *testing.T) {
	output := "PHP 8.4.11 (cli) (built: Jul 29 2025 15:30:21) (NTS)\nCopyright (c) The PHP Group\n"

	got := parsePHPVersion(output)
	want := "PHP 8.4.11 (cli) (built: Jul 29 2025 15:30:21) (NTS)"
	if got != want {
		t.Fatalf("parsePHPVersion() = %q, want %q", got, want)
	}
}

func TestParseFPMListenAddressTCP(t *testing.T) {
	output := `
[30-Mar-2026 20:49:47] NOTICE: [www]
[30-Mar-2026 20:49:47] NOTICE:  listen = 127.0.0.1:9000
`

	got := parseFPMListenAddress(output)
	if got != "127.0.0.1:9000" {
		t.Fatalf("parseFPMListenAddress() = %q, want 127.0.0.1:9000", got)
	}
}

func TestParseFPMListenAddressUnixSocket(t *testing.T) {
	output := `
[30-Mar-2026 20:49:47] NOTICE: [www]
[30-Mar-2026 20:49:47] NOTICE:  listen = /run/php/php8.4-fpm.sock
`

	got := parseFPMListenAddress(output)
	if got != "/run/php/php8.4-fpm.sock" {
		t.Fatalf("parseFPMListenAddress() = %q, want /run/php/php8.4-fpm.sock", got)
	}
}
