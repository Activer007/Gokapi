//go:build !integration && test

package webserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage"
	"github.com/Activer007/Gokapi/internal/storage/chunking"
	"github.com/Activer007/Gokapi/internal/storage/chunking/chunksession"
	"github.com/Activer007/Gokapi/internal/uploadquota"
	"github.com/Activer007/Gokapi/internal/webserver/authentication"
)

// The login middleware supplies the authenticated user context. Exercise the
// browser multipart receiver with the same UUID for two distinct contexts.
func TestSEC032BrowserChunkScope(t *testing.T) {
	id := rand.Text()
	env := configuration.GetEnvironment()
	if err := env.ValidateWholeUploadLimits(); err != nil {
		t.Fatal(err)
	}
	const mib = 1 << 20
	limits := uploadquota.Limits{User: uploadquota.Usage{Bytes: env.UploadUserTotalMB * mib, Files: env.UploadUserMaxFiles}, Instance: uploadquota.Usage{Bytes: env.UploadInstanceTotalMB * mib, Files: env.UploadInstanceMaxFiles}, UserActive: env.UploadUserActive, InstanceActive: env.UploadInstanceActive, StagingBytes: env.UploadStagingMB * mib}
	var leases []*chunksession.Lease
	defer func() {
		for _, lease := range leases {
			func() {
				defer lease.Release()
				if err := lease.CloseInput(); err != nil {
					t.Error("browser test input cleanup failed", err)
					return
				}
				if _, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Abort, Upload: lease.Upload, Now: time.Now().Unix(), Terminal: uploadquota.Aborted, CleanupConfirmed: true}); err != nil {
					t.Error("unpublished browser test claim cleanup failed", err)
				}
			}()
		}
	}()
	for _, user := range []int{9132, 9133} {
		payload := []byte("synthetic-browser-" + strconv.Itoa(user))
		body := new(bytes.Buffer)
		form := multipart.NewWriter(body)
		for name, value := range map[string]string{"dzuuid": id, "dzchunkbyteoffset": "0", "dztotalfilesize": strconv.Itoa(len(payload))} {
			if err := form.WriteField(name, value); err != nil {
				t.Fatal(err)
			}
		}
		f, err := form.CreateFormFile("file", "synthetic.txt")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err = form.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/uploadChunk", body)
		r.Header.Set("Content-Type", form.FormDataContentType())
		r = authentication.SetUserInRequest(r, models.User{Id: user, UserLevel: models.UserLevelUser})
		w := httptest.NewRecorder()
		uploadChunk(w, r)
		if w.Code != 200 {
			t.Fatalf("browser upload status=%d", w.Code)
		}
		scope := chunksession.Scope{ClientID: id, UserID: user}
		ledger, err := database.ReadUploadQuota(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		record, found := ledger.Records[chunksession.ID(scope)]
		if !found || record.UserID != user || record.RequestID != "" || record.Bytes != int64(len(payload)) {
			t.Fatal("browser upload lost authenticated scope")
		}
		lease, err := storage.ChunkSessions().Prepare(context.Background(), scope, chunking.FileHeader{Size: int64(len(payload))}, "browser-test-"+rand.Text(), limits)
		if err != nil {
			t.Fatal("browser upload lacks complete confirmed coverage", err)
		}
		leases = append(leases, lease)
		if data, err := io.ReadAll(lease.Input); err != nil || !bytes.Equal(data, payload) {
			t.Fatal("browser upload not bound to context user")
		}
	}
	// The second upload must not have overwritten the first.
	if _, err := leases[0].Input.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if data, err := io.ReadAll(leases[0].Input); err != nil || string(data) != "synthetic-browser-9132" {
		t.Fatal("browser upload crossed user boundary")
	}
	w := httptest.NewRecorder()
	uploadChunk(w, httptest.NewRequest("POST", "/uploadChunk", nil))
	if w.Code != 401 {
		t.Fatal("receiver accepted a missing user context")
	}
}
