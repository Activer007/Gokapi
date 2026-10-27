//go:build test

package oauth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
)

func TestSEC012CallbackCookieTransport(t *testing.T) {
	previous := configuration.Get().ServerUrl
	t.Cleanup(func() { configuration.Get().ServerUrl = previous })
	for _, tc := range []struct {
		url    string
		secure bool
	}{
		{"http://localhost:53842/", false}, {"https://files.example.invalid/", true},
	} {
		configuration.Get().ServerUrl = tc.url
		w := httptest.NewRecorder()
		setCallbackCookie(w, "synthetic-state")
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("expected one callback cookie")
		}
		c := cookies[0]
		if c.Secure != tc.secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Errorf("public URL %s: Secure=%v HttpOnly=%v SameSite=%v", tc.url, c.Secure, c.HttpOnly, c.SameSite)
		}
	}
}
