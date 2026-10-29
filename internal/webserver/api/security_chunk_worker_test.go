//go:build test && security_whole && !integration

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/webserver/fileupload"
	"github.com/forceu/gokapi/internal/webserver/fileupload/uploadservice"
)

// A blocked final write proves that storage has returned but the API worker has
// not. Drain must keep waiting through both synchronous and detached worker tails.
func TestSEC030ChunkWorkerTailLifetime(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				for _, mode := range []string{"sync", "async"} {
					candidate := sec030ChunkCase{backend, kind, mode, "finish", 1}
					t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkWorkerTailLifetime", candidate) })
				}
			}
		}
		return
	}
	candidate := sec030ChunkSelected(t)
	fixture := sec030FinishSetup(t, candidate)
	generation := sec030WorkerGeneration(t)
	sec030FinishUpload(t, fixture)
	params := fileupload.CreateUploadConfig(0, 0, "", true, true, false, int64(len(fixture.content)), fixture.request.Id)
	task, err := fileupload.PrepareChunkCompletion(context.Background(), fixture.uuid,
		chunking.FileHeader{Filename: "synthetic-complete.png", ContentType: "image/png", Size: int64(len(fixture.content))}, sec030WholeOwner, params)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_WORKER_SETUP valid completion could not prepare")
	}
	user, found := database.GetUser(sec030WholeOwner)
	if !found {
		task.Done()
		t.Fatal("CHUNK_QUOTA_WORKER_SETUP synthetic owner disappeared")
	}
	writer := &sec030WorkerTailWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan struct{})
	t.Cleanup(func() {
		writer.resume.Do(func() { close(writer.release) })
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("CHUNK_QUOTA_WORKER_CLEANUP worker did not finish after its response tail was released")
		}
	})
	go func() {
		defer close(finished)
		completePreparedChunk(writer, task, user, params, candidate.scope == "async")
	}()
	select {
	case <-writer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("CHUNK_QUOTA_WORKER_SETUP completion did not reach the response tail")
	}
	// The gate is after Run and its logging query. Verify a real successful
	// storage commit before interpreting drain behavior as worker accounting.
	file := sec030FinishSettled(t, fixture)
	sec030FinishCheckFile(t, fixture, file)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	err = generation.Wait(ctx)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("CHUNK_QUOTA_WORKER_LIFETIME service drained before the admitted API worker returned")
	}
	writer.resume.Do(func() { close(writer.release) })
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("CHUNK_QUOTA_WORKER_LIFETIME worker did not return after output resumed")
	}
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := generation.Wait(ctx); err != nil {
		t.Error("CHUNK_QUOTA_WORKER_LIFETIME worker exit did not release its service ticket")
	}
	// Done is an explicit idempotent contract for callers owning the worker.
	task.Done()
	task.Done()
	var response sec030ChunkResponse
	if err := json.Unmarshal(writer.Body.Bytes(), &response); err != nil || response.Result != "OK" || response.FileInfo.Id != file.Id {
		t.Error("CHUNK_QUOTA_WORKER_RESPONSE completed worker did not output its committed identity")
	}
}

func TestSEC030ChunkWorkerPrepareFailureDrains(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			for _, kind := range []string{"ordinary", "fr"} {
				candidate := sec030ChunkCase{backend, kind, "user", "finish", 1}
				t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkWorkerPrepareFailureDrains", candidate) })
			}
		}
		return
	}
	fixture := sec030FinishSetup(t, sec030ChunkSelected(t))
	generation := sec030WorkerGeneration(t)
	params := fileupload.CreateUploadConfig(0, 0, "", true, true, false, int64(len(fixture.content)), fixture.request.Id)
	task, err := fileupload.PrepareChunkCompletion(context.Background(), fixture.uuid,
		chunking.FileHeader{Filename: "synthetic-complete.png", ContentType: "image/png", Size: int64(len(fixture.content))}, sec030WholeOwner, params)
	if task != nil {
		task.Done()
	}
	if err == nil || task != nil {
		t.Fatal("CHUNK_QUOTA_WORKER_SETUP missing chunks did not fail preparation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := generation.Wait(ctx); err != nil {
		t.Error("CHUNK_QUOTA_WORKER_LIFETIME failed preparation leaked an admitted service ticket")
	}
}

func sec030WorkerGeneration(t *testing.T) *uploadservice.Generation {
	t.Helper()
	generation := uploadservice.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := generation.Wait(ctx); err != nil {
			t.Error("CHUNK_QUOTA_WORKER_CLEANUP service did not drain")
		}
		uploadservice.Start(context.Background())
	})
	return generation
}

type sec030WorkerTailWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	resume  sync.Once
}

func (writer *sec030WorkerTailWriter) Write(data []byte) (int, error) {
	writer.once.Do(func() { close(writer.entered) })
	<-writer.release
	return writer.ResponseRecorder.Write(data)
}

func (writer *sec030WorkerTailWriter) WriteString(data string) (int, error) {
	return writer.Write([]byte(data))
}
