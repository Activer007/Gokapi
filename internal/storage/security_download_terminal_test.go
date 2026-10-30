//go:build test && noaws

package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
)

const fix03TerminalChild = "SUPASHARE_FIX03_TERMINAL_CHILD"
const fix03TerminalPassed = "FIX03_LOCAL_TERMINAL_OK"

// The master cipher cannot be reset. Keep it, download guards, and the package's
// database fixture in a child, using only synthetic files and loopback HTTP/1.
func TestFIX03LocalDownloadTerminal(t *testing.T) {
	if os.Getenv(fix03TerminalChild) == "1" {
		fix03LocalDownloadTerminal(t)
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	pattern := "^TestFIX03LocalDownloadTerminal$"
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
		if upper != fix03TerminalChild && upper != "REAL_AWS_CREDENTIALS" && !strings.HasPrefix(upper, "GOKAPI_") {
			child.Env = append(child.Env, entry)
		}
	}
	child.Env = append(child.Env, fix03TerminalChild+"=1", "REAL_AWS_CREDENTIALS=false")
	output, childErr := child.CombinedOutput()
	// All output comes from this test's synthetic child, never operator data.
	t.Logf("isolated local terminal contract:\n%s", output)
	if childErr != nil || !bytes.Contains(output, []byte(fix03TerminalPassed)) {
		t.Fatal("FIX03_LOCAL child contract failed")
	}
}

type fix03LocalFile struct {
	file            models.File
	path            string
	plaintext, disk []byte
}

func fix03MakeLocalFile(t *testing.T, plaintext []byte, encrypted bool) fix03LocalFile {
	t.Helper()
	id := rand.Text()
	locator := sha1.Sum([]byte("fix03-synthetic-" + id))
	fixture := fix03LocalFile{file: models.File{
		Id: id, UserId: 5, Name: "synthetic-" + id + ".bin", SHA1: hex.EncodeToString(locator[:]),
		SizeBytes: int64(len(plaintext)), ContentType: "application/octet-stream", UploadDate: time.Now().Unix(),
		UnlimitedDownloads: true, UnlimitedTime: true,
	}, plaintext: bytes.Clone(plaintext), disk: bytes.Clone(plaintext)}
	if encrypted {
		var ciphertext bytes.Buffer
		if err := encryption.Encrypt(&fixture.file.Encryption, bytes.NewReader(plaintext), &ciphertext); err != nil {
			t.Fatal("FIX03_SETUP real encryption failed", err)
		}
		fixture.disk = bytes.Clone(ciphertext.Bytes())
	}
	fixture.path = filepath.Join(configuration.Get().DataDir, fixture.file.SHA1)
	if err := os.WriteFile(fixture.path, fixture.disk, 0600); err != nil {
		t.Fatal(err)
	}
	database.SaveMetaData(fixture.file)
	t.Cleanup(func() {
		downloadstatus.SetAllComplete(fixture.file.Id)
		_ = os.Remove(fixture.path)
	})
	return fixture
}

func fix03CheckSource(t *testing.T, fixture fix03LocalFile, expected []byte, missing bool) {
	t.Helper()
	stored, found := database.GetMetaDataById(fixture.file.Id)
	if !found || !reflect.DeepEqual(stored, fixture.file) {
		t.Error("FIX03_SOURCE download changed source metadata")
	}
	data, err := os.ReadFile(fixture.path)
	if missing {
		if !os.IsNotExist(err) {
			t.Error("FIX03_SOURCE download recreated a missing source")
		}
	} else if err != nil || !bytes.Equal(data, expected) {
		t.Error("FIX03_SOURCE download changed bytes beyond the injected fault")
	}
}

