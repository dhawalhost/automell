package proxy

import (
	"net"
	"testing"

	"github.com/dhawalhost/automell/config"
)

func testCfg() *config.Config {
	cfg, _ := config.Load()
	// Reset singleton for test isolation
	return cfg
}

func TestIsWebToolCall(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"web_fetch", true},
		{"web_search", true},
		{"bash", false},
		{"read_file", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isWebToolCall(c.name); got != c.want {
			t.Errorf("isWebToolCall(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"172.31.255.255",
		"192.168.1.1",
		"169.254.0.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"100.64.0.1",
		"224.0.0.1",
	}
	for _, addr := range private {
		ip := net.ParseIP(addr)
		if ip == nil {
			t.Fatalf("could not parse test IP %s", addr)
		}
		if !isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = false, want true", addr)
		}
	}

	public := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",                          // example.com
		"2606:2800:21f:cb07:6820:80da:af6b:8b2c", // example.com IPv6
	}
	for _, addr := range public {
		ip := net.ParseIP(addr)
		if ip == nil {
			t.Fatalf("could not parse test IP %s", addr)
		}
		if isPrivateIP(ip) {
			t.Errorf("isPrivateIP(%s) = true, want false", addr)
		}
	}
}

func TestEgressPolicy_BlocksBadScheme(t *testing.T) {
	cfg := &config.Config{
		WebFetchAllowedSchemes:       []string{"http", "https"},
		WebFetchAllowPrivateNetworks: true, // skip IP resolution for this test
	}
	err := egressPolicy("ftp://example.com/file", cfg)
	if err == nil {
		t.Error("expected error for ftp:// scheme, got nil")
	}
}

func TestEgressPolicy_AllowsPublicURL(t *testing.T) {
	cfg := &config.Config{
		WebFetchAllowedSchemes:       []string{"http", "https"},
		WebFetchAllowPrivateNetworks: false,
	}
	// Use a hostname that resolves to a public IP (skip if offline)
	err := egressPolicy("https://example.com/", cfg)
	if err != nil {
		// May fail if the test machine has no DNS — skip rather than fail
		t.Skipf("egressPolicy(example.com) returned error (possibly offline): %v", err)
	}
}

func TestEgressPolicy_BlocksLoopback(t *testing.T) {
	cfg := &config.Config{
		WebFetchAllowedSchemes:       []string{"http", "https"},
		WebFetchAllowPrivateNetworks: false,
	}
	err := egressPolicy("http://127.0.0.1/admin", cfg)
	if err == nil {
		t.Error("expected error for loopback URL, got nil")
	}
}

func TestEgressPolicy_AllowsPrivateWhenEnabled(t *testing.T) {
	cfg := &config.Config{
		WebFetchAllowedSchemes:       []string{"http", "https"},
		WebFetchAllowPrivateNetworks: true,
	}
	// Should not block when private networks are allowed
	err := egressPolicy("http://192.168.1.1/api", cfg)
	if err != nil {
		t.Errorf("expected nil for private URL when allowed, got %v", err)
	}
}
