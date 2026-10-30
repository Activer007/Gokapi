//go:build test && !noaws && !awsmock

package aws

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestM5SDKStreamContextCancellationLoopback(t *testing.T) {
	arrived, upstreamCanceled, release, done := make(chan struct{}, 1), make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	m5DownloadSDKEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case arrived <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			select {
			case upstreamCanceled <- struct{}{}:
			default:
			}
		case <-release:
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	t.Cleanup(func() {
		cancel()
		unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("M5_STREAM_DRAIN stream did not finish after fixture release")
		}
	})
	result := make(chan error, 1)
	go func() {
		defer close(done)
		result <- StreamContext(ctx, io.Discard, m5DownloadSDKFile(4096))
	}()
	select {
	case <-arrived:
	case <-time.After(3 * time.Second):
		t.Fatal("M5_STREAM_SETUP GET never reached loopback")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Error("M5_STREAM_CANCEL did not preserve request cancellation")
		}
	case <-time.After(time.Second):
		t.Error("M5_STREAM_CANCEL stream remained blocked on upstream")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Error("M5_STREAM_CANCEL upstream HTTP request was not canceled")
	}
}
