//go:build test && !noaws && !awsmock

package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
)

// All requests use the production SDK/presigner and disposable loopback HTTP.
// No awsmock driver, cloud profile or external object store is involved.
func m5DownloadSDKEndpoint(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Setenv("AWS_SDK_LOAD_CONFIG", "0")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent-credentials"))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	saved := awsConfig
	awsConfig = models.AwsConfig{Endpoint: server.URL, Region: "synthetic-1", Bucket: "synthetic-bucket", KeyId: "synthetic-id", KeySecret: "synthetic-secret", ProxyDownload: true}
	t.Cleanup(func() { awsConfig = saved })
}

func m5DownloadSDKFile(size int64) models.File {
	return models.File{Id: "m5-sdk-download", AwsBucket: "synthetic-bucket", SHA1: "synthetic-object", Name: "synthetic.bin", ContentType: "application/octet-stream", SizeBytes: size}
}

func m5DownloadSDKBody(t *testing.T, payload []byte, mode string) {
	t.Helper()
	m5DownloadSDKEndpoint(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body := payload
		if mode == "status" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write(bytes.Repeat([]byte("e"), len(payload)))
			return
		}
		if mode == "short-eof" || mode == "chunked-short" || mode == "truncated" {
			body = body[:len(body)/2]
		}
		if strings.HasPrefix(mode, "chunked") {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
		} else if mode == "truncated" {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		_, _ = w.Write(body)
	})
}

func TestM5SDKDownloadProxyLoopback(t *testing.T) {
	for _, mode := range []string{"healthy", "empty", "chunked-healthy", "status", "short-eof", "chunked-short", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			payload := bytes.Repeat([]byte("synthetic proxy payload"), 256)
			if mode == "empty" {
				payload = nil
			}
			m5DownloadSDKBody(t, payload, mode)
			response := httptest.NewRecorder()
			blocking, err := ServeFile(response, httptest.NewRequest(http.MethodGet, "/download", nil), m5DownloadSDKFile(int64(len(payload))), true, false)
			fault := mode == "status" || mode == "short-eof" || mode == "chunked-short" || mode == "truncated"
			if !blocking || (err != nil) != fault {
				t.Fatalf("M5_SDK_PROXY mode=%s blocking=%v has_error=%v, want blocking=true error=%v", mode, blocking, err != nil, fault)
			}
			if !fault && !bytes.Equal(response.Body.Bytes(), payload) {
				t.Fatal("M5_SDK_CONTROL proxy bytes differ")
			}
			if (mode == "status" || mode == "short-eof") && response.Body.Len() != 0 {
				t.Fatal("M5_SDK_PROXY invalid upstream representation reached attachment output")
			}
		})
	}
}

func TestM5SDKDownloadStreamLoopback(t *testing.T) {
	for _, mode := range []string{"healthy", "empty", "chunked-healthy", "short-eof", "chunked-short", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			payload := bytes.Repeat([]byte("synthetic SDK stream"), 256)
			if mode == "empty" {
				payload = nil
			}
			m5DownloadSDKBody(t, payload, mode)
			var output bytes.Buffer
			err := Stream(&output, m5DownloadSDKFile(int64(len(payload))))
			fault := mode == "short-eof" || mode == "chunked-short" || mode == "truncated"
			if (err != nil) != fault {
				t.Fatalf("M5_SDK_STREAM mode=%s has_error=%v, want %v", mode, err != nil, fault)
			}
			if !fault && !bytes.Equal(output.Bytes(), payload) {
				t.Fatal("M5_SDK_CONTROL stream bytes differ")
			}
		})
	}
}

// A synthetic downstream writer failure must remain observable to storage.
type m5SDKFailedWriter struct {
	*httptest.ResponseRecorder
	cause error
}

func (w *m5SDKFailedWriter) Write(p []byte) (int, error) { return 0, w.cause }

func TestM5SDKDownloadOutputError(t *testing.T) {
	payload := bytes.Repeat([]byte("write failure"), 256)
	m5DownloadSDKBody(t, payload, "healthy")
	cause := errors.New("synthetic downstream failure")
	writer := &m5SDKFailedWriter{httptest.NewRecorder(), cause}
	_, err := ServeFile(writer, httptest.NewRequest(http.MethodGet, "/download", nil), m5DownloadSDKFile(int64(len(payload))), true, false)
	if !errors.Is(err, cause) {
		t.Fatal("M5_SDK_WRITE downstream error was discarded")
	}
}

