//go:build test && !integration

package webserver

import (
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/webserver/authentication/csrftoken"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSEC023BrowserCookieContract(t *testing.T) {
	oldURL := configuration.Get().ServerUrl
	t.Cleanup(func() { configuration.Get().ServerUrl = oldURL })
	for _, secure := range []bool{false, true} {
		base, name := "http://example.invalid/", "gokapi_csrf"
		if secure {
			base, name = "https://example.invalid/", "__Host-gokapi_csrf"
		}
		configuration.Get().ServerUrl = base
		r := httptest.NewRequest(http.MethodGet, base+"login", nil)
		w := httptest.NewRecorder()
		first := newBrowserCSRF(w, r, csrftoken.TypeLogin)
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing browser context cookie")
		}
		c := cookies[0]
		if c.Name != name || c.Secure != secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 1800 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("browser context cookie or cache policy changed")
		}
		r.AddCookie(c)
		w = httptest.NewRecorder()
		second := newBrowserCSRF(w, r, csrftoken.TypeLogin)
		if first == second || w.Result().Cookies()[0].Value != c.Value {
			t.Fatal("new tab must reuse context but receive a new token")
		}
		if !csrftoken.IsValidBound(csrftoken.TypeLogin, first, browserCSRFBinding(r)) || !csrftoken.IsValidBound(csrftoken.TypeLogin, second, browserCSRFBinding(r)) {
			t.Fatal("same-browser tabs must both work")
		}
		r.AddCookie(c)
		if browserCSRFBinding(r) != "" {
			t.Fatal("ambiguous duplicate cookie accepted")
		}
	}
}
