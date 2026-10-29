//go:build test

package fileupload

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/storage/chunking/chunksession"
	"github.com/forceu/gokapi/internal/storage/filesystem"
)

// These tests use only synthetic bodies and their own storage directory. They
// exercise production receive entry points without relying on a new pool API.
func sec030ReceiveDirectory(t *testing.T) string {
	t.Helper()
	config := configuration.Get()
	original := config.DataDir
	path := t.TempDir()
	config.DataDir = path
	filesystem.Init(path)
	t.Cleanup(func() {
		config.DataDir = original
		filesystem.Init(original)
	})
	return path
}

// A production pool owns exactly one DataDir. Give each fixture its own process
// instead of resetting server state or weakening the spool ownership check.
func sec030ReceiveChild(t *testing.T) bool {
	t.Helper()
	const childEnv = "GOKAPI_SEC030_RECEIVE_CHILD"
	if os.Getenv(childEnv) == t.Name() {
		return true
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^"+t.Name()+"$", "-test.v", "-test.timeout=80s")
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), childEnv+"="+t.Name())
	output, err := command.CombinedOutput()
	t.Logf("isolated receive contract:\n%s", output)
	if err != nil {
		t.Fatalf("receive contract failed: %v", err)
	}
	return false
}

type sec030ReceiveEvent struct {
	id   int
	read bool
	err  error
}

type sec030ReceiveGateBody struct {
	id      int
	events  chan<- sec030ReceiveEvent
	release <-chan struct{}
	once    sync.Once
	entered atomic.Bool
}

func (body *sec030ReceiveGateBody) Read(_ []byte) (int, error) {
	body.once.Do(func() {
		body.entered.Store(true)
		body.events <- sec030ReceiveEvent{id: body.id, read: true}
		<-body.release
	})
	return 0, io.EOF
}

func (*sec030ReceiveGateBody) Close() error { return nil }

func sec030ReceiveEmptyRequest(ctx context.Context, body io.ReadCloser) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/chunk/add", nil).WithContext(ctx)
	request.Header.Set("Content-Type", "multipart/form-data; boundary=sec030-synthetic-empty")
	request.Body = body
	request.ContentLength = -1
	return request
}

func TestSEC030ReceiveBodyAdmission(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	sec030ReceiveDirectory(t)
	for _, contenders := range []int{10, 50} {
		t.Run(strconv.Itoa(contenders), func(t *testing.T) {
			// The reference profile's default is four receive slots. All accepted
			// bodies hold their first Read so a slot cannot be reused during the
			// observation. Rejected requests must return without touching Body.
			const capacity = 4
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			events := make(chan sec030ReceiveEvent, 2*contenders)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			ready := make(chan struct{}, contenders)
			start := make(chan struct{})
			bodies := make([]*sec030ReceiveGateBody, contenders)
			var workers sync.WaitGroup
			for i := 0; i < contenders; i++ {
				body := &sec030ReceiveGateBody{id: i, events: events, release: release}
				bodies[i] = body
				workers.Add(1)
				go func(id int, body *sec030ReceiveGateBody) {
					defer workers.Done()
					ready <- struct{}{}
					<-start
					request := sec030ReceiveEmptyRequest(ctx, body)
					var err error
					switch id % 3 {
					case 0:
						request.URL.Path = "/api/files/add"
						err = ProcessCompleteFile(httptest.NewRecorder(), request, 9130, configuration.Get().MaxMemory)
					case 1:
						_, err = ProcessNewChunk(httptest.NewRecorder(), request, true, "", 9130, 1024*1024)
					case 2:
						// This is a pre-parse resource test, not an authentication
						// shortcut: EOF prevents reaching any reservation operation.
						// Real request/Key authentication remains in route tests.
						request.URL.Path = "/api/uploadrequest/chunk/add"
						_, err = ProcessNewChunk(httptest.NewRecorder(), request, true, "sec030-synthetic-request", 9130, 1024*1024)
					}
					events <- sec030ReceiveEvent{id: id, err: err}
				}(i, body)
			}
			for i := 0; i < contenders; i++ {
				<-ready
			}
			close(start)
			classified := make(map[int]bool, contenders)
			reads, rejected := 0, 0
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			observed := true
			for len(classified) < contenders && observed {
				select {
				case event := <-events:
					if classified[event.id] {
						continue
					}
					classified[event.id] = true
					if event.read {
						reads++
					} else {
						rejected++
						if event.err == nil || bodies[event.id].entered.Load() {
							t.Error("body rejection did not fail before its first Read")
						}
					}
				case <-timer.C:
					observed = false
					t.Error("excess receive requests did not return while accepted bodies were blocked")
				}
			}
			// Always unblock and join the synthetic bodies before reporting a
			// failed invariant, so the RED run does not leave held goroutines.
			unblock()
			workers.Wait()
			if reads != capacity || rejected != contenders-capacity {
				t.Errorf("receive admission: body reads=%d rejected=%d; want %d/%d", reads, rejected, capacity, contenders-capacity)
			}
			// Both entry points must remain usable after all held requests fail
			// parsing. These empty probes create no chunks or metadata.
			for _, entry := range []string{"chunk", "whole-file"} {
				probeEvents := make(chan sec030ReceiveEvent, 1)
				probe := &sec030ReceiveGateBody{events: probeEvents, release: release}
				request := sec030ReceiveEmptyRequest(context.Background(), probe)
				var err error
				if entry == "chunk" {
					_, err = ProcessNewChunk(httptest.NewRecorder(), request, true, "", 9130, 1024*1024)
				} else {
					err = ProcessCompleteFile(httptest.NewRecorder(), request, 9130, configuration.Get().MaxMemory)
				}
				if err == nil || !probe.entered.Load() {
					t.Errorf("%s receive slot did not recover after parse failure", entry)
				}
			}
		})
	}
}

