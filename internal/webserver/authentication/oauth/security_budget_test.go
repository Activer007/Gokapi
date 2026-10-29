//go:build test

package oauth

import (
	"context"
	"encoding/json"
	"github.com/coreos/go-oidc/v3/oidc"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/webserver/authentication"
	"golang.org/x/oauth2"
)

func TestSEC014CanceledCallbackDoesNotExchange(t *testing.T) {
	oldConfig, oldContext := config, ctx
	t.Cleanup(func() { config, ctx = oldConfig, oldContext })
	var requests atomic.Int32
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); http.Error(w, "synthetic refusal", 400) }))
	defer idp.Close()
	ctx = context.Background()
	config = oauth2.Config{ClientID: "synthetic", Endpoint: oauth2.Endpoint{TokenURL: idp.URL, AuthStyle: oauth2.AuthStyleInParams}}
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/oauth-callback?state=synthetic-state&code=synthetic-code", nil).WithContext(requestContext)
	r.AddCookie(&http.Cookie{Name: authentication.CookieOauth, Value: "synthetic-state"})
	HandlerCallback(httptest.NewRecorder(), r)
	if requests.Load() != 0 {
		t.Fatal("canceled callback contacted token endpoint")
	}
}

func TestSEC014CallbackDeadlineReachesProvider(t *testing.T) {
	for _, stage := range []string{"token", "userinfo"} {
		t.Run(stage, func(t *testing.T) {
			oldConfig, oldProvider, oldContext := config, provider, ctx
			defer func() { config, provider, ctx = oldConfig, oldProvider, oldContext }()
			started := make(chan struct{}, 1)
			disconnected := make(chan struct{}, 1)
			release := make(chan struct{})
			var base string
			idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.Copy(io.Discard, r.Body)
				if r.URL.Path == "/"+stage {
					started <- struct{}{}
					select {
					case <-r.Context().Done():
						disconnected <- struct{}{}
						return
					case <-release:
						http.Error(w, "synthetic release", 400)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					json.NewEncoder(w).Encode(map[string]string{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "userinfo_endpoint": base + "/userinfo", "jwks_uri": base + "/keys"})
				case "/token":
					io.WriteString(w, `{"access_token":"synthetic-access-token","token_type":"Bearer","expires_in":60}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer idp.Close()
			base = idp.URL
			ctx = context.Background()
			var err error
			provider, err = oidc.NewProvider(ctx, base)
			if err != nil {
				t.Fatal(err)
			}
			config = oauth2.Config{ClientID: "synthetic", Endpoint: provider.Endpoint()}
			config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
			requestContext, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			r := httptest.NewRequest("GET", "/oauth-callback?state=synthetic-state&code=synthetic-code", nil).WithContext(requestContext)
			r.AddCookie(&http.Cookie{Name: authentication.CookieOauth, Value: "synthetic-state"})
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { HandlerCallback(w, r); close(done) }()
			defer func() { cancel(); close(release); <-done }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("provider stage not reached")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("callback ignored request deadline")
			}
			select {
			case <-disconnected:
			case <-time.After(time.Second):
				t.Error("provider request remained connected")
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == "session_token" && c.Value != "" {
					t.Error("timed-out callback authenticated")
				}
			}
		})
	}
}
