//go:build test && security_red

package sessionmanager

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
)

func TestSEC012HTTPSCookieRequiresSecure(t *testing.T) {
	previous := configuration.Get().ServerUrl
	configuration.Get().ServerUrl = "https://fixture.invalid/"
	t.Cleanup(func() { configuration.Get().ServerUrl = previous })
	w := httptest.NewRecorder()
	writeSessionCookie(w, "synthetic-session", time.Now().Add(time.Minute))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("SEC-012: HTTPS session cookie lacks Secure")
	}
}
