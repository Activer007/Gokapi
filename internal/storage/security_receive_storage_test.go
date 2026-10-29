//go:build test && linux

package storage

import (
	"bytes"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/encryption"
)

// A completed encrypted work file must not rely on a future GC cycle to close
// its already-unlinked plaintext source and release the physical blocks.
func TestSEC030ReceiveEncryptedSourceClosed(t *testing.T) {
	config := configuration.Get()
	saved := *config
	defer func() { *config = saved }()
	config.DataDir = t.TempDir()
	config.MaxMemory = 1
	config.Encryption.Level = encryption.LocalEncryptionStored
	config.Encryption.Cipher = bytes.Repeat([]byte{0x47}, 32) // synthetic test key
	encryption.Init(*config)
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	const size = 9 * 1024 * 1024
	reader, _, file, _ := generateHashAndEncrypt(io.LimitReader(sec030ZeroReader{}, size), &multipart.FileHeader{Size: size})
	renamed := false
	defer deleteTempFile(file, &renamed)
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && strings.HasPrefix(target, config.DataDir+"/") && strings.HasSuffix(target, " (deleted)") {
			t.Error("encrypted staging retained an unlinked plaintext descriptor")
		}
	}
}

type sec030ZeroReader struct{}

func (sec030ZeroReader) Read(buffer []byte) (int, error) { clear(buffer); return len(buffer), nil }
