//go:build test && !noaws && !awsmock

package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
)

const fix03RemoteChild = "SUPASHARE_FIX03_REMOTE_CHILD"
const fix03RemotePassed = "FIX03_REMOTE_TERMINAL_OK"

// Exercise SDK errors through storage and an actual HTTP/1 client, rather than
// inferring the public response from the SDK's return value or a Recorder.
// Encryption's global master and the package TestMain stay in a disposable child.
func TestFIX03RemoteDownloadTerminal(t *testing.T) {
	if os.Getenv(fix03RemoteChild) == "1" {
		fix03RemoteDownloadTerminal(t)
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pattern := "^TestFIX03RemoteDownloadTerminal$"
	if selected := flag.Lookup("test.run"); selected != nil {
		if _, subtest, found := strings.Cut(selected.Value.String(), "/"); found {
			pattern += "/" + subtest
		}
	}
	child := exec.CommandContext(ctx, binary, "-test.run="+pattern, "-test.count=1", "-test.v")
	child.Dir = t.TempDir()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if upper != fix03RemoteChild && upper != "REAL_AWS_CREDENTIALS" && !strings.HasPrefix(upper, "GOKAPI_") && !strings.HasPrefix(upper, "AWS_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, fix03RemoteChild+"=1", "REAL_AWS_CREDENTIALS=false", "AWS_SDK_LOAD_CONFIG=0", "AWS_EC2_METADATA_DISABLED=true")
	output, childErr := child.CombinedOutput()
	t.Logf("isolated synthetic remote terminal contract:\n%s", output)
	if childErr != nil || !bytes.Contains(output, []byte(fix03RemotePassed)) {
		t.Fatal("FIX03_REMOTE child contract failed")
	}
}

type fix03RemoteObject struct {
	file       models.File
	plaintext  []byte
	data       []byte // Immutable: faults affect the response, not this source.
	mode       string
	gets       atomic.Int64
	arrived    chan struct{}
	canceled   chan struct{}
	release    chan struct{}
	arriveOne  sync.Once
	cancelOne  sync.Once
	releaseOne sync.Once
	released   atomic.Bool
}

func fix03MakeRemoteObject(t *testing.T, plaintext []byte, encrypted, e2e, tampered bool, mode string) *fix03RemoteObject {
	t.Helper()
	id := rand.Text()
	object := &fix03RemoteObject{file: models.File{
		Id: id, UserId: 5, Name: "synthetic-remote-" + id + ".bin", SHA1: "synthetic-" + id,
		AwsBucket: "synthetic-bucket", SizeBytes: int64(len(plaintext)), ContentType: "application/octet-stream",
		UploadDate: time.Now().Unix(), UnlimitedTime: true, UnlimitedDownloads: true,
	}, plaintext: bytes.Clone(plaintext), data: bytes.Clone(plaintext), mode: mode,
		arrived: make(chan struct{}), canceled: make(chan struct{}), release: make(chan struct{})}
	if encrypted || e2e {
		var ciphertext bytes.Buffer
		if err := encryption.Encrypt(&object.file.Encryption, bytes.NewReader(plaintext), &ciphertext); err != nil {
			t.Fatal(err)
		}
		object.data = bytes.Clone(ciphertext.Bytes())
		if tampered {
			object.data[len(object.data)-1] ^= 0x80 // Same size, valid wrapping, bad final authentication.
		}
		if e2e {
			// The synthetic bytes are opaque to this raw-transfer control. This
			// proves the E2E flag/size contract, not a browser crypto protocol.
			object.file.Encryption.IsEndToEndEncrypted = true
			object.file.SizeBytes = int64(len(object.data))
		}
	}
	database.SaveMetaData(object.file)
	t.Cleanup(func() { downloadstatus.SetAllComplete(object.file.Id) })
	return object
}

func (object *fix03RemoteObject) unblock() { object.releaseOne.Do(func() { close(object.release) }) }

func fix03RemoteOrigin(t *testing.T, objects []*fix03RemoteObject) {
	t.Helper()
	byKey := make(map[string]*fix03RemoteObject)
	for _, object := range objects {
		byKey[object.file.SHA1] = object
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// aws.Init verifies fixed credentials through HEAD of an absent key.
		// It must be a real 404, not an invalid Content-Length success.
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		object := byKey[path.Base(r.URL.Path)]
		if r.Method != http.MethodGet || object == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		object.gets.Add(1)
		body := object.data
		w.Header().Set("Content-Type", "application/octet-stream")
		switch object.mode {
		case "status":
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(bytes.Repeat([]byte("!"), len(body)))
			return
		case "short-header", "chunked-short", "disconnect":
			body = body[:len(body)/2]
		}
		if object.mode == "chunked-short" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
		} else if object.mode == "disconnect" || object.mode == "blocked" {
			w.Header().Set("Content-Length", strconv.Itoa(len(object.data)))
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		if object.mode != "blocked" {
			_, _ = w.Write(body)
			return
		}
		_, _ = w.Write(body[:65536])
		w.(http.Flusher).Flush()
		object.arriveOne.Do(func() { close(object.arrived) })
		select {
		case <-r.Context().Done():
			object.cancelOne.Do(func() { close(object.canceled) })
		case <-object.release:
			object.released.Store(true)
		}
	}))
	t.Cleanup(func() {
		for _, object := range objects {
			object.unblock()
		}
		server.Close()
		aws.LogOut()
	})
	if !aws.Init(models.AwsConfig{
		Endpoint: server.URL, Region: "synthetic-1", Bucket: "synthetic-bucket",
		KeyId: "synthetic-id", KeySecret: "synthetic-secret", ProxyDownload: true,
	}) {
		t.Fatal("FIX03_REMOTE_SETUP synthetic loopback SDK initialization failed")
	}
}

