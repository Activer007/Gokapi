//go:build test && noaws

package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
)

const sec052CancelChild = "SUPASHARE_SEC052_CANCEL_CHILD"
const sec052CancelPassed = "SEC052_CANCEL_CONTRACT_OK"

// Encryption's in-memory master has no snapshot/reset API. A single isolated
// child keeps this test's synthetic master and TestMain fixture out of the
// parent's encryption state. Controlled output failure and real HTTP/1 client
// cancellation are separate subcases; neither claims browser coverage.
func TestSEC052ServeFileCancel(t *testing.T) {
	if os.Getenv(sec052CancelChild) == "1" {
		sec052ServeFileCancel(t)
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("SEC052_SETUP isolated test executable unavailable")
	}
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pattern := "^TestSEC052ServeFileCancel$"
	if selected := flag.Lookup("test.run"); selected != nil {
		if _, subtest, found := strings.Cut(selected.Value.String(), "/"); found {
			pattern += "/" + subtest
		}
	}
	child := exec.CommandContext(ctx, binary, "-test.run="+pattern, "-test.count=1", "-test.v")
	child.Dir = root
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if upper != sec052CancelChild && upper != "REAL_AWS_CREDENTIALS" && !strings.HasPrefix(upper, "GOKAPI_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, sec052CancelChild+"=1", "REAL_AWS_CREDENTIALS=false")
	output, childErr := child.CombinedOutput()
	if err := os.WriteFile(filepath.Join(root, "cancel-private.log"), output, 0600); err != nil {
		t.Fatal("SEC052_SETUP isolated diagnostics could not be retained")
	}
	if childErr != nil || !bytes.Contains(output, []byte(sec052CancelPassed)) {
		// Only fixed contract diagnostics are forwarded; arbitrary dependency
		// and fixture output may contain paths or synthetic credentials.
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, "SEC052_") {
				t.Log(strings.TrimSpace(line))
			}
		}
		t.Fatal("SEC052_CANCEL isolated normal/cancellation contract did not pass")
	}
	if bytes.Contains(output, []byte("SEC052_HTTP1_CANCEL_OK")) {
		t.Log("SEC052_HTTP1_CANCEL_OK")
	}
}

