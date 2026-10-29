//go:build test && security_whole && security_chunk && security_large && linux && !integration

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

const sec030ChunkLargeChild = "GOKAPI_SEC030_CHUNK_LARGE_CHILD"

// This opt-in gate sends real bytes over loopback HTTP, completes the managed
// chunk session, then hashes a streaming plaintext read-back. Each child owns a
// fresh database and DataDir; no deployment data is read, removed or filled.
func TestSEC030ChunkLargeFiles(t *testing.T) {
	if os.Getenv("GOKAPI_SEC030_CHUNK_LARGE") != "1" {
		t.Skip("CHUNK_LARGE_DISABLED explicit opt-in is required")
	}
	if os.Getenv(sec030ChunkLargeChild) == "" {
		for _, item := range []string{"ordinary-1024-plain", "fr-2048-encrypted"} {
			t.Run(item, func(t *testing.T) { sec030ChunkLargeRunChild(t, item) })
		}
		return
	}
	kind, mib, encrypted := "ordinary", int64(1024), false
	switch os.Getenv(sec030ChunkLargeChild) {
	case "ordinary-1024-plain":
	case "fr-2048-encrypted":
		kind, mib, encrypted = "fr", 2048, true
	default:
		t.Fatal("CHUNK_LARGE_SETUP invalid isolated case")
	}
	sec030WholeSetup(t, "sqlite")
	cfg := configuration.Get()
	cfg.MaxFileSizeMB, cfg.ChunkSize, cfg.MaxMemory = 3072, 8, 4096
	if encrypted {
		cfg.Encryption.Level = encryption.LocalEncryptionStored
		cfg.Encryption.Cipher = make([]byte, 32)
		if _, err := rand.Read(cfg.Encryption.Cipher); err != nil {
			t.Fatal("CHUNK_LARGE_SETUP cannot create synthetic encryption key")
		}
		encryption.Init(*cfg)
	}
	payloadBytes := mib * (1 << 20)
	// This is a conservative test preflight, not an atomic space allocator.
	// Leave at least 1GiB beyond the full 4x processing bound instead of filling
	// a runner disk to exercise failure. An opted-in blocked run must fail.
	free, err := helper.GetFreeSpace(cfg.DataDir)
	minimum := uint64(4*payloadBytes + (1 << 30))
	if err != nil || free < minimum {
		t.Fatalf("CHUNK_LARGE_BLOCKED insufficient isolated free space available=%d required=%d", free, minimum)
	}
	request, key := sec030ChunkRequestScope(t, sec030ChunkCase{"sqlite", kind, "user", "large", 1})
	if kind == "fr" {
		request.MaxSize = 3072
		database.SaveFileRequest(request)
	} else {
		key = sec030WholeKey(t)
	}
	server := httptest.NewServer(http.HandlerFunc(Process))
	defer server.Close()
	client := server.Client()
	client.Timeout = 6 * time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	uuid := rand.Text()
	if kind == "fr" {
		uuid = sec030ChunkReserveOne(t, client, server.URL, request, key)
	}
	metrics := &sec030ReceiveMetrics{root: cfg.DataDir}
	stop, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			metrics.sample()
			select {
			case <-stop:
				return
			case <-tick.C:
			}
		}
	}()
	defer func() { close(stop); <-finished }()
	sourceHash := sha256.New()
	started := time.Now()
	const chunkBytes int64 = 8 << 20
	for offset := int64(0); offset < payloadBytes; offset += chunkBytes {
		length := min(chunkBytes, payloadBytes-offset)
		response := sec030ChunkLargeSend(ctx, client, server.URL, kind, request.Id, key.Id, uuid, payloadBytes, offset, length, sourceHash)
		if !sec030ChunkOK(response) {
			t.Fatalf("CHUNK_LARGE_UPLOAD streamed chunk failed offset=%d status=%d code=%d", offset, response.status, response.ErrorCode)
		}
	}
	call := sec030ChunkCall{path: "/chunk/complete", key: key.Id, headers: map[string]string{
		"uuid": uuid, "filename": "synthetic-large.bin", "filesize": strconv.FormatInt(payloadBytes, 10),
		"contenttype": "application/octet-stream", "allowedDownloads": "0", "expiryDays": "0", "nonblocking": "false",
	}}
	if kind == "fr" {
		call.path, call.headers["fileRequestId"] = "/uploadrequest/chunk/complete", request.Id
	}
	response := sec030ChunkSend(ctx, client, server.URL, call)
	elapsed := time.Since(started)
	if !sec030ChunkOK(response) || response.FileInfo.Id == "" {
		t.Fatalf("CHUNK_LARGE_COMPLETE synchronous completion failed status=%d code=%d", response.status, response.ErrorCode)
	}
	file, found := database.GetMetaDataById(response.FileInfo.Id)
	if !found || len(database.GetAllMetadata()) != 1 || file.UserId != sec030WholeOwner || file.UploadRequestId != request.Id || file.SizeBytes != payloadBytes || file.Encryption.IsEncrypted != encrypted || file.Encryption.IsEndToEndEncrypted || file.AwsBucket != "" {
		t.Fatal("CHUNK_LARGE_METADATA completed metadata differs from the scoped upload")
	}
	decodedHash, err := hex.DecodeString(file.SHA1)
	if err != nil || len(decodedHash) != 20 || strings.ToLower(file.SHA1) != file.SHA1 {
		t.Fatal("CHUNK_LARGE_METADATA canonical identity is not a bounded local blob name")
	}
	ledger, err := database.ReadUploadQuota(ctx)
	if err != nil || len(ledger.Records) != 1 {
		t.Fatal("CHUNK_LARGE_LEDGER completed upload did not retain exactly one receipt")
	}
	for _, record := range ledger.Records {
		if record.FileID != file.Id || record.UserID != sec030WholeOwner || record.RequestID != request.Id || record.State != uploadquota.Committed || record.Bytes != payloadBytes || !record.StagingReleased || record.Completing {
			t.Fatal("CHUNK_LARGE_LEDGER completion did not commit once and release known staging")
		}
	}
	blob, err := os.Open(filepath.Join(cfg.DataDir, file.SHA1))
	if err != nil {
		t.Fatal("CHUNK_LARGE_READBACK cannot open the synthetic canonical blob")
	}
	defer blob.Close()
	if stat, err := blob.Stat(); err != nil || (encrypted && stat.Size() <= payloadBytes) || (!encrypted && stat.Size() != payloadBytes) {
		t.Fatal("CHUNK_LARGE_READBACK physical encryption/length differs from the selected path")
	}
	actualHash := sha256.New()
	input := sec030ChunkLargeContextReader{ctx: ctx, input: blob}
	if encrypted {
		err = encryption.DecryptReader(file.Encryption, input, actualHash)
	} else {
		_, err = io.CopyBuffer(actualHash, input, make([]byte, 64<<10))
	}
	if err != nil || ctx.Err() != nil || !strings.EqualFold(hex.EncodeToString(actualHash.Sum(nil)), hex.EncodeToString(sourceHash.Sum(nil))) {
		t.Fatal("CHUNK_LARGE_READBACK streamed SHA256 differs from the exact uploaded bytes")
	}
	sec030ChunkLargeClean(t, cfg.DataDir, file.SHA1)
	metrics.sample()
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if metrics.heap > 128<<20 || metrics.rss == 0 || metrics.rss > 256<<20 {
		t.Fatalf("CHUNK_LARGE_MEMORY streaming budget exceeded/unavailable heap=%d rss=%d", metrics.heap, metrics.rss)
	}
	if metrics.apparent > uint64(4*payloadBytes+(64<<20)) || metrics.allocated > uint64(4*payloadBytes+(64<<20)) {
		t.Fatal("CHUNK_LARGE_DISK processing exceeded the conservative staged-file bound")
	}
	// Only synthetic checksums and numeric measurements leave the child.
	t.Logf("CHUNK_LARGE_METRIC bytes=%d kind=%s encrypted=%t seconds=%.3f MiBps=%.2f heap_peak=%d rss_peak=%d apparent_peak=%d allocated_peak=%d sha256=%s", payloadBytes, kind, encrypted, elapsed.Seconds(), float64(mib)/elapsed.Seconds(), metrics.heap, metrics.rss, metrics.apparent, metrics.allocated, hex.EncodeToString(actualHash.Sum(nil)))
}

