//go:build test && security_large && linux

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
)

// Explicit opt-in: real bytes over HTTP, disk spool, storage and read-back.
// The child owns all databases/files; it never scans or changes deployment data.
func TestSEC030ReceiveLargeFiles(t *testing.T) {
	const childEnv = "GOKAPI_SEC030_RECEIVE_LARGE_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, binary, "-test.run=^TestSEC030ReceiveLargeFiles$", "-test.v", "-test.timeout=11m")
		command.Dir = t.TempDir()
		command.Env = append(os.Environ(), childEnv+"=1")
		output, err := command.CombinedOutput()
		t.Logf("isolated large-file results:\n%s", output)
		if err != nil {
			t.Fatalf("large-file validation failed: %v", err)
		}
		return
	}
	database.Close()
	defer configuration.ConnectDatabase()
	stableDataDir := t.TempDir()
	for _, item := range []struct {
		mib       int64
		encrypted bool
	}{{1024, false}, {2048, true}} {
		t.Run(fmt.Sprintf("%dMiB-encrypted-%t", item.mib, item.encrypted), func(t *testing.T) {
			cfg := configuration.Get()
			saved := *cfg
			t.Cleanup(func() { *cfg = saved; filesystem.Init(saved.DataDir) })
			cfg.DataDir = stableDataDir
			cfg.MaxFileSizeMB = 3072
			// Deliberately large old tuning: server receipt and processing must
			// still stream instead of allocating the entire payload on the heap.
			cfg.MaxMemory = 4096
			cfg.Encryption.Level = encryption.NoEncryption
			if item.encrypted {
				cfg.Encryption.Level = encryption.LocalEncryptionStored
				cfg.Encryption.Cipher = make([]byte, 32)
				if _, err := rand.Read(cfg.Encryption.Cipher); err != nil {
					t.Fatal(err)
				}
				encryption.Init(*cfg)
			}
			filesystem.Init(cfg.DataDir)
			database.Connect(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "large.sqlite")})
			t.Cleanup(database.Close)
			user := models.User{Id: 9030, Name: "synthetic-large", UserLevel: models.UserLevelUser}
			key := models.ApiKey{Id: rand.Text(), UserId: user.Id, Permissions: models.ApiPermUpload}
			database.SaveUser(user, false)
			database.SaveApiKey(key)
			server := httptest.NewServer(http.HandlerFunc(Process))
			defer server.Close()
			payloadBytes := item.mib * 1024 * 1024
			metrics := &sec030ReceiveMetrics{root: cfg.DataDir}
			stop := make(chan struct{})
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				for {
					metrics.sample()
					select {
					case <-stop:
						return
					case <-ticker.C:
					}
				}
			}()
			defer func() { close(stop); <-finished }()
			reader, writer := io.Pipe()
			form := multipart.NewWriter(writer)
			sourceHash := sha256.New()
			written := make(chan error, 1)
			go func() {
				part, err := form.CreateFormFile("file", "synthetic-large.bin")
				if err == nil {
					_, err = io.CopyBuffer(io.MultiWriter(part, sourceHash), io.LimitReader(sec030PatternReader{}, payloadBytes), make([]byte, 64*1024))
				}
				if err == nil {
					err = form.WriteField("allowedDownloads", "2")
				}
				if err == nil {
					err = form.WriteField("expiryDays", "1")
				}
				if err == nil {
					err = form.Close()
				}
				_ = writer.CloseWithError(err)
				written <- err
			}()
			defer reader.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/files/add", reader)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.Header.Set("apikey", key.Id)
			started := time.Now()
			response, err := server.Client().Do(request)
			if err != nil {
				_ = reader.CloseWithError(err)
				t.Fatal(err)
			}
			defer response.Body.Close()
			var result struct {
				FileInfo  models.FileApiOutput
				ErrorCode int
			}
			if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(started)
			if response.StatusCode != http.StatusOK || result.FileInfo.Id == "" {
				t.Fatalf("upload failed: status=%d code=%d", response.StatusCode, result.ErrorCode)
			}
			if err := <-written; err != nil {
				t.Fatal(err)
			}
			file, ok := database.GetMetaDataById(result.FileInfo.Id)
			if !ok || file.SizeBytes != payloadBytes || file.Encryption.IsEncrypted != item.encrypted {
				t.Fatal("stored metadata differs")
			}
			blob, err := os.Open(filepath.Join(cfg.DataDir, file.SHA1))
			if err != nil {
				t.Fatal(err)
			}
			defer blob.Close()
			t.Cleanup(func() {
				if err := os.Remove(filepath.Join(cfg.DataDir, file.SHA1)); err != nil {
					t.Error(err)
				}
			})
			actualHash := sha256.New()
			if item.encrypted {
				err = encryption.DecryptReader(file.Encryption, blob, actualHash)
			} else {
				_, err = io.Copy(actualHash, blob)
			}
			if err != nil {
				t.Fatal(err)
			}
			if hex.EncodeToString(actualHash.Sum(nil)) != hex.EncodeToString(sourceHash.Sum(nil)) {
				t.Fatal("streamed read-back checksum differs")
			}
			metrics.sample()
			metrics.mu.Lock()
			defer metrics.mu.Unlock()
			if metrics.heap > 128*1024*1024 || metrics.rss > 256*1024*1024 {
				t.Fatalf("memory grew beyond streaming budget: heap=%d rss=%d", metrics.heap, metrics.rss)
			}
			if metrics.rss == 0 {
				t.Fatal("RSS measurement unavailable")
			}
			// Only numeric performance data and checksum, never credentials/URLs.
			t.Logf("RECEIVE_METRIC bytes=%d encrypted=%t seconds=%.3f MiBps=%.2f heap_peak=%d rss_peak=%d apparent_peak=%d allocated_peak=%d sha256=%s", payloadBytes, item.encrypted, elapsed.Seconds(), float64(item.mib)/elapsed.Seconds(), metrics.heap, metrics.rss, metrics.apparent, metrics.allocated, hex.EncodeToString(actualHash.Sum(nil)))
		})
	}
}

type sec030PatternReader struct{}

func (sec030PatternReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = byte(i % 251)
	}
	return len(p), nil
}

type sec030ReceiveMetrics struct {
	root                           string
	mu                             sync.Mutex
	heap, rss, apparent, allocated uint64
}

func (metrics *sec030ReceiveMetrics) sample() {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	var rss, apparent, allocated uint64
	if data, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					n, _ := strconv.ParseUint(fields[1], 10, 64)
					rss = n * 1024
				}
			}
		}
	}
	_ = filepath.WalkDir(metrics.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		apparent += uint64(info.Size())
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			allocated += uint64(stat.Blocks) * 512
		}
		return nil
	})
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.heap = max(metrics.heap, mem.Alloc)
	metrics.rss = max(metrics.rss, rss)
	metrics.apparent = max(metrics.apparent, apparent)
	metrics.allocated = max(metrics.allocated, allocated)
}