func TestM5SDKDownloadCancellationLoopback(t *testing.T) {
	for _, decrypt := range []bool{false, true} {
		t.Run(strconv.FormatBool(decrypt), func(t *testing.T) {
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
					t.Error("M5_SDK_DRAIN download did not finish after fixture release")
				}
			})
			result := make(chan error, 1)
			go func() {
				defer close(done)
				_, err := ServeFile(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/download", nil).WithContext(ctx), m5DownloadSDKFile(4096), true, decrypt)
				result <- err
			}()
			select {
			case <-arrived:
			case <-time.After(3 * time.Second):
				t.Fatal("M5_SDK_SETUP GET never reached loopback")
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Error("M5_SDK_CANCEL did not preserve request cancellation")
				}
			case <-time.After(time.Second):
				t.Error("M5_SDK_CANCEL canceled download remained blocked on upstream")
			}
			select {
			case <-upstreamCanceled:
			case <-time.After(time.Second):
				t.Error("M5_SDK_CANCEL upstream HTTP request was not canceled")
			}
		})
	}
}

// Encryption's master has no reset API. Keep the synthetic key and all encrypted
// controls in a child process rather than changing another test's live master.
func TestM5SDKDownloadEncryptedLoopback(t *testing.T) {
	const childEnv = "SUPASHARE_M5_SDK_ENCRYPTED_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestM5SDKDownloadEncryptedLoopback$", "-test.count=1", "-test.v")
		command.Dir = t.TempDir()
		command.Env = append(os.Environ(), childEnv+"=1")
		output, err := command.CombinedOutput()
		for _, line := range strings.Split(string(output), "\n") {
			if strings.Contains(line, "M5_SDK_") {
				t.Log(strings.TrimSpace(line))
			}
		}
		if err != nil || !bytes.Contains(output, []byte("M5_SDK_ENCRYPTED_OK")) {
			t.Fatal("M5_SDK_ENCRYPTED isolated contracts failed")
		}
		return
	}
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.FullEncryptionStored, Cipher: master}})
	plaintext := bytes.Repeat([]byte("synthetic authenticated stream!"), 6000)
	var info models.EncryptionInfo
	var ciphertext bytes.Buffer
	if err := encryption.Encrypt(&info, bytes.NewReader(plaintext), &ciphertext); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"decrypted", "stream", "proxy-cipher", "e2e-proxy", "tampered", "short-eof", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			body := append([]byte(nil), ciphertext.Bytes()...)
			if mode == "tampered" {
				body[len(body)-1] ^= 0x80
			}
			fault := mode == "tampered" || mode == "short-eof" || mode == "truncated"
			serverMode := "healthy"
			if mode == "short-eof" || mode == "truncated" {
				serverMode = mode
			}
			m5DownloadSDKBody(t, body, serverMode)
			file := m5DownloadSDKFile(int64(len(plaintext)))
			file.Encryption = info
			if mode == "e2e-proxy" {
				file.Encryption.IsEndToEndEncrypted = true
				file.SizeBytes = int64(len(body))
			}
			response := httptest.NewRecorder()
			var downloadErr error
			if mode == "stream" {
				downloadErr = Stream(response, file)
			} else {
				_, downloadErr = ServeFile(response, httptest.NewRequest(http.MethodGet, "/download", nil), file, true, mode != "proxy-cipher" && mode != "e2e-proxy")
			}
			if (downloadErr != nil) != fault {
				t.Fatalf("M5_SDK_ENCRYPTED mode=%s error=%v, want %v", mode, downloadErr != nil, fault)
			}
			if !fault {
				want := plaintext
				if mode == "proxy-cipher" || mode == "e2e-proxy" {
					want = body
				}
				if !bytes.Equal(response.Body.Bytes(), want) {
					t.Fatal("M5_SDK_ENCRYPTED healthy exact bytes differ")
				}
			}
			t.Logf("M5_SDK_ENCRYPTED_CASE mode=%s error=%v bytes=%d", mode, downloadErr != nil, response.Body.Len())
		})
	}
	for _, size := range []int{0, 1, 65536} {
		t.Run("boundary-"+strconv.Itoa(size), func(t *testing.T) {
			plain := bytes.Repeat([]byte{0x61}, size)
			var info models.EncryptionInfo
			var encoded bytes.Buffer
			if err := encryption.Encrypt(&info, bytes.NewReader(plain), &encoded); err != nil {
				t.Fatal(err)
			}
			m5DownloadSDKBody(t, encoded.Bytes(), "healthy")
			file := m5DownloadSDKFile(int64(size))
			file.Encryption = info
			var output bytes.Buffer
			if err := Stream(&output, file); err != nil || !bytes.Equal(output.Bytes(), plain) {
				t.Fatal("M5_SDK_BOUNDARY stream output differs")
			}
			response := httptest.NewRecorder()
			if _, err := ServeFile(response, httptest.NewRequest(http.MethodGet, "/download", nil), file, true, true); err != nil || !bytes.Equal(response.Body.Bytes(), plain) {
				t.Fatal("M5_SDK_BOUNDARY decrypted output differs")
			}
			t.Logf("M5_SDK_ENCRYPTED_BOUNDARY plaintext=%d ciphertext=%d", size, encoded.Len())
		})
	}
	t.Log("M5_SDK_ENCRYPTED_OK")
}

var _ io.Writer = (*m5SDKFailedWriter)(nil)