func sec030ChunkLargeRunChild(t *testing.T, item string) {
	t.Helper()
	work := t.TempDir()
	policy := map[string]string{
		"TMPDIR":                    work,
		"GOKAPI_SEC030_CHUNK_LARGE": "1", sec030ChunkLargeChild: item,
		"GOKAPI_UPLOAD_USER_TOTAL_MB": "5120", "GOKAPI_UPLOAD_USER_MAX_FILES": "100", "GOKAPI_UPLOAD_USER_ACTIVE": "4",
		"GOKAPI_UPLOAD_INSTANCE_TOTAL_MB": "20480", "GOKAPI_UPLOAD_INSTANCE_MAX_FILES": "100", "GOKAPI_UPLOAD_INSTANCE_ACTIVE": "4",
		"GOKAPI_UPLOAD_REQUEST_TOTAL_MB": "4096", "GOKAPI_UPLOAD_REQUEST_ACTIVE": "4", "GOKAPI_UPLOAD_STAGING_MB": "12288",
		"GOKAPI_UPLOAD_BODY_SLOTS": "4", "GOKAPI_UPLOAD_RECEIVE_MB": "128", "GOKAPI_MIN_FREE_SPACE": "64", "GOKAPI_MAX_SIZE_GUESTUPLOAD": "3072",
	}
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := policy[strings.ToUpper(name)]; !replaced {
			environment = append(environment, entry)
		}
	}
	for name, value := range policy {
		environment = append(environment, name+"="+value)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("CHUNK_LARGE_SETUP cannot locate the isolated executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestSEC030ChunkLargeFiles$", "-test.v", "-test.timeout=6m45s")
	command.Dir, command.Env = work, environment
	output, err := command.CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "CHUNK_LARGE_") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if err != nil {
		t.Fatal("CHUNK_LARGE_CHILD isolated streaming validation failed")
	}
}

