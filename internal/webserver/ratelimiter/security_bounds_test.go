//go:build test

package ratelimiter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestSEC014CancelledWait(t *testing.T) {
	old := failedIdLimiter
	failedIdLimiter = newLimiter()
	t.Cleanup(func() { failedIdLimiter = old })
	if !failedIdLimiter.Get("192.0.2.1", 1, 10).AllowN(time.Now(), 10) {
		t.Fatal("cannot exhaust fixture limiter")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { WaitOnFailedId(r); close(done) }()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("canceled request remains queued in limiter")
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("limiter did not drain")
		}
	}
}

func TestSEC014AllWaitsCancel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slot  **store
		key   string
		burst int
		wait  func(context.Context) error
	}{
		{"login", &failedLoginLimiter, "192.0.2.1", 9, func(ctx context.Context) error { return WaitOnLogin(ctx, "192.0.2.1") }},
		{"setup", &failedSetupLogin, "setup", 15, WaitOnSetupLogin},
		{"api", &failedApiKeyLimiter, "192.0.2.1", 200, func(ctx context.Context) error { return WaitOnApiAuthentication(ctx, "192.0.2.1") }},
		{"download-password", &failedDownloadPasswordLimiter, "192.0.2.1", 20, func(ctx context.Context) error { return WaitOnDownloadPassword(ctx, "192.0.2.1") }},
		{"failed-id", &failedIdLimiter, "192.0.2.1", 10, func(ctx context.Context) error {
			return WaitOnFailedId(httptest.NewRequest("GET", "/", nil).WithContext(ctx))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := *tc.slot
			*tc.slot = newLimiter()
			t.Cleanup(func() { *tc.slot = old })
			if err := tc.wait(context.Background()); err != nil {
				t.Fatal("fresh request rejected", err)
			}
			limiter := (*tc.slot).Get(tc.key, 1, tc.burst)
			limiter.AllowN(time.Now(), int(limiter.Tokens()))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- tc.wait(ctx) }()
			select {
			case err := <-done:
				t.Fatalf("exhausted limiter did not wait: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("got %v, want cancellation", err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Fatal("cancel did not release waiter")
			}
		})
	}
}

func TestSEC014HTTPDisconnectCancelsWait(t *testing.T) {
	old := failedLoginLimiter
	failedLoginLimiter = newLimiter()
	t.Cleanup(func() { failedLoginLimiter = old })
	failedLoginLimiter.Get("fixture", 1, 9).AllowN(time.Now(), 9)
	started := make(chan struct{})
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		finished <- WaitOnLogin(r.Context(), "fixture")
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		resp, _ := server.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("disconnect returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP disconnect left handler queued")
	}
	<-clientDone
}

func TestSEC014CleanupGoroutinesBounded(t *testing.T) {
	s := newLimiter()
	before := runtime.NumGoroutine()
	for i := 0; i < 128; i++ {
		s.Get("same-key", 1, 10)
	}
	time.Sleep(50 * time.Millisecond)
	if growth := runtime.NumGoroutine() - before; growth > 4 {
		t.Fatalf("128 lookups retained %d goroutines; expected one cleaner", growth)
	}
}