func sec030ReceiveChunk(t *testing.T, fileFirst, unknownLength bool) {
	t.Helper()
	payload := bytes.Repeat([]byte("q"), 64*1024)
	id := rand.Text()
	const user = 9130
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	writeFields := func() {
		for _, field := range [][2]string{{"uuid", id}, {"filesize", strconv.Itoa(len(payload))}, {"offset", "0"}} {
			if err := form.WriteField(field[0], field[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeFile := func() {
		part, err := form.CreateFormFile("file", "synthetic.bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = part.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if fileFirst {
		writeFile()
		writeFields()
	} else {
		writeFields()
		writeFile()
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/chunk/add", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", form.FormDataContentType())
	if unknownLength {
		request.ContentLength = -1
	}
	recorder := httptest.NewRecorder()
	code, err := ProcessNewChunk(recorder, request, true, "", user, 1024*1024)
	if err != nil || code != 0 {
		t.Fatalf("normal last chunk rejected: code=%d error=%v", code, err)
	}
	if recorder.Body.String() != `{"result":"OK"}` {
		t.Fatalf("upload response changed: %q", recorder.Body.String())
	}
	assertReceivedChunkSession(t, chunksession.Scope{ClientID: id, UserID: user}, payload)
}

func TestSEC030ReceiveFileFirstUnknownLengthLastChunk(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	sec030ReceiveDirectory(t)
	sec030ReceiveChunk(t, true, true)
}

func TestSEC030ReceiveDeclaredTailMustMatchPayload(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	directory := sec030ReceiveDirectory(t)
	id := rand.Text()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "synthetic.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "testContent"); err != nil {
		t.Fatal(err)
	}
	for _, field := range [][2]string{{"uuid", id}, {"filesize", "13"}, {"offset", "0"}} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/chunk/add", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	if _, err := ProcessNewChunk(httptest.NewRecorder(), request, true, "", 9130, 1024*1024); err == nil {
		t.Fatal("11-byte payload was accepted as a complete 13-byte last chunk")
	}
	ledger, err := database.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, found := ledger.Records[chunksession.ID(chunksession.Scope{ClientID: id, UserID: 9130})]; found {
		t.Fatal("invalid last chunk acquired a durable reservation")
	}
	if _, err := os.Lstat(filepath.Join(directory, ".gokapi-chunks-v1")); !os.IsNotExist(err) {
		t.Fatal("invalid last chunk allocated a persistent namespace", err)
	}
	if count := sec030ReceivePayloadCount(t, directory); count != 0 {
		t.Fatal("invalid last chunk retained receive payload")
	}
}

func TestSEC030ReceiveKnownLengthOrders(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	sec030ReceiveDirectory(t)
	for _, fileFirst := range []bool{true, false} {
		t.Run(strconv.FormatBool(fileFirst), func(t *testing.T) { sec030ReceiveChunk(t, fileFirst, false) })
	}
}

func TestSEC030ReceiveCipherChunkOverTuningSize(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	sec030ReceiveDirectory(t)
	config := configuration.Get()
	originalChunkSize := config.ChunkSize
	config.ChunkSize = 1
	t.Cleanup(func() { config.ChunkSize = originalChunkSize })

	const plaintextSize = 1 << 20
	const maxFileSize = 2 << 20
	plaintext := bytes.Repeat([]byte("p"), plaintextSize)
	key := bytes.Repeat([]byte("s"), 32) // Synthetic test key, never a real key.
	// The real API supplies its own zero 12-byte GCM nonce internally, matching
	// the current CLI/WASM streaming encryption format.
	reader, err := encryption.GetEncryptReader(key, bytes.NewReader(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) <= plaintextSize || len(ciphertext) > maxFileSize {
		t.Fatalf("unexpected encrypted size: plaintext=%d ciphertext=%d", plaintextSize, len(ciphertext))
	}
	id := rand.Text()
	const user = 9130
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "encrypted.file")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write(ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, field := range [][2]string{
		{"dztotalfilesize", strconv.Itoa(len(ciphertext))},
		{"dzchunkbyteoffset", "0"},
		{"dzuuid", id},
	} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/uploadChunk", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", form.FormDataContentType())
	recorder := httptest.NewRecorder()
	code, err := ProcessNewChunk(recorder, request, false, "", user, maxFileSize)
	if err != nil || code != 0 {
		t.Fatalf("ciphertext was limited by plaintext chunk tuning: code=%d error=%v", code, err)
	}
	if recorder.Body.String() != `{"result":"OK"}` {
		t.Fatalf("encrypted chunk response changed: %q", recorder.Body.String())
	}
	assertReceivedChunkSession(t, chunksession.Scope{ClientID: id, UserID: user}, ciphertext)
}

func sec030ReceiveFourAvailable(t *testing.T) {
	t.Helper()
	const count = 4
	events := make(chan sec030ReceiveEvent, count*2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var workers sync.WaitGroup
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			body := &sec030ReceiveGateBody{id: id, events: events, release: release}
			_, err := ProcessNewChunk(httptest.NewRecorder(), sec030ReceiveEmptyRequest(context.Background(), body), true, "", 9130, 1024*1024)
			events <- sec030ReceiveEvent{id: id, err: err}
		}(i)
	}
	seen := make(map[int]bool, count)
	reads := 0
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	complete := false
	for len(seen) < count && !complete {
		select {
		case event := <-events:
			if seen[event.id] {
				continue
			}
			seen[event.id] = true
			if event.read {
				reads++
			}
		case <-timer.C:
			complete = true
			t.Error("receive probes remained blocked after client disconnect")
		}
	}
	unblock()
	workers.Wait()
	if reads != count {
		t.Errorf("client disconnect leaked receive slots: available=%d want=%d", reads, count)
	}
}

func sec030ReceivePayloadCount(t *testing.T, directory string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(directory, ".gokapi-receive-v1"))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "receive-") {
			count++
		}
	}
	return count
}

