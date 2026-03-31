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

func TestAPTInstallPackages(t *testing.T) {
	want := []string{
		"php8.4-cgi",
		"php8.4-fpm",
		"php8.4-cli",
		"php8.4-common",
		"php8.4-mysql",
		"php8.4-curl",
		"php8.4-gd",
		"php8.4-intl",
		"php8.4-imagick",
		"php8.4-mbstring",
		"php8.4-xml",
		"php8.4-zip",
	}

	if len(aptPHP84Packages) != len(want) {
		t.Fatalf("len(aptPHP84Packages) = %d, want %d", len(aptPHP84Packages), len(want))
	}

	for i := range want {
		if aptPHP84Packages[i] != want[i] {
			t.Fatalf("aptPHP84Packages[%d] = %q, want %q", i, aptPHP84Packages[i], want[i])
		}
	}
}
