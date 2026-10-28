//go:build !integration && test

package webserver

import (
	"bytes"
	"crypto/rand"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

// The login middleware supplies the authenticated user context. Exercise the
// browser multipart receiver with the same UUID for two distinct contexts.
func TestSEC032BrowserChunkScope(t *testing.T) {
	id := rand.Text()
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
		path := filepath.Join(configuration.Get().DataDir, "chunk-"+chunking.StorageID(id, user, ""))
		t.Cleanup(func() { _ = os.Remove(path) })
		if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, payload) {
			t.Fatal("browser upload not bound to context user")
		}
	}
	// The second upload must not have overwritten the first.
	path := filepath.Join(configuration.Get().DataDir, "chunk-"+chunking.StorageID(id, 9132, ""))
	if data, err := os.ReadFile(path); err != nil || string(data) != "synthetic-browser-9132" {
		t.Fatal("browser upload crossed user boundary")
	}
	w := httptest.NewRecorder()
	uploadChunk(w, httptest.NewRequest("POST", "/uploadChunk", nil))
	if w.Code != 401 {
		t.Fatal("receiver accepted a missing user context")
	}
}
