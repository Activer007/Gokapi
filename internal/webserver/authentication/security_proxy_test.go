//go:build test

package authentication

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/logging"
	"github.com/Activer007/Gokapi/internal/models"
)

func TestSEC013HeaderAuthenticationPeer(t *testing.T) {
	oldAuth := authSettings
	t.Cleanup(func() { authSettings = oldAuth; logging.Init(configuration.Get().DataDir) })
	t.Setenv("GOKAPI_TRUSTED_PROXIES", "127.0.0.1,::1,10.0.0.2,172.18.0.2,2001:db8:1::/64")
	t.Setenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY", "true")
	logging.Init(t.TempDir())
	Init(models.AuthenticationConfig{Method: models.AuthenticationHeader, HeaderKey: "Remote-User", OnlyRegisteredUsers: true})
	for _, tc := range []struct {
		name, peer string
		values     []string
		want       bool
	}{
		{"direct", "198.51.100.9:9000", []string{"user"}, false},
		{"trusted", "127.0.0.1:9000", []string{"user"}, true},
		{"missing", "127.0.0.1:9000", nil, false},
		{"duplicate", "127.0.0.1:9000", []string{"user", "test"}, false},
		{"unregistered", "127.0.0.1:9000", []string{"synthetic-unregistered"}, false},
		{"ipv6-loopback", "[::1]:9000", []string{"user"}, true},
		{"ipv6-cidr", "[2001:db8:1::2]:9000", []string{"user"}, true},
		{"ipv6-direct", "[2001:db8:2::9]:9000", []string{"user"}, false},
		{"docker-proxy", "172.18.0.2:9000", []string{"user"}, true},
		{"docker-neighbor", "172.18.0.3:9000", []string{"user"}, false},
		{"invalid-peer", "invalid", []string{"user"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/admin", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.values {
				r.Header.Add("Remote-User", v)
			}
			// A claimed proxy address in forwarding headers is never the peer.
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("CF-Connecting-IP", "127.0.0.1")
			user, ok, _ := IsAuthenticated(httptest.NewRecorder(), r)
			if ok != tc.want {
				t.Fatalf("authenticated=%v, want %v", ok, tc.want)
			}
			if ok && user.Id != 7 {
				t.Fatal("wrong fixture identity")
			}
		})
	}
	// With automatic provisioning enabled, an untrusted peer must not create
	// even a normal account as a side effect of a rejected request.
	authSettings.OnlyRegisteredUsers = false
	before := len(database.GetAllUsers())
	r := httptest.NewRequest("GET", "/admin", nil)
	r.RemoteAddr = "198.51.100.9:9000"
	r.Header.Set("Remote-User", "synthetic-sec013-new-user")
	_, ok, _ := IsAuthenticated(httptest.NewRecorder(), r)
	if ok || len(database.GetAllUsers()) != before {
		t.Fatal("untrusted peer provisioned an account")
	}
}

func TestSEC013HeaderAuthenticationHTTP(t *testing.T) {
	oldAuth := authSettings
	t.Cleanup(func() { authSettings = oldAuth; logging.Init(configuration.Get().DataDir) })
	t.Setenv("GOKAPI_DISABLE_DOCKER_TRUSTED_PROXY", "true")
	t.Setenv("GOKAPI_USE_CLOUDFLARE", "true")
	Init(models.AuthenticationConfig{Method: models.AuthenticationHeader, HeaderKey: "Remote-User", OnlyRegisteredUsers: true})
	for _, trusted := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "configured-proxy"}[trusted], func(t *testing.T) {
			proxyIP := "192.0.2.10"
			if trusted {
				proxyIP = "127.0.0.1"
			}
			t.Setenv("GOKAPI_TRUSTED_PROXIES", proxyIP)
			logging.Init(t.TempDir())
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Fixture-Client-IP", logging.GetIpAddress(r))
				user, ok, _ := IsAuthenticated(w, r)
				if !ok || user.Id != 7 {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Remote-User", "user")
			req.Header.Set("X-Forwarded-For", "192.0.2.10")
			req.Header.Set("CF-Connecting-IP", "203.0.113.5")
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			wantStatus, wantIP := http.StatusUnauthorized, "127.0.0.1"
			if trusted {
				wantStatus, wantIP = http.StatusNoContent, "203.0.113.5"
			}
			if resp.StatusCode != wantStatus || resp.Header.Get("X-Fixture-Client-IP") != wantIP {
				t.Fatalf("status=%d clientIP=%s", resp.StatusCode, resp.Header.Get("X-Fixture-Client-IP"))
			}
		})
	}
}
