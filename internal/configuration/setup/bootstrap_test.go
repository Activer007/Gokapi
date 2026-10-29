//go:build test

package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/Activer007/SupaShare/internal/webserver/ratelimiter"
)

// Authentication must not depend on whether a configuration already exists.
func TestSEC010ProofRequired(t *testing.T) {
	oldInitial, oldUser, oldPassword := isInitialSetup, credentialUsername, credentialPassword
	oldComplete := setupComplete.Load()
	setupComplete.Store(false)
	t.Cleanup(func() { setupComplete.Store(oldComplete) })
	t.Cleanup(func() { isInitialSetup, credentialUsername, credentialPassword = oldInitial, oldUser, oldPassword })
	ratelimiter.SetUnitTestMode(true)
	t.Cleanup(func() { ratelimiter.SetUnitTestMode(false) })
	credentialUsername, credentialPassword = "setup", "synthetic-proof"
	for _, initial := range []bool{true, false} {
		isInitialSetup = initial
		for _, candidate := range []string{"", "wrong", "synthetic-proof"} {
			r := httptest.NewRequest(http.MethodPost, "/setup/setupResult", nil)
			if candidate != "" {
				r.SetBasicAuth("setup", candidate)
			}
			w := httptest.NewRecorder()
			called := false
			basicAuth(func(http.ResponseWriter, *http.Request) { called = true })(w, r)
			if called != (candidate == "synthetic-proof") {
				t.Errorf("initial=%v supplied=%v: authorization=%v", initial, candidate != "", called)
			}
			if candidate != "synthetic-proof" && w.Code != http.StatusUnauthorized {
				t.Errorf("initial=%v: status=%d, want 401", initial, w.Code)
			}
		}
	}
}

func TestSEC010CredentialLifecycle(t *testing.T) {
	oldUser, oldPassword, oldAWS, oldFile := credentialUsername, credentialPassword, credentialAWSTest, credentialFile
	oldComplete := setupComplete.Load()
	t.Cleanup(func() {
		credentialUsername, credentialPassword, credentialAWSTest, credentialFile = oldUser, oldPassword, oldAWS, oldFile
		setupComplete.Store(oldComplete)
	})
	dir := t.TempDir()
	t.Setenv("GOKAPI_CONFIG_DIR", dir)
	if err := prepareSetupCredentials(); err != nil {
		t.Fatal(err)
	}
	firstFile, firstPassword := credentialFile, credentialPassword
	data, err := os.ReadFile(firstFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != firstPassword || len(firstPassword) != 32 {
		t.Fatal("credential file does not match active proof")
	}
	info, err := os.Stat(firstFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal("credential file is not private")
	}
	if filepath.Dir(firstFile) != dir {
		t.Fatal("credential file escaped config directory")
	}
	setupComplete.Store(true)
	r := httptest.NewRequest(http.MethodGet, "/setup/start", nil)
	r.SetBasicAuth("setup", firstPassword)
	w := httptest.NewRecorder()
	basicAuth(func(http.ResponseWriter, *http.Request) { t.Error("consumed proof accepted") })(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("consumed proof did not return 401")
	}
	removeSetupCredential()
	if _, err := os.Stat(firstFile); !os.IsNotExist(err) {
		t.Fatal("proof file remains")
	}
	if err := prepareSetupCredentials(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(removeSetupCredential)
	if credentialPassword == firstPassword || credentialFile == firstFile {
		t.Fatal("proof reused across setup runs")
	}
	w = httptest.NewRecorder()
	basicAuth(func(http.ResponseWriter, *http.Request) { t.Error("previous proof accepted after restart") })(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("old proof did not return 401")
	}
}

func TestSEC010ListenBoundary(t *testing.T) {
	if setupListenAddress("53842", false) != "127.0.0.1:53842" {
		t.Fatal("native setup is not loopback-only")
	}
	if setupListenAddress("53842", true) != ":53842" {
		t.Fatal("Docker port mapping cannot reach setup")
	}
}

func TestSEC010BrowserOrigin(t *testing.T) {
	oldUser, oldPassword, oldComplete := credentialUsername, credentialPassword, setupComplete.Load()
	credentialUsername, credentialPassword = "setup", "synthetic-proof"
	setupComplete.Store(false)
	t.Cleanup(func() {
		credentialUsername, credentialPassword = oldUser, oldPassword
		setupComplete.Store(oldComplete)
	})
	for _, tc := range []struct {
		origin, site string
		want         int
	}{
		{"", "", 200},
		{"http://fixture.invalid", "same-origin", 200},
		{"https://fixture.invalid", "", 200}, // TLS terminates at the trusted proxy.
		{"https://other.invalid", "cross-site", 403},
		{"https://other.invalid", "", 403},
		{"null", "", 403},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://fixture.invalid/setup/setupResult", nil)
		r.SetBasicAuth("setup", "synthetic-proof")
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Sec-Fetch-Site", tc.site)
		w := httptest.NewRecorder()
		basicAuth(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })(w, r)
		if w.Code != tc.want {
			t.Errorf("origin=%q site=%q: got %d, want %d", tc.origin, tc.site, w.Code, tc.want)
		}
	}
}

func TestSEC010SafeMethodCannotCommit(t *testing.T) {
	previous := setupComplete.Load()
	setupComplete.Store(false)
	t.Cleanup(func() { setupComplete.Store(previous) })
	w := httptest.NewRecorder()
	handleResult(w, httptest.NewRequest(http.MethodGet, "/setup/setupResult", nil), new(http.Server))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status=%d, want 405", w.Code)
	}
}

func TestSEC010CompletedCommitRejectsConcurrentReplay(t *testing.T) {
	previous := setupComplete.Load()
	setupComplete.Store(true)
	t.Cleanup(func() { setupComplete.Store(previous) })
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			handleResult(w, httptest.NewRequest(http.MethodPost, "/setup/setupResult", nil), new(http.Server))
			if w.Code != http.StatusConflict {
				t.Errorf("in-flight replay: status=%d", w.Code)
			}
		}()
	}
	wg.Wait()
}