func sec052ServeFileCancel(t *testing.T) {
	t.Helper()
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal("SEC052_SETUP synthetic master creation failed")
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.LocalEncryptionStored, Cipher: master}})
	plaintext := bytes.Repeat([]byte("0123456789abcdef"), 8192) // 128 KiB across authentication blocks.
	var info models.EncryptionInfo
	var ciphertext bytes.Buffer
	if err := encryption.Encrypt(&info, bytes.NewReader(plaintext), &ciphertext); err != nil {
		t.Fatal("SEC052_SETUP real encrypted stream creation failed")
	}
	locator := sha1.Sum(plaintext) // Existing synthetic locator convention.
	file := models.File{
		Id: "sec052-cancel-local", UserId: 5, Name: "synthetic-cancel.bin", SHA1: hex.EncodeToString(locator[:]),
		SizeBytes: int64(len(plaintext)), ContentType: "application/octet-stream", Encryption: info,
		UnlimitedTime: true, UnlimitedDownloads: true,
	}
	path := filepath.Join(configuration.Get().DataDir, file.SHA1)
	if err := os.WriteFile(path, ciphertext.Bytes(), 0600); err != nil {
		t.Fatal("SEC052_SETUP owned ciphertext could not be written")
	}
	database.SaveMetaData(file)
	before, found := database.GetMetaDataById(file.Id)
	if !found || before.UserId <= 0 {
		t.Fatal("SEC052_SETUP positive owned metadata did not persist")
	}

	t.Run("normal", func(t *testing.T) {
		response := httptest.NewRecorder()
		synctest.Test(t, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/downloadFile", nil)
			if !ServeFile(file, response, request, true, false, false, false) {
				t.Fatal("SEC052_CONTROL valid encrypted download was refused")
			}
		}) // Drains the finite asynchronous traffic work before SQLite cleanup.
		if response.Code != http.StatusOK || response.Header().Get("Content-Disposition") == "" ||
			!bytes.Equal(response.Body.Bytes(), plaintext) || sha256.Sum256(response.Body.Bytes()) != sha256.Sum256(plaintext) {
			t.Fatal("SEC052_CONTROL normal plaintext bytes or SHA256 did not match")
		}
	})
	t.Run("cancel-after-partial-write", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response := &sec052CancelWriter{ResponseRecorder: httptest.NewRecorder(), ctx: ctx, entered: make(chan struct{})}
			request := httptest.NewRequest(http.MethodGet, "/downloadFile", nil).WithContext(ctx)
			type outcome struct {
				panicValue any
				returned   bool
			}
			done := make(chan outcome, 1)
			go func() {
				result := outcome{}
				defer func() {
					result.panicValue = recover()
					done <- result
				}()
				ServeFile(file, response, request, true, false, false, false)
				result.returned = true
			}()
			select {
			case <-response.entered:
			case <-time.After(time.Second):
				t.Fatal("SEC052_CANCEL real plaintext output was not reached")
			}
			cancel() // Only after real authenticated plaintext reached Write.
			var result outcome
			select {
			case result = <-done:
			case <-time.After(time.Second):
				t.Fatal("SEC052_CANCEL handler did not terminate after output cancellation")
			}
			synctest.Wait() // Drain handler and finite serverstats work inside this bubble.
			if result.returned || result.panicValue != http.ErrAbortHandler {
				t.Fatal("SEC052_CANCEL interrupted output did not use the transfer-abort path")
			}
			body := response.Body.Bytes()
			if response.writes != 1 || len(body) == 0 || len(body) >= len(plaintext) || !bytes.Equal(body, plaintext[:len(body)]) {
				t.Fatal("SEC052_CANCEL partial output was completed, retried or mixed with error text")
			}
			if ctx.Err() != context.Canceled {
				t.Fatal("SEC052_CANCEL request cancellation was not observed")
			}
		})
	})
	t.Run("http1-client-cancel", func(t *testing.T) {
		// The preceding bubbles use a fake clock. Refresh the real timestamp
		// synchronously before network work so its finite AddTraffic goroutine
		// cannot start a delayed database save during child/TestMain teardown.
		serverstats.AddTraffic(0)
		trafficBefore, _ := serverstats.GetCurrentTraffic()
		trafficWant := trafficBefore + uint64(file.SizeBytes)
		trafficSettled := func() bool {
			deadline := time.Now().Add(2 * time.Second)
			for {
				current, _ := serverstats.GetCurrentTraffic()
				if current >= trafficWant {
					return true
				}
				if time.Now().After(deadline) {
					return false
				}
				time.Sleep(time.Millisecond)
			}
		}
		type outcome struct {
			panicValue      any
			returned        bool
			requestCanceled bool
			writer          *sec052HTTPCancelWriter
		}
		done := make(chan outcome, 1)
		cleanup := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := outcome{writer: &sec052HTTPCancelWriter{ResponseWriter: w, request: r, cleanup: cleanup}}
			defer func() {
				result.panicValue = recover()
				result.requestCanceled = r.Context().Err() == context.Canceled
				done <- result
				if result.panicValue != nil {
					// Preserve net/http's real connection-abort handling after
					// observing ServeFile unwind and close its source descriptor.
					panic(result.panicValue)
				}
			}()
			ServeFile(file, result.writer, r, true, false, false, false)
			result.returned = true
		}))
		transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableKeepAlives: true}
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		ctx, cancelClient := context.WithCancel(t.Context())
		drained := false
		defer func() {
			cancelClient()
			// Failure cleanup alone may unblock the test writer. It never
			// cancels the server request or counts as cancellation evidence.
			close(cleanup)
			server.Close()
			transport.CloseIdleConnections()
			if !drained && !trafficSettled() {
				t.Error("SEC052_HTTP finite traffic work did not settle before teardown")
			}
		}()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/downloadFile", nil)
		if err != nil {
			t.Fatal("SEC052_HTTP isolated client request could not be created")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("SEC052_HTTP actual response did not start")
		}
		defer response.Body.Close()
		var first [1]byte
		if response.ProtoMajor != 1 || response.StatusCode != http.StatusOK {
			t.Fatal("SEC052_HTTP expected a real HTTP/1 encrypted response")
		}
		if _, err := io.ReadFull(response.Body, first[:]); err != nil || first[0] != plaintext[0] {
			t.Fatal("SEC052_HTTP client did not receive real plaintext before cancellation")
		}
		cancelClient()
		_ = response.Body.Close()
		var result outcome
		select {
		case result = <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("SEC052_HTTP handler did not exit after actual client cancellation")
		}
		if result.returned || result.panicValue != http.ErrAbortHandler || !result.requestCanceled ||
			result.writer.cleanupUsed || !result.writer.flushed || result.writer.writes != 1 ||
			len(result.writer.prefix) == 0 || len(result.writer.prefix) > 1024 ||
			!bytes.Equal(result.writer.prefix, plaintext[:len(result.writer.prefix)]) {
			t.Fatal("SEC052_HTTP client cancellation did not produce a bounded real transfer abort")
		}
		server.Close() // Wait for net/http's handler/connection cleanup as well.
		if !trafficSettled() {
			t.Fatal("SEC052_HTTP finite traffic work did not become visible")
		}
		drained = true
		t.Log("SEC052_HTTP1_CANCEL_OK")
	})
	// Counters are deliberately disabled in all calls, so the entire model
	// must remain exact. Production attempt-accounting semantics are unchanged.
	if after, found := database.GetMetaDataById(file.Id); !found || !reflect.DeepEqual(after, before) {
		t.Fatal("SEC052_CANCEL download changed its source metadata")
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, ciphertext.Bytes()) ||
		sha256.Sum256(data) != sha256.Sum256(ciphertext.Bytes()) {
		t.Fatal("SEC052_CANCEL download changed its owned ciphertext")
	}
	if !t.Failed() {
		t.Log(sec052CancelPassed)
	}
}

