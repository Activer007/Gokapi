package uploadreceive

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func receiveRequest(t *testing.T, first bool, fields [][2]string, payload []byte, unknown bool) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeFields := func() {
		for _, field := range fields {
			if err := writer.WriteField(field[0], field[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !first {
		writeFields()
	}
	part, err := writer.CreateFormFile("file", "synthetic.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if first {
		writeFields()
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/upload", bytes.NewReader(body.Bytes()))
	r.Header.Set("Content-Type", writer.FormDataContentType())
	if unknown {
		r.ContentLength = -1
	}
	return r
}

func TestReceiveMultipartOrdersAndUnknownLength(t *testing.T) {
	for _, first := range []bool{false, true} {
		for _, unknown := range []bool{false, true} {
			p, err := New(4, 64<<20)
			if err != nil {
				t.Fatal(err)
			}
			r := receiveRequest(t, first, [][2]string{{"expiryDays", "7"}}, []byte("streamed bytes"), unknown)
			upload, err := p.Receive(httptest.NewRecorder(), r, t.TempDir(), 1024, 0)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(upload.File)
			if err != nil || string(data) != "streamed bytes" || upload.Header.Size != 14 || upload.Header.Filename != "synthetic.bin" || upload.Fields.Get("expiryDays") != "7" {
				t.Fatalf("wrong received data/header/fields: bytes=%d error=%v", len(data), err)
			}
			path := upload.File.Name()
			if err := upload.Close(); err != nil {
				t.Fatal(err)
			}
			if err := upload.Close(); err != nil {
				t.Fatal("Close is not idempotent", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("owned spool remains after Close", err)
			}
			if p.active != 0 || p.reserved != 0 {
				t.Fatal("receiver capacity was not restored")
			}
		}
	}
}

func TestReceiveSlotAndCleanupRetry(t *testing.T) {
	p, err := New(1, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	call := func() (*Upload, error) {
		return p.Receive(httptest.NewRecorder(), receiveRequest(t, true, nil, []byte("abc"), false), dir, 1024, 0)
	}
	upload, err := call()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := call(); !errors.Is(err, ErrBusy) {
		t.Fatalf("occupied slot = %v", err)
	}
	p.remove = func(string) error { return os.ErrPermission }
	if err := upload.Close(); err == nil {
		t.Fatal("failed removal reported success")
	}
	if p.active != 0 || p.reserved == 0 {
		t.Fatal("failed cleanup lost capacity or held request slot")
	}
	if _, err := call(); !errors.Is(err, ErrCapacity) {
		t.Fatalf("orphan cleanup failure = %v", err)
	}
	p.remove = os.Remove
	next, err := call()
	if err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	if err := upload.Close(); err != nil {
		t.Fatal(err)
	}
	if p.active != 0 || p.reserved != 0 {
		t.Fatal("cleanup retry double-counted capacity")
	}
}