func fix03LocalDownloadTerminal(t *testing.T) {
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.LocalEncryptionStored, Cipher: master}})
	// Prime the real timestamp synchronously so no finite traffic worker starts
	// a delayed SQLite write during TestMain teardown. Post-header calls also
	// wait for their exact traffic increment in fix03ServeHTTP.
	serverstats.AddTraffic(0)
	payload := bytes.Repeat([]byte("synthetic-cross-block-payload;"), 5000)
	t.Run("raw-open-missing", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		if err := os.Remove(fixture.path); err != nil {
			t.Fatal(err)
		}
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "", nil, false)
		if result.status != http.StatusInternalServerError || result.disposition != "" || result.doErr != nil || result.readErr != nil {
			t.Errorf("FIX03_PREHEADER missing local source must be a plain 500: status=%d attachment=%q", result.status, result.disposition)
		}
		fix03CheckSource(t, fixture, nil, true)
	})
	t.Run("raw-known-size-mismatch", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		if err := os.Truncate(fixture.path, 1024); err != nil {
			t.Fatal(err)
		}
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "", nil, false)
		if result.status != http.StatusInternalServerError || result.disposition != "" || result.doErr != nil || result.readErr != nil {
			t.Errorf("FIX03_PREHEADER known-size truncated source was silently served: status=%d attachment=%q bytes=%d", result.status, result.disposition, len(result.body))
		}
		fix03CheckSource(t, fixture, payload[:1024], false)
	})
	t.Run("raw-truncate-at-write-header", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		triggered := false
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "", func(code int) error {
			if code == http.StatusOK {
				triggered = true
				return os.Truncate(fixture.path, 1024)
			}
			return nil
		}, true)
		if !triggered || result.faultErr != nil {
			t.Fatal("FIX03_SETUP header-gated source truncation did not run", result.faultErr)
		}
		fix03RequireAbort(t, result, []models.File{fixture.file})
		fix03CheckSource(t, fixture, payload[:1024], false)
	})
	t.Run("raw-output-error", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "first-write", nil, true)
		if !result.injected || result.accepted == 0 || len(result.body) == 0 || !bytes.Equal(result.body, payload[:len(result.body)]) {
			t.Fatal("FIX03_SETUP actual plaintext prefix/output failure was not reached")
		}
		fix03RequireAbort(t, result, []models.File{fixture.file})
		fix03CheckSource(t, fixture, fixture.disk, false)
	})
	t.Run("raw-controls", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		for _, tc := range []struct {
			name, method, byteRange string
			status                  int
			want                    []byte
			headers                 map[string]string
		}{
			{"full", http.MethodGet, "", http.StatusOK, payload, nil},
			{"range", http.MethodGet, "bytes=123-456", http.StatusPartialContent, payload[123:457], nil},
			{"head", http.MethodHead, "", http.StatusOK, nil, nil},
			{"not-modified", http.MethodGet, "", http.StatusNotModified, nil, map[string]string{"If-Modified-Since": time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result := fix03ServeHTTP(t, []models.File{fixture.file}, false, tc.method, tc.byteRange, "", nil, true, tc.headers)
				if !result.returned || result.panicValue != nil || result.doErr != nil || result.readErr != nil || result.status != tc.status || !bytes.Equal(result.body, tc.want) || result.disposition == "" {
					t.Errorf("FIX03_CONTROL healthy raw contract failed: status=%d bytes=%d do=%v read=%v", result.status, len(result.body), result.doErr, result.readErr)
				}
				if downloadstatus.IsCurrentlyDownloading(fixture.file) {
					t.Error("FIX03_CONTROL healthy raw transfer did not complete")
				}
			})
		}
		fixture.file.SizeBytes = 0 // Preserve the existing legacy unknown-size contract.
		database.SaveMetaData(fixture.file)
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "", nil, true)
		if result.status != http.StatusOK || result.doErr != nil || result.readErr != nil || !bytes.Equal(result.body, payload) {
			t.Error("FIX03_CONTROL legacy unknown-size raw file stopped working")
		}
		fix03CheckSource(t, fixture, fixture.disk, false)
	})
	t.Run("raw-invalid-range-terminal-rejection", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, false)
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "bytes=999999999-", "", nil, true)
		if !result.returned || result.panicValue != nil || result.status != http.StatusRequestedRangeNotSatisfiable || result.doErr != nil || result.readErr != nil {
			t.Errorf("FIX03_RANGE complete range rejection did not remain HTTP 416: status=%d do=%v read=%v", result.status, result.doErr, result.readErr)
		}
		// Releasing the guard for a complete HTTP rejection is not a claim
		// that the file was downloaded. The request no longer reads its source.
		if downloadstatus.IsCurrentlyDownloading(fixture.file) {
			t.Error("FIX03_RANGE completed 416 rejection pinned the source")
		}
		fix03CheckSource(t, fixture, fixture.disk, false)
	})
	t.Run("encrypted-empty-single", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, nil, true)
		result := fix03ServeHTTP(t, []models.File{fixture.file}, false, http.MethodGet, "", "", nil, true)
		if !result.returned || result.panicValue != nil || result.status != http.StatusOK || result.doErr != nil || result.readErr != nil || len(result.body) != 0 {
			t.Error("FIX03_EMPTY healthy encrypted empty attachment did not finish as HTTP 200")
		}
		if downloadstatus.IsCurrentlyDownloading(fixture.file) {
			t.Error("FIX03_EMPTY healthy encrypted empty attachment left its guard active")
		}
		fix03CheckSource(t, fixture, fixture.disk, false)
	})
	t.Run("encrypted-zip-plaintext-size-mismatch", func(t *testing.T) {
		fixture := fix03MakeLocalFile(t, payload, true)
		fixture.file.SizeBytes++ // Complete authentic cipher, inconsistent plaintext metadata.
		database.SaveMetaData(fixture.file)
		result := fix03ServeHTTP(t, []models.File{fixture.file}, true, http.MethodGet, "", "", nil, true)
		fix03RequireAbort(t, result, []models.File{fixture.file})
		fix03CheckSource(t, fixture, fixture.disk, false)
	})
	t.Run("encrypted-zip-tail-authentication", func(t *testing.T) {
		healthy := fix03MakeLocalFile(t, []byte("healthy synthetic archive member"), false)
		broken := fix03MakeLocalFile(t, payload, true)
		broken.disk[len(broken.disk)-1] ^= 0x80 // Keep the trusted size and wrapped key unchanged.
		if err := os.WriteFile(broken.path, broken.disk, 0600); err != nil {
			t.Fatal(err)
		}
		files := []models.File{healthy.file, broken.file}
		result := fix03ServeHTTP(t, files, true, http.MethodGet, "", "", nil, true)
		fix03RequireAbort(t, result, files)
		fix03CheckSource(t, healthy, healthy.disk, false)
		fix03CheckSource(t, broken, broken.disk, false)
	})
	for _, mode := range []string{"first-write", "zip-footer", "http-flush"} {
		t.Run("zip-"+mode+"-failure", func(t *testing.T) {
			data := payload
			if mode == "first-write" {
				// This archive fits bufio's buffer: the first output error occurs
				// in zip.Writer.Flush, after entry io.Copy has returned success.
				data = payload[:1024]
			}
			fixture := fix03MakeLocalFile(t, data, false)
			files := []models.File{fixture.file}
			result := fix03ServeHTTP(t, files, true, http.MethodGet, "", mode, nil, true)
			if !result.injected {
				t.Fatal("FIX03_SETUP selected archive output fault did not run")
			}
			for _, active := range result.activeAtFault {
				if !active {
					t.Error("FIX03_STATUS member was already complete when archive output failed")
				}
			}
			fix03RequireAbort(t, result, files)
			fix03CheckSource(t, fixture, fixture.disk, false)
		})
	}
	t.Run("zip-healthy", func(t *testing.T) {
		plain := fix03MakeLocalFile(t, []byte("healthy plain archive member"), false)
		encoded := fix03MakeLocalFile(t, payload, true)
		result := fix03ServeHTTP(t, []models.File{plain.file, encoded.file}, true, http.MethodGet, "", "", nil, true)
		if !result.returned || result.panicValue != nil || result.status != http.StatusOK || result.doErr != nil || result.readErr != nil {
			t.Fatalf("FIX03_CONTROL healthy archive failed: status=%d do=%v read=%v", result.status, result.doErr, result.readErr)
		}
		archive, err := zip.NewReader(bytes.NewReader(result.body), int64(len(result.body)))
		if err != nil || len(archive.File) != 2 {
			t.Fatal("FIX03_CONTROL healthy ZIP central directory is invalid", err)
		}
		for i, fixture := range []fix03LocalFile{plain, encoded} {
			reader, err := archive.File[i].Open()
			if err != nil {
				t.Fatal(err)
			}
			actual, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr != nil || closeErr != nil || archive.File[i].Name != fixture.file.Name || !bytes.Equal(actual, fixture.plaintext) {
				t.Error("FIX03_CONTROL healthy archive member bytes or CRC differ")
			}
			if downloadstatus.IsCurrentlyDownloading(fixture.file) {
				t.Error("FIX03_CONTROL healthy archive member did not complete")
			}
			fix03CheckSource(t, fixture, fixture.disk, false)
		}
	})
	if !t.Failed() {
		t.Log(fix03TerminalPassed)
	}
}
