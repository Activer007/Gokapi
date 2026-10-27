//go:build test

package oauth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/test/securityfixture"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/sessionmanager"
)

// Real HTTP login/callback/session traffic through a reverse proxy. The cookie
// jar enforces Secure transport; SameSite is asserted separately, not emulated.
func TestSEC012CallbackThroughProxy(t *testing.T) {
	oldURL, oldAuth := configuration.Get().ServerUrl, configuration.Get().Authentication
	oldConfig, oldProvider, oldContext := config, provider, ctx
	t.Cleanup(func() {
		configuration.Get().ServerUrl, configuration.Get().Authentication = oldURL, oldAuth
		authentication.Init(oldAuth)
		config, provider, ctx = oldConfig, oldProvider, oldContext
	})
	for _, secure := range []bool{false, true} {
		name := "http-development"
		if secure {
			name = "https-proxy"
		}
		t.Run(name, func(t *testing.T) {
			idp := securityfixture.NewIDP(t, securityfixture.Identity{Subject: "synthetic-subject", Email: "sec012@example.invalid", EmailVerified: true})
			user := models.User{Id: 9013, Name: "sec012@example.invalid", UserLevel: models.UserLevelUser}
			database.SaveUser(user, false)
			t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })
			auth := models.AuthenticationConfig{Method: models.AuthenticationOAuth2, OAuthProvider: idp.URL, OAuthClientId: "fixture-client", OAuthClientSecret: "synthetic-secret", OAuthRecheckInterval: 1, OnlyRegisteredUsers: true}
			configuration.Get().Authentication = auth
			authentication.Init(auth)
			mux := http.NewServeMux()
			mux.HandleFunc("/login", HandlerLogin)
			mux.HandleFunc("/oauth-callback", HandlerCallback)
			mux.HandleFunc("/probe", func(w http.ResponseWriter, r *http.Request) {
				if r.TLS != nil {
					t.Error("backend unexpectedly received TLS")
				}
				cookie, err := r.Cookie("session_token")
				if err != nil {
					w.WriteHeader(401)
					return
				}
				session, ok := database.GetSession(cookie.Value)
				if !ok || session.UserId != user.Id {
					w.WriteHeader(401)
					return
				}
				w.WriteHeader(200)
			})
			mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) { sessionmanager.LogoutSession(w, r) })
			backend := httptest.NewServer(mux)
			t.Cleanup(backend.Close)
			target, err := url.Parse(backend.URL)
			if err != nil {
				t.Fatal(err)
			}
			proxy := httputil.NewSingleHostReverseProxy(target)
			var front *httptest.Server
			if secure {
				front = httptest.NewTLSServer(proxy)
			} else {
				front = httptest.NewServer(proxy)
			}
			t.Cleanup(front.Close)
			publicURL, err := url.Parse(front.URL)
			if err != nil {
				t.Fatal(err)
			}
			// Go 1.26 treats loopback HTTP as a secure cookie origin. Use a
			// non-loopback hostname, but route every connection to this fixture.
			publicURL.Host = net.JoinHostPort("example.com", publicURL.Port())
			publicBase := publicURL.String()
			configuration.Get().ServerUrl = publicBase + "/"
			Init(publicBase+"/", auth)
			client := front.Client()
			transport := client.Transport.(*http.Transport).Clone()
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr != publicURL.Host {
					return nil, fmt.Errorf("unexpected fixture destination: %s", addr)
				}
				return (&net.Dialer{}).DialContext(ctx, network, front.Listener.Addr().String())
			}
			if secure {
				if err := front.Certificate().VerifyHostname(publicURL.Hostname()); err != nil {
					t.Fatal(err)
				}
			}
			client.Transport = transport
			t.Cleanup(transport.CloseIdleConnections)
			client.Jar, err = cookiejar.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			get := func(path string) *http.Response {
				t.Helper()
				req, err := http.NewRequest(http.MethodGet, publicBase+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				// Deliberately inconsistent forwarding headers must not change
				// the policy derived from the configured external URL.
				req.Header.Set("X-Forwarded-Proto", "https")
				if secure {
					req.Header.Set("X-Forwarded-Proto", "http")
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				return resp
			}
			login := get("/login")
			if login.StatusCode != http.StatusFound {
				t.Fatal("login did not redirect to provider")
			}
			location, err := url.Parse(login.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := location.Query().Get("state")
			if state == "" {
				t.Fatal("missing login state")
			}
			for _, cookie := range login.Cookies() {
				if cookie.Name == authentication.CookieOauth && (cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode) {
					t.Fatal("callback cookie attributes changed")
				}
			}
			wrong := get("/oauth-callback?state=wrong&code=synthetic-code")
			if wrong.Header.Get("Location") == "/admin" || get("/probe").StatusCode != 401 {
				t.Fatal("wrong state authenticated")
			}
			callback := get("/oauth-callback?state=" + url.QueryEscape(state) + "&code=synthetic-code")
			if callback.StatusCode != http.StatusTemporaryRedirect || callback.Header.Get("Location") != "/admin" {
				location, _ := url.Parse(callback.Header.Get("Location"))
				t.Fatalf("valid callback did not authenticate: status=%d path=%s", callback.StatusCode, location.Path)
			}
			if get("/probe").StatusCode != 200 {
				t.Fatal("browser did not send the new session through proxy")
			}
			httpURL := *publicURL
			httpURL.Scheme = "http"
			sentOverHTTP := false
			for _, cookie := range client.Jar.Cookies(&httpURL) {
				if cookie.Name == "session_token" {
					sentOverHTTP = true
				}
			}
			if sentOverHTTP == secure {
				t.Fatal("session cookie HTTP transport policy is incorrect")
			}
			get("/logout")
			if get("/probe").StatusCode != 401 {
				t.Fatal("logout left a usable session")
			}
		})
	}
}
