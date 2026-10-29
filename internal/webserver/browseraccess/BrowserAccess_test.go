package browseraccess

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopPolicy(t *testing.T) {
	for _, agent := range []string{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)",
		"Mozilla/5.0 (Linux; Android 15) Mobile", "Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)", "Opera Mini/12"} {
		called := false
		handler := DesktopOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }), "zh-CN")
		r := httptest.NewRequest("POST", "/setup/setupResult", nil)
		r.Header.Set("User-Agent", agent)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if called || response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "请使用电脑") {
			t.Errorf("mobile request reached handler or lacks explanation: %s", agent)
		}
	}
	for _, agent := range []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "Mozilla/5.0 (Macintosh; Intel Mac OS X)", "curl/8.0", ""} {
		response := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/login", nil)
		r.Header.Set("User-Agent", agent)
		DesktopOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }), "en").ServeHTTP(response, r)
		if response.Code != 204 {
			t.Errorf("desktop/API client rejected: %q", agent)
		}
	}
}