func sec030ChunkLargeSend(ctx context.Context, client *http.Client, url, kind, requestID, key, uuid string, total, offset, length int64, source hash.Hash) sec030ChunkResponse {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	form := multipart.NewWriter(writer)
	written := make(chan error, 1)
	go func() {
		part, err := form.CreateFormFile("file", "synthetic-large.bin")
		if err == nil {
			var count int64
			count, err = io.CopyBuffer(io.MultiWriter(part, source), io.LimitReader(sec030PatternReader{}, length), make([]byte, 64<<10))
			if err == nil && count != length {
				err = io.ErrUnexpectedEOF
			}
		}
		if err == nil {
			for _, field := range [][2]string{{"uuid", uuid}, {"filesize", strconv.FormatInt(total, 10)}, {"offset", strconv.FormatInt(offset, 10)}} {
				if err = form.WriteField(field[0], field[1]); err != nil {
					break
				}
			}
		}
		if err == nil {
			err = form.Close()
		}
		_ = writer.CloseWithError(err)
		written <- err
	}()
	path := "/chunk/add"
	if kind == "fr" {
		path = "/uploadrequest/chunk/add"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+path, reader)
	result := sec030ChunkResponse{}
	if err == nil {
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.Header.Set("apikey", key)
		if kind == "fr" {
			request.Header.Set("fileRequestId", requestID)
		}
		var response *http.Response
		response, err = client.Do(request)
		if err == nil {
			result.status = response.StatusCode
			decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
			err = decoder.Decode(&result)
			if err == nil && decoder.Decode(new(any)) != io.EOF {
				err = fmt.Errorf("invalid bounded JSON response")
			}
			err = errors.Join(err, response.Body.Close())
		}
	}
	_ = reader.CloseWithError(io.ErrClosedPipe)
	select {
	case producerErr := <-written:
		result.failed = err != nil || producerErr != nil
	case <-time.After(5 * time.Second):
		result.failed = true
	}
	return result
}

type sec030ChunkLargeContextReader struct {
	ctx   context.Context
	input io.Reader
}

func (reader sec030ChunkLargeContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.input.Read(data)
}

func sec030ChunkLargeClean(t *testing.T, root, canonical string) {
	t.Helper()
	entries := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entries++
		if entries > 16 || entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected isolated namespace entry")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." || relative == ".gokapi-chunks-v1" || relative == ".gokapi-receive-v1" {
			if !entry.IsDir() {
				return fmt.Errorf("expected owned directory")
			}
			return nil
		}
		if !entry.Type().IsRegular() || (relative != canonical && relative != ".gokapi-chunks-v1/.owner" && relative != ".gokapi-receive-v1/.owner") {
			return fmt.Errorf("released staging still contains owned work")
		}
		return nil
	})
	if err != nil {
		t.Fatal("CHUNK_LARGE_CLEANUP committed upload left unknown input, manifest or processing work")
	}
}
