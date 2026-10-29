package cliapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSEC040CLIHTTPStatus(t *testing.T) {
	previousKey := apiKey
	apiKey = "synthetic-cli-private-key"
	t.Cleanup(func() { apiKey = previousKey })
	const responseBody = "synthetic-private-response-body"
	for _, scenario := range []struct {
		status  int
		wantErr error
	}{
		{http.StatusConflict, nil},
		{http.StatusInternalServerError, nil},
		{http.StatusServiceUnavailable, nil},
		{http.StatusBadRequest, ErrInvalidRequest},
		{http.StatusUnauthorized, ErrUnauthorised},
		{http.StatusNotFound, ErrNotFound},
		{http.StatusOK, nil},
	} {
		t.Run(fmt.Sprint(scenario.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(scenario.status)
				_, _ = w.Write([]byte(responseBody))
			}))
			defer server.Close()
			result, err := getUrl(server.URL+"/restore?private=synthetic-url-marker", nil, false)
			if scenario.status == http.StatusOK {
				if err != nil || result != responseBody {
					t.Fatal("HTTP 200 did not preserve the successful response body")
				}
				return
			}
			if scenario.wantErr != nil {
				if err != scenario.wantErr || result != "" {
					t.Fatal("legacy status sentinel or empty error response changed")
				}
				return
			}
			if err == nil || result != "" {
				t.Fatal("non-2xx status was reported as success or exposed its response body")
			}
			// Exact fixed text excludes endpoint/query, API key and response body.
			if err.Error() != fmt.Sprintf("HTTP status %d", scenario.status) {
				t.Fatal("HTTP failure did not return only its fixed status-code error")
			}
		})
	}
}
