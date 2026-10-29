//go:build !integration && test

package webserver

import (
	"bytes"
	"crypto/rand"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/webserver/authentication/downloadPasswordToken"
)

const sec062RangePayload = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_"

// These small requests prove Range compatibility through the existing handlers.
// They do not reproduce resource exhaustion or prove a Go security fix; that
// requires the patched toolchain policy and vulnerability scan evidence.
func TestSEC062RangeStaticCompatibility(t *testing.T) {
	files := fstest.MapFS{
		"sec062-range.bin": &fstest.MapFile{
			Data:    []byte(sec062RangePayload),
			Mode:    0444,
			ModTime: time.Unix(1700000000, 0),
		},
	}
	mux := http.NewServeMux()
	mux.Handle("/", filesystemHandler(files))
	sec062RangeCompatibility(t, mux, "/sec062-range.bin", nil)
}

func TestSEC062RangeLocalDownloadCompatibility(t *testing.T) {
	// Keep TestMain's database, DataDir and filesystem configuration stable.
	// Only this test's new blob, metadata and capability are created/removed.
	blob, err := os.CreateTemp(configuration.Get().DataDir, "sec062-range-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blob.Name()) })
	_, writeErr := io.WriteString(blob, sec062RangePayload)
	closeErr := blob.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("write synthetic range blob: write=%v close=%v", writeErr, closeErr)
	}
	file := models.File{
		Id:                 rand.Text(),
		Name:               "sec062-range.bin",
		SHA1:               filepath.Base(blob.Name()),
		SizeBytes:          int64(len(sec062RangePayload)),
		ContentType:        "application/octet-stream",
		UserId:             7,
		UnlimitedTime:      true,
		UnlimitedDownloads: true,
		PasswordHash:       configuration.HashPassword("synthetic-range-password", false, ""),
	}
	database.SaveMetaData(file)
	t.Cleanup(func() { database.DeleteMetaData(file.Id) })
	t.Cleanup(func() { downloadPasswordToken.DeleteAllForFile(file.Id) })
	mux := http.NewServeMux()
	mux.HandleFunc("/downloadFile", downloadFile)
	path := "/downloadFile?id=" + file.Id
	denied := httptest.NewRecorder()
	unauthorized := httptest.NewRequest(http.MethodGet, path, nil)
	unauthorized.Header.Set("Range", "bytes=0-3")
	mux.ServeHTTP(denied, unauthorized)
	if denied.Code < 300 || denied.Code >= 400 {
		t.Fatalf("protected range request without capability status=%d; want redirect", denied.Code)
	}
	cookie := &http.Cookie{Name: "p" + file.Id, Value: downloadPasswordToken.Generate(file.Id)}
	sec062RangeCompatibility(t, mux, path, cookie)
	after, err := os.ReadFile(blob.Name())
	if err != nil || !bytes.Equal(after, []byte(sec062RangePayload)) {
		t.Fatal("range download changed the synthetic blob")
	}
}

func sec062RangeCompatibility(t *testing.T, handler http.Handler, path string, cookie *http.Cookie) {
	t.Helper()
	payload := []byte(sec062RangePayload)
	if len(payload) != 64 {
		t.Fatal("range fixture must remain 64 bytes")
	}
	for _, test := range []struct {
		name         string
		method       string
		ranges       string
		status       int
		contentRange string
		want         []byte
	}{
		{name: "full", method: http.MethodGet, status: http.StatusOK, want: payload},
		{name: "head", method: http.MethodHead, status: http.StatusOK},
		{name: "single", method: http.MethodGet, ranges: "bytes=0-3", status: http.StatusPartialContent, contentRange: "bytes 0-3/64", want: payload[:4]},
		{name: "multiple", method: http.MethodGet, ranges: "bytes=0-3,8-11", status: http.StatusPartialContent},
		{name: "unsatisfiable", method: http.MethodGet, ranges: "bytes=128-131", status: http.StatusRequestedRangeNotSatisfiable, contentRange: "bytes */64"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, path, nil)
			if test.ranges != "" {
				request.Header.Set("Range", test.ranges)
			}
			if cookie != nil {
				request.AddCookie(cookie)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("range response status=%d; want %d", recorder.Code, test.status)
			}
			if got := recorder.Header().Get("Content-Range"); got != test.contentRange {
				t.Fatalf("Content-Range=%q; want %q", got, test.contentRange)
			}
			if test.name == "multiple" {
				sec062CheckRangeParts(t, recorder, payload)
				return
			}
			if test.status == http.StatusRequestedRangeNotSatisfiable {
				return
			}
			if !bytes.Equal(recorder.Body.Bytes(), test.want) {
				t.Fatal("range response did not contain the exact expected bytes")
			}
			length := len(test.want)
			if test.method == http.MethodHead {
				length = len(payload)
			}
			if got := recorder.Header().Get("Content-Length"); got != strconv.Itoa(length) {
				t.Fatalf("Content-Length=%q; want %d", got, length)
			}
		})
	}
}

func sec062CheckRangeParts(t *testing.T, recorder *httptest.ResponseRecorder, payload []byte) {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(recorder.Header().Get("Content-Type"))
	if err != nil || mediaType != "multipart/byteranges" || parameters["boundary"] == "" {
		t.Fatal("multiple ranges did not produce a multipart/byteranges response")
	}
	reader := multipart.NewReader(bytes.NewReader(recorder.Body.Bytes()), parameters["boundary"])
	for _, expected := range []struct {
		contentRange string
		data         []byte
	}{
		{contentRange: "bytes 0-3/64", data: payload[:4]},
		{contentRange: "bytes 8-11/64", data: payload[8:12]},
	} {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read expected range part: %v", err)
		}
		if got := part.Header.Get("Content-Range"); got != expected.contentRange {
			t.Fatalf("part Content-Range=%q; want %q", got, expected.contentRange)
		}
		data, readErr := io.ReadAll(io.LimitReader(part, int64(len(payload))+1))
		closeErr := part.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(data, expected.data) {
			t.Fatal("multipart range part did not contain the exact expected bytes")
		}
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatal("multiple ranges returned extra data or a malformed closing boundary")
	}
}
