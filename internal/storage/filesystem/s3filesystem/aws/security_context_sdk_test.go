//go:build test && !noaws && !awsmock

package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/models"
)

// The real SDK is used only against a synthetic loopback endpoint. None of
// these tests loads an operator's AWS profile or contacts an external service.
func TestSEC030AWSSDKContextLoopback(t *testing.T) {
	t.Setenv("AWS_SDK_LOAD_CONFIG", "0")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent-credentials"))
	var stored []byte
	var mutex sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut:
			data, err := io.ReadAll(io.LimitReader(r.Body, 65))
			if err != nil || len(data) > 64 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mutex.Lock()
			stored = data
			mutex.Unlock()
			w.Header().Set("ETag", "\"synthetic-etag\"")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/missing"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/denied"):
			w.WriteHeader(http.StatusForbidden)
		case r.Method == http.MethodHead:
			mutex.Lock()
			length := len(stored)
			mutex.Unlock()
			w.Header().Set("Content-Length", strconv.Itoa(length))
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(server.Close)
	saved := awsConfig
	awsConfig = models.AwsConfig{Endpoint: server.URL, Region: "synthetic-1", Bucket: "synthetic-bucket", KeyId: "synthetic-id", KeySecret: "synthetic-secret"}
	t.Cleanup(func() { awsConfig = saved })
	file := models.File{AwsBucket: "synthetic-bucket", SHA1: "present"}
	payload := []byte("synthetic-object")
	if _, err := UploadContext(context.Background(), bytes.NewReader(payload), file); err != nil {
		t.Fatal("loopback SDK upload failed")
	}
	mutex.Lock()
	actual := append([]byte(nil), stored...)
	mutex.Unlock()
	if !bytes.Equal(actual, payload) {
		t.Fatal("loopback SDK upload did not deliver the expected body")
	}
	if exists, size, err := FileExistsContext(context.Background(), file); err != nil || !exists || size != int64(len(payload)) {
		t.Fatal("loopback HEAD did not verify the actual object length")
	}
	file.SHA1 = "missing"
	if exists, size, err := FileExistsContext(context.Background(), file); err != nil || exists || size != 0 {
		t.Fatal("loopback missing object was not distinguished from an error")
	}
	file.SHA1 = "denied"
	if exists, size, err := FileExistsContext(context.Background(), file); err == nil || exists || size != 0 {
		t.Fatal("loopback denied HEAD was treated as verified existence")
	}
}

func TestSEC030AWSSDKContextCancelsRequests(t *testing.T) {
	t.Setenv("AWS_SDK_LOAD_CONFIG", "0")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent-credentials"))
	for _, method := range []string{http.MethodHead, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			arrived := make(chan struct{}, 1)
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != method {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if r.Method == http.MethodPut {
					_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 65))
				}
				select {
				case arrived <- struct{}{}:
				default:
				}
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			t.Cleanup(func() { close(release); server.Close() })
			saved := awsConfig
			awsConfig = models.AwsConfig{Endpoint: server.URL, Region: "synthetic-1", Bucket: "synthetic-bucket", KeyId: "synthetic-id", KeySecret: "synthetic-secret"}
			t.Cleanup(func() { awsConfig = saved })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				file := models.File{AwsBucket: "synthetic-bucket", SHA1: "synthetic-key"}
				if method == http.MethodHead {
					_, _, err := FileExistsContext(ctx, file)
					finished <- err
				} else {
					_, err := UploadContext(ctx, bytes.NewReader([]byte("abc")), file)
					finished <- err
				}
			}()
			select {
			case <-arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("SDK request did not reach the synthetic endpoint")
			}
			cancel()
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Error("SDK request did not preserve caller cancellation")
				}
			case <-time.After(3 * time.Second):
				t.Error("SDK request did not exit after caller cancellation")
			}
		})
	}
}
