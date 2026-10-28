//go:build !integration && test

package webserver

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
)

type sec030ReceiveLogCapture struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (capture *sec030ReceiveLogCapture) Write(data []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.data.Write(data)
}

func (capture *sec030ReceiveLogCapture) String() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.data.String()
}

func sec030ReceiveLogSpools(t *testing.T) map[string]bool {
	t.Helper()
	root := filepath.Join(configuration.Get().DataDir, ".gokapi-receive-v1")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return map[string]bool{}
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "receive-") {
			names[entry.Name()] = true
		}
	}
	return names
}

func TestSEC030ReceiveRejectLoggingRedactsMultipartFields(t *testing.T) {
	// Use TestMain's stable DataDir and shared receiver. The synthetic values
	// below exercise untrusted error text; they are never real credentials.
	const marker = "sec030-synthetic-private-marker"
	const password = "sec030-synthetic-private-password"
	before := sec030ReceiveLogSpools(t)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "synthetic.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = part.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	for _, field := range [][2]string{
		{"dztotalfilesize", marker},
		{"dzchunkbyteoffset", "0"},
		{"dzuuid", rand.Text()},
		{"password", password},
	} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/uploadChunk", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", form.FormDataContentType())
	request = authentication.SetUserInRequest(request, models.User{Id: 9130, UserLevel: models.UserLevelUser})
	capture := &sec030ReceiveLogCapture{}
	original := log.Writer()
	log.SetOutput(capture)
	defer log.SetOutput(original)
	recorder := httptest.NewRecorder()
	uploadChunk(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("invalid multipart metadata status=%d; want 400", recorder.Code)
	}
	var response struct {
		Result       string `json:"Result"`
		ErrorMessage string `json:"ErrorMessage"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Error("receive rejection response is not valid JSON")
	} else if response.Result != "error" || response.ErrorMessage == "" {
		t.Error("receive rejection did not retain its structured error response")
	}
	logged := capture.String()
	if strings.Contains(logged, marker) || strings.Contains(logged, password) {
		t.Error("receive rejection logged untrusted private multipart fields")
	}
	for name := range sec030ReceiveLogSpools(t) {
		if !before[name] {
			t.Error("rejected multipart metadata left a new owned receive spool")
		}
	}
}
