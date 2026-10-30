//go:build test && !integration

package webserver

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const sec050ReportOnlyPolicy = "base-uri 'self'; object-src 'none'; script-src 'self' 'wasm-unsafe-eval'"

func TestSEC050OriginalWriter(t *testing.T) {
	original := httptest.NewRecorder()
	withResponsePolicy(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if w != original {
			t.Fatal("response policy replaced the streaming writer")
		}
		w.Header().Set("Content-Security-Policy", "sandbox")
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(original, httptest.NewRequest(http.MethodGet, "/", nil))
	sec050CheckCommonHeaders(t, original.Result().Header)
	if original.Result().Header.Get("Content-Security-Policy") != "sandbox" {
		t.Fatal("download sandbox was overwritten")
	}
}

// Use the existing TestMain server to cover the actual outer handler, including
// responses that never reach renderPage. Do not follow redirects into a second
// response or silently accept an error page in place of the requested resource.
func TestSEC050PublicResponseHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, path, content string
		status              int
	}{
		{"login", "/login?lang=en", `id="uname_hidden"`, http.StatusOK},
		{"error", "/error?e2e=1&lang=en", `id="errorTitle"`, http.StatusOK},
		{"index", "/index", `http-equiv="Refresh"`, http.StatusOK},
		{"redirect", "/admin", "", http.StatusTemporaryRedirect},
		{"not-found", "/sec050-missing-resource", "404 page not found", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, body := sec050ReadResponse(t, tc.path, 256<<10)
			if response.StatusCode != tc.status {
				t.Fatalf("unexpected status: got %d, want %d", response.StatusCode, tc.status)
			}
			sec050CheckCommonHeaders(t, response.Header)
			if tc.content != "" && !bytes.Contains(body, []byte(tc.content)) {
				t.Error("expected product response content missing")
			}
			if tc.name == "redirect" && response.Header.Get("Location") != "/login" && response.Header.Get("Location") != "login" {
				t.Error("unauthenticated redirect target changed")
			}
		})
	}
}

// nosniff makes the MIME of scripts and workers consequential. Check the
// production file-serving routes rather than deriving MIME from file suffixes.
func TestSEC050ArtifactResponseHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, path, mediaType, content string
		wasm, staticHTML               bool
	}{
		{"download-wasm", "/main.wasm", "application/wasm", "", true, false},
		{"e2e-wasm", "/e2e.wasm", "application/wasm", "", true, false},
		{"shared-worker", "/js/sse-worker.js", "javascript", "new EventSource(SSE_URL)", false, false},
		{"service-worker", "/serviceworker/sw.js", "javascript", "self.onfetch", false, false},
		{"streamsaver-frame", "/serviceworker/", "text/html", "registerWorker", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit := int64(256 << 10)
			if tc.wasm {
				limit = 8 // Validate binary framing without reading both full WASM binaries.
			}
			response, body := sec050ReadResponse(t, tc.path, limit)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("artifact route returned status %d", response.StatusCode)
			}
			sec050CheckCommonHeaders(t, response.Header)
			mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal("invalid artifact Content-Type")
			}
			if tc.mediaType == "javascript" {
				if mediaType != "text/javascript" && mediaType != "application/javascript" {
					t.Errorf("worker has non-JavaScript MIME %q", mediaType)
				}
			} else if mediaType != tc.mediaType {
				t.Errorf("unexpected artifact MIME %q", mediaType)
			}
			if tc.wasm && !bytes.Equal(body, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
				t.Error("WASM magic or version missing")
			}
			if tc.content != "" && !bytes.Contains(body, []byte(tc.content)) {
				t.Error("expected worker or iframe implementation missing")
			}
			if tc.staticHTML && (response.Header.Get("Content-Security-Policy") != "" || response.Header.Get("Content-Security-Policy-Report-Only") != "") {
				t.Error("application page CSP leaked onto the StreamSaver static iframe")
			}
		})
	}
}

func TestSEC050PageReportOnly(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		view          interface{ setUI(pageUI) }
	}{
		{"login", `id="uname_hidden"`, &LoginView{}},
		{"download", "synthetic-policy.txt", &DownloadView{Name: "synthetic-policy.txt", IsDownloadView: true}},
		{"error", `id="errorTitle"`, &genericView{IsGenericError: true, ErrorId: 0}},
		{"forgotpw", "If you forgot your user password", &genericView{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/?lang=en", nil)
			if err := renderPage(response, request, tc.name, tc.view); err != nil {
				t.Fatal(err)
			}
			if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(response.Body.String(), tc.content) {
				t.Error("application HTML rendering changed")
			}
			if response.Header().Get("Content-Security-Policy-Report-Only") != sec050ReportOnlyPolicy {
				t.Error("page CSP observation policy missing or changed")
			}
			if response.Header().Get("Content-Security-Policy") != "" {
				t.Error("page CSP must be observed before enforcement")
			}
		})
	}
}

func sec050ReadResponse(t *testing.T, path string, limit int64) (*http.Response, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:53843"+path, nil)
	if err != nil {
		t.Fatal("unable to create isolated HTTP request")
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("isolated product HTTP request failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	if err != nil {
		t.Fatal("unable to read isolated product response")
	}
	return response, body
}

func sec050CheckCommonHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	for name, expected := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
	} {
		if headers.Get(name) != expected {
			t.Errorf("missing or changed %s policy", name)
		}
	}
}