func TestSEC030ReceiveHTTPDisconnectCleanup(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	directory := sec030ReceiveDirectory(t)
	for _, entry := range []string{"chunk", "whole-file"} {
		for _, disconnect := range []string{"cancel-context", "close-pipe"} {
			t.Run(entry+"/"+disconnect, func(t *testing.T) {
				type outcome struct {
					err      error
					canceled bool
				}
				handled := make(chan outcome, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var err error
					if entry == "chunk" {
						_, err = ProcessNewChunk(w, r, true, "", 9130, 1024*1024)
					} else {
						err = ProcessCompleteFile(w, r, 9130, configuration.Get().MaxMemory)
					}
					handled <- outcome{err: err, canceled: r.Context().Err() != nil}
					if err != nil {
						w.WriteHeader(http.StatusBadRequest)
					}
				}))
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				pipeReader, pipeWriter := io.Pipe()
				transport := &http.Transport{DisableKeepAlives: true}
				defer func() {
					cancel()
					_ = pipeWriter.CloseWithError(io.ErrUnexpectedEOF)
					_ = pipeReader.Close()
					transport.CloseIdleConnections()
					server.CloseClientConnections()
					server.Close()
				}()
				form := multipart.NewWriter(pipeWriter)
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, pipeReader)
				if err != nil {
					t.Fatal(err)
				}
				request.Header.Set("Content-Type", form.FormDataContentType())
				clientDone := make(chan error, 1)
				go func() {
					response, err := (&http.Client{Transport: transport}).Do(request)
					if response != nil {
						_, _ = io.Copy(io.Discard, response.Body)
						_ = response.Body.Close()
					}
					clientDone <- err
				}()
				writerDone := make(chan error, 1)
				go func() {
					part, err := form.CreateFormFile("file", "synthetic-partial.bin")
					if err == nil {
						_, err = part.Write(bytes.Repeat([]byte("r"), 128*1024))
					}
					// Keep the multipart body open after the prefix. In particular,
					// no UUID/size fields or final boundary have been transmitted.
					writerDone <- err
				}()
				// Do not cancel before receive I/O starts: observe bytes in the
				// receiver's actual owned spool before interrupting the client.
				observed := false
				deadline := time.NewTimer(5 * time.Second)
				ticker := time.NewTicker(5 * time.Millisecond)
				defer deadline.Stop()
				defer ticker.Stop()
				for !observed {
					select {
					case <-ticker.C:
						entries, err := os.ReadDir(filepath.Join(directory, ".gokapi-receive-v1"))
						if os.IsNotExist(err) {
							continue
						}
						if err != nil {
							t.Fatal(err)
						}
						for _, candidate := range entries {
							if !strings.HasPrefix(candidate.Name(), "receive-") {
								continue
							}
							info, err := candidate.Info()
							if err != nil && os.IsNotExist(err) {
								continue
							}
							if err != nil {
								t.Fatal(err)
							}
							if info.Mode().IsRegular() && info.Size() > 0 {
								observed = true
							}
						}
					case result := <-handled:
						t.Fatalf("handler returned before real receive/spool activity: %v", result.err)
					case <-deadline.C:
						t.Fatal("no owned spool bytes observed before client disconnect")
					}
				}
				if disconnect == "cancel-context" {
					cancel()
					// Cancel also stops the local io.Pipe producer/reader. Otherwise
					// net/http may wait for its pending body read after the remote
					// connection has already observed cancellation.
					_ = pipeWriter.CloseWithError(context.Canceled)
					_ = pipeReader.CloseWithError(context.Canceled)
				} else {
					_ = pipeWriter.CloseWithError(io.ErrUnexpectedEOF)
				}
				select {
				case result := <-handled:
					if result.err == nil || !result.canceled {
						t.Fatalf("partial network upload did not fail with canceled context: error=%v canceled=%v", result.err, result.canceled)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("upload handler did not exit after client disconnect")
				}
				_ = pipeWriter.CloseWithError(io.ErrUnexpectedEOF)
				_ = pipeReader.CloseWithError(io.ErrUnexpectedEOF)
				select {
				case <-clientDone:
				case <-time.After(5 * time.Second):
					t.Fatal("client did not exit after disconnect")
				}
				select {
				case <-writerDone:
				case <-time.After(5 * time.Second):
					t.Fatal("multipart prefix writer did not exit")
				}
				if count := sec030ReceivePayloadCount(t, directory); count != 0 {
					t.Fatalf("disconnected upload left %d owned spool files", count)
				}
				sec030ReceiveFourAvailable(t)
			})
		}
	}
}

