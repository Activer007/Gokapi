//go:build test

package securityfixture

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// S3 is an in-memory S3 endpoint with opt-in atomic faults. DELETE failures
// leave HEAD and GET working unless FailHead is separately enabled. IgnoreDelete
// acknowledges DELETE without altering the object. Defaults preserve normal S3
// behavior; there is no external storage or credential use.
type S3 struct {
	Server         *httptest.Server
	FailDelete     atomic.Bool
	FailHead       atomic.Bool
	IgnoreDelete   atomic.Bool
	DeleteAttempts atomic.Int64
	HeadAttempts   atomic.Int64
}

func NewS3(t *testing.T) *S3 {
	t.Helper()
	backend := s3mem.New()
	if err := backend.CreateBucket("security-fixture"); err != nil {
		t.Fatal(err)
	}
	f := &S3{}
	next := gofakes3.New(backend).Server()
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			f.HeadAttempts.Add(1)
			if f.FailHead.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		if r.Method == http.MethodDelete {
			f.DeleteAttempts.Add(1)
			if f.FailDelete.Load() {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`<Error><Code>ServiceUnavailable</Code><Message>synthetic delete failure</Message></Error>`))
				return
			}
			if f.IgnoreDelete.Load() {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Server.Close)
	return f
}
