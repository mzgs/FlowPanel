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
		"php-fpm",
		"php-cli",
		"php-common",
		"php-opcache",
		"php-bcmath",
		"php-mysql",
		"php-curl",
		"php-gd",
		"php-intl",
		"php-imagick",
		"php-mbstring",
		"php-xml",
		"php-zip",
	}

	if len(aptPHPPackages) != len(want) {
		t.Fatalf("len(aptPHPPackages) = %d, want %d", len(aptPHPPackages), len(want))
	}

	for i := range want {
		if aptPHPPackages[i] != want[i] {
			t.Fatalf("aptPHPPackages[%d] = %q, want %q", i, aptPHPPackages[i], want[i])
		}
	}
}

func TestNormalizePHPErrorReportingValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "all",
			input: "32767",
			want:  "E_ALL",
		},
		{
			name:  "all except strict",
			input: "30719",
			want:  "E_ALL & ~E_STRICT",
		},
		{
			name:  "already symbolic",
			input: "E_ALL & ~E_NOTICE",
			want:  "E_ALL & ~E_NOTICE",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := normalizePHPErrorReportingValue(test.input)
			if got != test.want {
				t.Fatalf("normalizePHPErrorReportingValue(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
