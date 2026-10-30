//go:build test && !integration

package webserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

func TestCollectionEncryptedDownloadSize(t *testing.T) {
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.LocalEncryptionStored, Cipher: master}})
	plaintext := bytes.Repeat([]byte("synthetic collection payload"), 8192)
	var info models.EncryptionInfo
	var ciphertext bytes.Buffer
	if err := encryption.Encrypt(&info, bytes.NewReader(plaintext), &ciphertext); err != nil {
		t.Fatal(err)
	}
	if int64(ciphertext.Len()) != encryption.CalculateEncryptedFilesize(int64(len(plaintext))) {
		t.Fatal("actual encrypted fixture disagrees with the storage size calculation")
	}
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "encrypted-collection.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "encrypted_collection_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name      string
				zip, e2e  bool
				sizeDelta int
			}{
				{name: "intact-single"}, {name: "truncated-single", sizeDelta: -1},
				{name: "intact-zip", zip: true}, {name: "truncated-zip", zip: true, sizeDelta: -1},
				{name: "oversized-single", sizeDelta: 1}, {name: "e2e-raw-size", e2e: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					file := s.Files["request"]
					file.Encryption, file.SizeBytes = info, int64(len(plaintext))
					payload := append([]byte(nil), ciphertext.Bytes()...)
					if tc.e2e {
						file.Encryption = models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}
						file.SizeBytes = int64(len(payload))
					}
					if tc.sizeDelta < 0 {
						payload = payload[:len(payload)+tc.sizeDelta]
					} else if tc.sizeDelta > 0 {
						payload = append(payload, 0)
					}
					if err := os.WriteFile(filepath.Join(s.DataDir, file.SHA1), payload, 0600); err != nil {
						t.Fatal(err)
					}
					database.SaveMetaData(file)
					ids := []string{file.Id}
					if tc.zip {
						// The healthy member must retain its allowance if any other
						// member has an unreadable encrypted representation.
						ids = append([]string{s.Files["own"].Id}, ids...)
					}
					grant := collectionDownloadGrant(t, s.Keys["owner"].Id, ids...)
					w := collectionDownloadCall(grant, s.Users["owner"], http.MethodGet, "", nil)
					if tc.sizeDelta != 0 {
						if w.Code < 400 || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || bytes.Contains(w.Body.Bytes(), plaintext[:64]) {
							t.Errorf("invalid ciphertext was served: status=%d", w.Code)
						}
						for _, id := range ids {
							stored, _ := database.GetMetaDataById(id)
							if stored.DownloadsRemaining != 1 || stored.DownloadCount != 0 {
								t.Errorf("invalid ciphertext charged member %q: remaining=%d count=%d", stored.Name, stored.DownloadsRemaining, stored.DownloadCount)
							}
						}
						return
					}
					if w.Code != http.StatusOK {
						t.Fatalf("intact download status=%d", w.Code)
					}
					if tc.zip {
						archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
						if err != nil || len(archive.File) != 2 {
							t.Fatal("intact archive is invalid", err)
						}
						found := false
						for _, entry := range archive.File {
							if entry.Name != file.Name {
								continue
							}
							handle, err := entry.Open()
							if err != nil {
								t.Fatal(err)
							}
							data, err := io.ReadAll(handle)
							_ = handle.Close()
							if err != nil || !bytes.Equal(data, plaintext) {
								t.Fatal("archive plaintext differs", err)
							}
							found = true
						}
						if !found {
							t.Fatal("archive omitted its encrypted member")
						}
					} else {
						want := plaintext
						if tc.e2e {
							want = payload
						}
						if !bytes.Equal(w.Body.Bytes(), want) {
							t.Fatal("intact download bytes differ")
						}
					}
					for _, id := range ids {
						collectionDownloadRemaining(t, id, 0)
						stored, _ := database.GetMetaDataById(id)
						if stored.DownloadCount != 1 {
							t.Fatal("intact transfer did not count exactly once")
						}
					}
				})
			}
		})
	}
}