// A bounded prefix is actually accepted before cancellation. The next result
// from that same Write is a partial-write error, as an interrupted output can
// produce. No reader, ciphertext or production encryption function is mocked.
type sec052CancelWriter struct {
	*httptest.ResponseRecorder
	ctx     context.Context
	entered chan struct{}
	writes  int
}

func (writer *sec052CancelWriter) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes != 1 {
		return 0, context.Canceled
	}
	accepted := len(data)
	if accepted > 1024 {
		accepted = 1024
	}
	n, err := writer.ResponseRecorder.Write(data[:accepted])
	close(writer.entered)
	if err != nil {
		return n, err
	}
	<-writer.ctx.Done()
	return n, writer.ctx.Err()
}

// Only the first bounded prefix is written to the actual HTTP connection. A
// successful flush lets the client receive it before canceling its own request;
// the server-side error is released by that real request context, not by a test
// call to a server CancelFunc. cleanup exists solely to prevent a failed test
// from leaving a handler blocked, and is explicitly rejected by the assertions.
type sec052HTTPCancelWriter struct {
	http.ResponseWriter
	request     *http.Request
	cleanup     <-chan struct{}
	writes      int
	prefix      []byte
	flushed     bool
	cleanupUsed bool
}

func (writer *sec052HTTPCancelWriter) Write(data []byte) (int, error) {
	writer.writes++
	if writer.writes != 1 {
		return 0, context.Canceled
	}
	accepted := len(data)
	if accepted > 1024 {
		accepted = 1024
	}
	n, err := writer.ResponseWriter.Write(data[:accepted])
	writer.prefix = bytes.Clone(data[:n])
	if err != nil {
		return n, err
	}
	if err := http.NewResponseController(writer.ResponseWriter).Flush(); err != nil {
		return n, err
	}
	writer.flushed = true
	select {
	case <-writer.request.Context().Done():
		return n, writer.request.Context().Err()
	case <-writer.cleanup:
		writer.cleanupUsed = true
		return n, context.Canceled
	}
}