func TestSEC030ReceiveBusyHTTPDoesNotDrain(t *testing.T) {
	if !sec030ReceiveChild(t) {
		return
	}
	sec030ReceiveDirectory(t)
	const slots = 4
	events := make(chan sec030ReceiveEvent, slots*2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	var workers sync.WaitGroup
	defer func() {
		unblock()
		workers.Wait()
	}()
	for id := 0; id < slots; id++ {
		workers.Add(1)
		go func(id int) {
			defer workers.Done()
			body := &sec030ReceiveGateBody{id: id, events: events, release: release}
			request := sec030ReceiveEmptyRequest(context.Background(), body)
			_, err := ProcessNewChunk(httptest.NewRecorder(), request, true, "", 9130, 1024*1024)
			events <- sec030ReceiveEvent{id: id, err: err}
		}(id)
	}
	for range slots {
		select {
		case event := <-events:
			if !event.read {
				t.Fatalf("could not occupy all receive slots: %v", event.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("receive slot fixture did not enter its held body")
		}
	}
	type outcome struct {
		err    error
		status int
	}
	handlerExited := make(chan outcome, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := ProcessCompleteFile(w, r, 9130, configuration.Get().MaxMemory)
		status := ReceiveErrorStatus(err)
		if err == nil {
			status = http.StatusInternalServerError
		}
		http.Error(w, "synthetic receive rejection", status)
		handlerExited <- outcome{err: err, status: status}
	}))
	defer server.Close()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// Close the connection before server.Close on any failure. This also frees
	// an old server stuck draining the byte deliberately omitted below.
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	request := "POST /api/files/add HTTP/1.1\r\nHost: " + server.Listener.Addr().String() +
		"\r\nContent-Type: multipart/form-data; boundary=sec030-absent-body\r\nContent-Length: 1\r\n\r\n"
	// Do not send Connection: close or the declared body byte. The server must
	// terminate rejection itself, including net/http's post-handler drain.
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("busy HTTP request waited for its missing body byte: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests || (!response.Close && !strings.EqualFold(response.Header.Get("Connection"), "close")) {
		t.Fatalf("busy response did not close HTTP/1 connection: status=%d close=%v", response.StatusCode, response.Close)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("busy response did not finish within its deadline: %v", err)
	}
	if _, err := reader.Peek(1); err != io.EOF {
		t.Fatalf("busy HTTP connection was not closed: %v", err)
	}
	select {
	case result := <-handlerExited:
		if result.err == nil || result.status != http.StatusTooManyRequests {
			t.Fatalf("occupied receive pool did not reject handler: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("busy handler remained active without a request body")
	}
}
