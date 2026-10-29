//go:build test

package logging

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Activer007/SupaShare/internal/environment"
)

func TestSEC013ProxyIPBoundary(t *testing.T) {
	oldIPs, oldCIDRs, oldCF := parsedTrustedIPs, parsedTrustedCIDRs, useCloudflare
	t.Cleanup(func() { parsedTrustedIPs, parsedTrustedCIDRs, useCloudflare = oldIPs, oldCIDRs, oldCF })
	parseTrustedProxies([]string{"127.0.0.1", "::1", "10.0.0.2", "172.18.0.2", "2001:db8:1::/64"}, false)
	for _, tc := range []struct {
		name, peer, want string
		cf               bool
		xff, xri, cfi    []string
	}{
		{name: "direct-cf", peer: "198.51.100.9:9000", cf: true, cfi: []string{"203.0.113.5"}, want: "198.51.100.9"},
		{name: "direct-forwarded", peer: "198.51.100.9:9000", xff: []string{"127.0.0.1"}, xri: []string{"10.0.0.2"}, want: "198.51.100.9"},
		{name: "trusted-cf", peer: "127.0.0.1:9000", cf: true, cfi: []string{"203.0.113.5"}, want: "203.0.113.5"},
		{name: "invalid-cf", peer: "127.0.0.1:9000", cf: true, cfi: []string{"not-an-ip"}, want: "127.0.0.1"},
		{name: "duplicate-cf", peer: "127.0.0.1:9000", cf: true, cfi: []string{"203.0.113.5", "203.0.113.6"}, want: "127.0.0.1"},
		{name: "disabled-cf", peer: "127.0.0.1:9000", cfi: []string{"203.0.113.5"}, want: "127.0.0.1"},
		{name: "trusted-real-ip", peer: "127.0.0.1:9000", xri: []string{"203.0.113.5"}, want: "203.0.113.5"},
		{name: "duplicate-real-ip", peer: "127.0.0.1:9000", xri: []string{"203.0.113.5", "203.0.113.6"}, want: "127.0.0.1"},
		{name: "multihop", peer: "127.0.0.1:9000", xff: []string{"203.0.113.5, 10.0.0.2"}, want: "203.0.113.5"},
		{name: "untrusted-boundary", peer: "127.0.0.1:9000", xff: []string{"192.0.2.99, 198.51.100.9, 10.0.0.2"}, want: "198.51.100.9"},
		{name: "multiple-lines", peer: "127.0.0.1:9000", xff: []string{"192.0.2.99", "198.51.100.9, 10.0.0.2"}, want: "198.51.100.9"},
		{name: "malformed-hop", peer: "127.0.0.1:9000", xff: []string{"203.0.113.5, invalid"}, xri: []string{"192.0.2.99"}, want: "127.0.0.1"},
		{name: "empty-hop", peer: "127.0.0.1:9000", xff: []string{"203.0.113.5, "}, want: "127.0.0.1"},
		{name: "all-trusted-chain", peer: "127.0.0.1:9000", xff: []string{"10.0.0.2"}, xri: []string{"192.0.2.99"}, want: "10.0.0.2"},
		{name: "ipv6-proxy", peer: "[2001:db8:1::2]:9000", xff: []string{"2001:db8:2::5, ::1"}, want: "2001:db8:2::5"},
		{name: "ipv6-direct", peer: "[2001:db8:2::9]:9000", cf: true, cfi: []string{"203.0.113.5"}, want: "2001:db8:2::9"},
		{name: "docker-explicit-peer", peer: "172.18.0.2:9000", xff: []string{"203.0.113.5"}, want: "203.0.113.5"},
		{name: "docker-other-peer", peer: "172.18.0.3:9000", cf: true, cfi: []string{"203.0.113.5"}, want: "172.18.0.3"},
		{name: "invalid-peer", peer: "invalid", cf: true, cfi: []string{"203.0.113.5"}, want: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useCloudflare = tc.cf
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			for _, v := range tc.xri {
				r.Header.Add("X-Real-IP", v)
			}
			for _, v := range tc.cfi {
				r.Header.Add("CF-Connecting-IP", v)
			}
			if got := GetIpAddress(r); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSEC013DockerProxyConfiguration(t *testing.T) {
	oldIPs, oldCIDRs, oldCF, oldPath, oldStdout := parsedTrustedIPs, parsedTrustedCIDRs, useCloudflare, logPath, outputToStdout
	oldDocker := environment.IsDocker
	t.Cleanup(func() {
		parsedTrustedIPs, parsedTrustedCIDRs, useCloudflare, logPath, outputToStdout = oldIPs, oldCIDRs, oldCF, oldPath, oldStdout
		environment.IsDocker = oldDocker
	})
	environment.IsDocker = "true"
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY", "")
	if err := os.Unsetenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOKAPI_TRUSTED_PROXIES", "127.0.0.1")
	Init(t.TempDir())
	if len(parsedTrustedCIDRs) != 0 {
		t.Fatal("Docker startup implicitly trusts a network")
	}
	for _, tc := range []struct {
		configured, peer string
		want             bool
	}{
		{"172.18.0.2", "172.18.0.2:9000", true},
		{"172.18.0.2", "172.18.0.3:9000", false},
		{"172.18.0.0/24", "172.18.0.3:9000", true},
		{"172.18.0.0/24", "172.19.0.3:9000", false},
		{"invalid", "172.18.0.2:9000", false},
	} {
		t.Setenv("GOKAPI_TRUSTED_PROXIES", tc.configured)
		Init(t.TempDir())
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.peer
		if got := IsTrustedProxy(r); got != tc.want {
			t.Fatalf("config=%s peer=%s trusted=%v", tc.configured, tc.peer, got)
		}
	}
}
