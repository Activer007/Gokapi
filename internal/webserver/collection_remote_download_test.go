//go:build test && !integration && !noaws && !awsmock

package webserver

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/cloudconfig"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"github.com/Activer007/SupaShare/internal/webserver/api"
	"github.com/Activer007/SupaShare/internal/webserver/authentication"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"github.com/alicebob/miniredis/v2"
)

// The real SDK decrypts S3 archive members; awsmock.Stream copies stored bytes
// and cannot establish this preflight contract. All objects stay on loopback.
func TestCollectionRemoteDownloadPreflight(t *testing.T) {
	master, err := encryption.GetRandomCipher()
	if err != nil {
		t.Fatal(err)
	}
	encryption.Init(models.Configuration{Encryption: models.Encryption{Level: encryption.FullEncryptionStored, Cipher: master}})
	plaintext := bytes.Repeat([]byte("synthetic remote collection payload"), 128)
	var info models.EncryptionInfo
	var ciphertext bytes.Buffer
	if err := encryption.Encrypt(&info, bytes.NewReader(plaintext), &ciphertext); err != nil {
		t.Fatal(err)
	}
	endpoint := securityfixture.NewS3(t)
	previousCloud, provided := cloudconfig.Load()
	wasAvailable := aws.IsAvailable()
	if !aws.Init(models.AwsConfig{Bucket: "security-fixture", Region: "us-east-1", Endpoint: endpoint.Server.URL, KeyId: "synthetic", KeySecret: "synthetic"}) {
		t.Fatal("loopback S3 initialization failed")
	}
	t.Cleanup(func() {
		aws.LogOut()
		if wasAvailable && provided && !aws.Init(previousCloud.Aws) {
			t.Error("could not restore the existing test S3 configuration")
		}
	})
	database.Close()
	t.Cleanup(configuration.ConnectDatabase)
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "remote-collection.sqlite")}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "remote_collection_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			for _, tc := range []struct {
				name, fault         string
				direct, single, e2e bool
			}{
				{name: "bad-wrap-direct", direct: true, fault: "wrapped-key"},
				{name: "bad-wrap-presigned", fault: "wrapped-key"},
				{name: "bad-nonce-direct", direct: true, fault: "nonce"},
				{name: "wrong-key-length-direct", direct: true, fault: "key-length"},
				{name: "valid-direct", direct: true}, {name: "valid-presigned"},
				{name: "bad-wrap-single-direct", direct: true, single: true, fault: "wrapped-key"},
				{name: "bad-wrap-single-presigned", single: true, fault: "wrapped-key"},
				{name: "valid-single-direct", direct: true, single: true}, {name: "valid-single-presigned", single: true},
				{name: "truncated-remote-direct", direct: true, fault: "truncated"},
				{name: "head-error-direct", direct: true, fault: "head"},
				{name: "valid-e2e-raw-single", direct: true, single: true, e2e: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					s := collectionDownloadScene(t)
					remote := s.Files["request"]
					remote.AwsBucket, remote.Encryption, remote.SizeBytes = "security-fixture", info, int64(len(plaintext))
					if _, err := aws.Upload(bytes.NewReader(ciphertext.Bytes()), remote); err != nil {
						t.Fatal(err)
					}
					var checked bytes.Buffer
					if err := aws.Stream(&checked, remote); err != nil || !bytes.Equal(checked.Bytes(), plaintext) {
						t.Fatal("control: stored remote ciphertext is not valid", err)
					}
					if tc.e2e {
						remote.Encryption = models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}
						remote.SizeBytes = int64(ciphertext.Len())
					}
					switch tc.fault {
					case "wrapped-key":
						remote.Encryption.DecryptionKey = append([]byte(nil), info.DecryptionKey...)
						remote.Encryption.DecryptionKey[0] ^= 0x80
					case "nonce":
						remote.Encryption.Nonce = info.Nonce[:1]
					case "key-length":
						wrapped, err := encryption.EncryptDecryptBytes(bytes.Repeat([]byte{0x6b}, 16), master, info.Nonce, true)
						if err != nil {
							t.Fatal(err)
						}
						remote.Encryption.DecryptionKey = wrapped
					case "truncated":
						if _, err := aws.Upload(bytes.NewReader(ciphertext.Bytes()[:ciphertext.Len()-1]), remote); err != nil {
							t.Fatal(err)
						}
					case "head":
						endpoint.FailHead.Store(true)
						t.Cleanup(func() { endpoint.FailHead.Store(false) })
					}
					database.SaveMetaData(remote)
					ids := []string{s.Files["own"].Id, remote.Id}
					if tc.single {
						ids = []string{remote.Id}
					}
					w := httptest.NewRecorder()
					func() {
						defer func() {
							if caught := recover(); caught != nil {
								t.Errorf("remote preflight allowed a streaming failure: %v", caught)
							}
						}()
						if tc.direct {
							if tc.single {
								r := httptest.NewRequest(http.MethodGet, "/api/files/download/"+remote.Id, nil)
								r.Header.Set("apikey", s.Keys["owner"].Id)
								api.Process(w, r)
							} else {
								w = collectionArchiveAPI(s.Keys["owner"].Id, ids, false, "")
							}
						} else {
							grant := ""
							if tc.single {
								grant = collectionDownloadGrant(t, s.Keys["owner"].Id, ids...)
							} else {
								grant = collectionArchiveGrant(t, s.Keys["owner"].Id, ids)
							}
							r := httptest.NewRequest(http.MethodGet, "/downloadPresigned?key="+grant, nil)
							downloadPresigned(w, authentication.SetUserInRequest(r, s.Users["owner"]))
						}
					}()
					if tc.fault != "" {
						if w.Code < 400 || bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) {
							t.Errorf("invalid remote representation was served: status=%d", w.Code)
						}
						for _, id := range ids {
							stored, ok := database.GetMetaDataById(id)
							if !ok || stored.DownloadsRemaining != 1 || stored.DownloadCount != 0 || downloadstatus.IsCurrentlyDownloading(stored) {
								t.Errorf("remote rejection charged member %q or left a guard: remaining=%d count=%d", stored.Name, stored.DownloadsRemaining, stored.DownloadCount)
							}
						}
						return
					}
					if tc.e2e {
						location := w.Header().Get("Location")
						if w.Code != http.StatusTemporaryRedirect || !strings.HasPrefix(location, endpoint.Server.URL+"/") {
							t.Fatalf("E2E raw transfer lost its loopback redirect: status=%d", w.Code)
						}
						response, err := endpoint.Server.Client().Get(location)
						if err != nil {
							t.Fatal(err)
						}
						data, readErr := io.ReadAll(response.Body)
						_ = response.Body.Close()
						if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(data, ciphertext.Bytes()) {
							t.Fatal("E2E raw S3 bytes changed", readErr)
						}
						collectionDownloadRemaining(t, remote.Id, 0)
						return
					}
					if tc.single {
						if w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), plaintext) {
							t.Fatalf("control: valid remote single status=%d", w.Code)
						}
						collectionDownloadRemaining(t, remote.Id, 0)
						return
					}
					archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
					if w.Code != http.StatusOK || err != nil || len(archive.File) != 2 {
						t.Fatalf("control: valid remote ZIP status=%d error=%v", w.Code, err)
					}
					foundRemote := false
					for _, entry := range archive.File {
						if entry.Name != remote.Name {
							continue
						}
						handle, err := entry.Open()
						if err != nil {
							t.Fatal(err)
						}
						data, err := io.ReadAll(handle)
						_ = handle.Close()
						if err != nil || !bytes.Equal(data, plaintext) {
							t.Fatal("remote archive plaintext differs", err)
						}
						foundRemote = true
					}
					if !foundRemote {
						t.Fatal("remote archive omitted its encrypted member")
					}
					for _, id := range ids {
						collectionDownloadRemaining(t, id, 0)
					}
				})
			}
		})
	}
}
