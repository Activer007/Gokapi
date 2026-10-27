//go:build test

package setup

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression promoted from the M0 opt-in RED. No setup result is applied.
func TestSEC010InitialSetupRequiresAuthorization(t *testing.T) {
	oldComplete := setupComplete.Load()
	setupComplete.Store(false)
	t.Cleanup(func() { setupComplete.Store(oldComplete) })
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
