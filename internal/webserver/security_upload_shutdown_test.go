//go:build test

package webserver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/webserver/fileupload/uploadservice"
)

func shutdownTestSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("%s did not finish", name)
	}
}

func TestSEC030UploadShutdownHTTPAndWorkerTail(t *testing.T) {
	before := uploadservice.Current()
	workers := uploadservice.New(context.Background())
	if uploadservice.Current() != before {
		t.Fatal("independent drain fixture replaced the global upload generation")
	}
	ticket, err := workers.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	httpRelease, workerRelease := make(chan struct{}), make(chan struct{})
	httpEntered, httpExited := make(chan struct{}), make(chan struct{})
	workerCanceled, workerExited := make(chan struct{}), make(chan struct{})
	var releaseHTTP, releaseWorker sync.Once
	var server *httptest.Server
	t.Cleanup(func() {
		releaseHTTP.Do(func() { close(httpRelease) })
		releaseWorker.Do(func() { close(workerRelease) })
		workers.BeginShutdown()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := workers.Wait(ctx); err != nil {
			t.Error("fixture left an admitted worker running")
		}
		if server != nil {
			server.Close()
		}
	})
	go func() {
		defer close(workerExited)
		defer ticket.Done()
		<-ticket.Context().Done()
		close(workerCanceled)
		// Cancellation alone is not proof that a worker's cleanup/DB tail ended.
		<-workerRelease
	}()
	server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(httpExited)
		close(httpEntered)
		<-httpRelease
		_, _ = io.WriteString(w, "finished")
	}))
	server.Config.BaseContext = func(net.Listener) context.Context { return workers.Context() }
	server.Start()
	clientExited := make(chan struct{})
	go func() {
		defer close(clientExited)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	shutdownTestSignal(t, httpEntered, "real HTTP handler admission")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	if shutdownUploads(ctx, server.Config, workers) {
		t.Error("shutdown reported success while HTTP and worker tails were blocked")
	}
	cancel()
	shutdownTestSignal(t, workerCanceled, "worker cancellation")
	if next, err := workers.Acquire(context.Background()); !errors.Is(err, uploadservice.ErrClosing) {
		if next != nil {
			next.Done()
		}
		t.Fatal("shutdown did not stop new upload admission")
	}
	releaseHTTP.Do(func() { close(httpRelease) })
	shutdownTestSignal(t, httpExited, "HTTP handler drain")
	shutdownTestSignal(t, clientExited, "HTTP client exit")
	httpCtx, finishHTTP := context.WithTimeout(context.Background(), 3*time.Second)
	httpErr := server.Config.Shutdown(httpCtx)
	finishHTTP()
	if httpErr != nil {
		t.Fatal("fixture HTTP server did not independently finish draining", httpErr)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 25*time.Millisecond)
	if shutdownUploads(ctx, server.Config, workers) {
		t.Error("HTTP drain alone was accepted before the worker cleanup tail ended")
	}
	cancel()
	select {
	case <-workerExited:
		t.Fatal("worker ended before the explicit cleanup release")
	default:
	}
	releaseWorker.Do(func() { close(workerRelease) })
	shutdownTestSignal(t, workerExited, "worker cleanup drain")
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !shutdownUploads(ctx, server.Config, workers) {
		t.Fatal("fully drained HTTP and workers were not acknowledged")
	}
}

func TestSEC030UploadShutdownBeforeHTTPStart(t *testing.T) {
	workers := uploadservice.New(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !shutdownUploads(ctx, nil, workers) {
		t.Fatal("shutdown before server installation did not drain the empty lifetime")
	}
	if workers.Context().Err() == nil {
		t.Fatal("shutdown before server installation did not cancel its lifetime")
	}
}
