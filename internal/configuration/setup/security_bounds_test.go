//go:build test

package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Activer007/SupaShare/internal/webserver/ratelimiter"
)

func TestSEC014CancelledProofDoesNotProceed(t *testing.T) {
	oldUser, oldPassword, oldComplete := credentialUsername, credentialPassword, setupComplete.Load()
	t.Cleanup(func() {
		credentialUsername, credentialPassword = oldUser, oldPassword
		setupComplete.Store(oldComplete)
		ratelimiter.SetUnitTestMode(false)
	})
	credentialUsername, credentialPassword = "setup", "synthetic-proof"
	setupComplete.Store(false)
	ratelimiter.SetUnitTestMode(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/setup/setupResult", nil).WithContext(ctx)
	r.SetBasicAuth("setup", "synthetic-proof")
	called := false
	basicAuth(func(http.ResponseWriter, *http.Request) { called = true })(httptest.NewRecorder(), r)
	if called {
		t.Fatal("canceled request passed bootstrap authentication")
	}
}