func fix03CheckRemoteSource(t *testing.T, object *fix03RemoteObject, original []byte) {
	t.Helper()
	stored, found := database.GetMetaDataById(object.file.Id)
	if !found || !reflect.DeepEqual(stored, object.file) || !bytes.Equal(object.data, original) {
		t.Error("FIX03_REMOTE_SOURCE download changed metadata or the origin object")
	}
}

func fix03RemoteDownloadTerminal(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent-credentials"))
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.FullEncryptionStored, Cipher: master}})
	serverstats.AddTraffic(0) // Prime before asynchronous workers can request a SQLite save.
	payload := bytes.Repeat([]byte("synthetic remote authenticated payload;"), 5000)
	for _, tc := range []struct {
		name, mode                      string
		encrypted, e2e, archive, broken bool
		preheader, failure, cancel      bool
		empty                           bool
	}{
		{name: "proxy-healthy"},
		{name: "proxy-empty", empty: true},
		{name: "proxy-status-503", mode: "status", preheader: true, failure: true},
		{name: "proxy-short-header", mode: "short-header", preheader: true, failure: true},
		{name: "proxy-chunked-short", mode: "chunked-short", failure: true},
		{name: "proxy-content-length-disconnect", mode: "disconnect", failure: true},
		{name: "forced-encrypted-healthy", encrypted: true},
		{name: "forced-encrypted-empty", encrypted: true, empty: true},
		{name: "forced-encrypted-tail-authentication", encrypted: true, broken: true, failure: true},
		{name: "zip-healthy", encrypted: true, archive: true},
		{name: "zip-later-remote-tail-authentication", encrypted: true, archive: true, broken: true, failure: true},
		{name: "e2e-raw-single", e2e: true},
		{name: "e2e-zip-rejected", e2e: true, archive: true, failure: true},
		{name: "proxy-client-cancel", mode: "blocked", failure: true, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			casePayload := payload
			if tc.empty {
				casePayload = nil
			}
			target := fix03MakeRemoteObject(t, casePayload, tc.encrypted, tc.e2e, tc.broken, tc.mode)
			objects := []*fix03RemoteObject{target}
			if tc.archive {
				healthy := fix03MakeRemoteObject(t, []byte("healthy earlier remote member"), false, false, false, "")
				objects = []*fix03RemoteObject{healthy, target}
			}
			files := make([]models.File, len(objects))
			originals := make([][]byte, len(objects))
			for i, object := range objects {
				files[i], originals[i] = object.file, bytes.Clone(object.data)
			}
			fix03RemoteOrigin(t, objects)
			var cancelObserved atomic.Bool
			var watchDone chan struct{}
			if tc.cancel {
				watchDone = make(chan struct{})
				go func() {
					defer close(watchDone)
					select {
					case <-target.arrived:
					case <-t.Context().Done():
						return
					}
					// The actual client times out after three seconds. This later
					// release only drains an old implementation; it never counts
					// as evidence that cancellation reached the origin.
					deadline := time.NewTimer(4 * time.Second)
					defer deadline.Stop()
					select {
					case <-target.canceled:
						cancelObserved.Store(true)
					case <-deadline.C:
						target.unblock()
					}
				}()
				t.Cleanup(func() { target.unblock(); <-watchDone })
			}
			result := fix03ServeHTTP(t, files, tc.archive, http.MethodGet, "", "", nil, true)
			if tc.e2e && tc.archive {
				if target.gets.Load() != 0 {
					t.Error("FIX03_REMOTE_E2E archive attempted to decrypt an E2E object")
				}
			} else if target.gets.Load() == 0 {
				t.Fatal("FIX03_REMOTE_SETUP no real SDK/proxy GET reached the origin")
			}
			if tc.preheader {
				if !result.returned || result.panicValue != nil || result.status != http.StatusInternalServerError || result.disposition != "" || result.doErr != nil || result.readErr != nil {
					t.Errorf("FIX03_REMOTE_PREHEADER invalid representation did not become a plain 500: status=%d attachment=%q returned=%v panic=%v", result.status, result.disposition, result.returned, result.panicValue)
				}
				if !downloadstatus.IsCurrentlyDownloading(target.file) {
					t.Error("FIX03_REMOTE_STATUS rejected transfer was marked complete")
				}
			} else if tc.failure {
				fix03RequireAbort(t, result, files)
				if !tc.archive && (len(result.body) == 0 || len(result.body) >= len(payload) || !bytes.Equal(result.body, payload[:len(result.body)])) {
					t.Error("FIX03_REMOTE_HTTP fault did not follow a real bounded plaintext prefix")
				}
			} else if !result.returned || result.panicValue != nil || result.status != http.StatusOK || result.doErr != nil || result.readErr != nil || result.disposition == "" {
				t.Errorf("FIX03_REMOTE_CONTROL healthy transfer failed: status=%d do=%v read=%v", result.status, result.doErr, result.readErr)
			} else if tc.archive {
				archive, err := zip.NewReader(bytes.NewReader(result.body), int64(len(result.body)))
				if err != nil || len(archive.File) != len(objects) {
					t.Fatal("FIX03_REMOTE_CONTROL healthy ZIP central directory is invalid", err)
				}
				for i, object := range objects {
					reader, err := archive.File[i].Open()
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(reader)
					closeErr := reader.Close()
					if readErr != nil || closeErr != nil || archive.File[i].Name != object.file.Name || !bytes.Equal(data, object.plaintext) {
						t.Error("FIX03_REMOTE_CONTROL healthy ZIP member bytes or CRC differ")
					}
				}
			} else {
				want := casePayload
				if tc.e2e {
					want = target.data
				}
				if !bytes.Equal(result.body, want) {
					t.Error("FIX03_REMOTE_CONTROL healthy full bytes differ")
				}
			}
			if tc.cancel {
				// The watcher may wake concurrently with handler unwind; join
				// its event rather than waiting for an arbitrary settling sleep.
				deadline := time.NewTimer(2 * time.Second)
				defer deadline.Stop()
				select {
				case <-watchDone:
				case <-deadline.C:
					t.Error("FIX03_REMOTE_CANCEL cancellation did not reach the blocked upstream")
				}
				if !cancelObserved.Load() || target.released.Load() {
					t.Error("FIX03_REMOTE_CANCEL fixture escape was used instead of real upstream cancellation")
				}
			}
			for i, object := range objects {
				if !tc.failure && downloadstatus.IsCurrentlyDownloading(object.file) {
					t.Error("FIX03_REMOTE_CONTROL healthy transfer did not complete")
				}
				fix03CheckRemoteSource(t, object, originals[i])
			}
		})
	}
	if !t.Failed() {
		t.Log(fix03RemotePassed)
	}
}
