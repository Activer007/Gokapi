//go:build test && security_red

package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Opt-in desired invariant. No setup result is applied and no listener is opened.
func TestSEC010InitialSetupRequiresAuthorization(t *testing.T) {
	previous := isInitialSetup
	isInitialSetup = true
	t.Cleanup(func() { isInitialSetup = previous })
	called := false
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/setup/setupResult", nil)
	basicAuth(func(http.ResponseWriter, *http.Request) { called = true })(w, r)
	if called {
		t.Fatal("SEC-010: unauthenticated initial setup reaches configuration handler")
	}
}
