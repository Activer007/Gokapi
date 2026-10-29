//go:build test && security_whole && !integration

package api

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/encryption"
)

// Match the WASM writer contract: retain the encryptor across calls, reset only
// its output buffer, then flush the final authentication bytes on Close. This
// exercises the actual cipher coordinates, without pretending to run a browser.
func TestSEC030ChunkE2ESegmentedWriter(t *testing.T) {
	if os.Getenv(sec030ChunkChildEnv) == "" {
		for _, backend := range []string{"sqlite", "redis"} {
			candidate := sec030ChunkCase{backend, "ordinary", "user", "finish", 1}
			t.Run(candidate.name(), func(t *testing.T) { sec030ChunkRunChild(t, "TestSEC030ChunkE2ESegmentedWriter", candidate) })
		}
		return
	}
	fixture := sec030FinishSetup(t, sec030ChunkSelected(t))
	configuration.Get().MaxFileSizeMB = 8 // Includes actual ciphertext overhead.
	configuration.Get().Encryption.Level = encryption.EndToEndEncryption
	plaintext := append(bytes.Repeat([]byte{0x31}, 2<<20), bytes.Repeat([]byte{0x72}, 2<<20)...)
	key := bytes.Repeat([]byte{0x19}, 32) // Synthetic fixture only.
	var output bytes.Buffer
	encryptor, err := encryption.GetEncryptWriter(key, &output)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_E2E_SETUP could not create the real stream writer")
	}
	parts := make([][]byte, 0, 3)
	for start := 0; start < len(plaintext); start += 2 << 20 {
		if _, err := encryptor.Write(plaintext[start : start+(2<<20)]); err != nil {
			t.Fatal("CHUNK_QUOTA_E2E_SETUP stream write failed")
		}
		parts = append(parts, bytes.Clone(output.Bytes()))
		output.Reset()
	}
	if err := encryptor.Close(); err != nil {
		t.Fatal("CHUNK_QUOTA_E2E_SETUP final stream flush failed")
	}
	parts = append(parts, bytes.Clone(output.Bytes()))
	fixture.content = bytes.Join(parts, nil)
	if int64(len(fixture.content)) != encryption.CalculateEncryptedFilesize(int64(len(plaintext))) || len(parts[2]) == 0 || len(parts[0]) == 2<<20 {
		t.Fatal("CHUNK_QUOTA_E2E_SETUP fixture failed to exercise buffered cipher boundaries")
	}
	var offset int64
	for _, part := range parts {
		response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, fixture.add(t, offset, part))
		if !sec030ChunkOK(response) {
			t.Fatalf("CHUNK_QUOTA_E2E_UPLOAD actual cipher range refused offset=%d status=%d code=%d", offset, response.status, response.ErrorCode)
		}
		offset += int64(len(part))
	}
	call := fixture.complete(false)
	call.headers["isE2E"], call.headers["realsize"] = "true", "7"
	response := sec030ChunkSend(context.Background(), fixture.client, fixture.url, call)
	if !sec030ChunkOK(response) {
		t.Fatalf("CHUNK_QUOTA_E2E_COMPLETE valid cipher completion failed status=%d code=%d", response.status, response.ErrorCode)
	}
	file := sec030FinishSettled(t, fixture)
	if file.Id != response.FileInfo.Id || !file.Encryption.IsEncrypted || !file.Encryption.IsEndToEndEncrypted || file.SizeBytes != offset || file.Size != "7 B" {
		t.Fatal("CHUNK_QUOTA_E2E_METADATA display plaintext claim changed cipher accounting or flags")
	}
	blob, err := os.Open(filepath.Join(configuration.Get().DataDir, file.SHA1))
	if err != nil {
		t.Fatal("CHUNK_QUOTA_E2E_READBACK cannot open the synthetic cipher blob")
	}
	defer blob.Close()
	reader, err := encryption.GetDecryptReader(key, blob)
	if err != nil {
		t.Fatal("CHUNK_QUOTA_E2E_READBACK cannot create a decryptor")
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, int64(len(plaintext))+1))
	if err != nil || !bytes.Equal(decoded, plaintext) {
		t.Fatal("CHUNK_QUOTA_E2E_READBACK authentication or exact plaintext readback failed")
	}
	if strconv.FormatInt(file.SizeBytes, 10) != call.headers["filesize"] {
		t.Fatal("CHUNK_QUOTA_E2E_ACCOUNT declared plaintext display reduced the wire byte charge")
	}
}
