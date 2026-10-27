//go:build test && !integration

package webserver

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
	"github.com/forceu/gokapi/internal/webserver/sse"
)

func TestSEC014SlowHeader(t *testing.T) {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:53843", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(7 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(conn, "GET /admin HTTP/1.1\r\nHost: localhost\r\nX-Slow: "); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatal("incomplete header outlived the short header deadline")
	}
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode < 400 {
			t.Fatal("incomplete request reached a successful handler")
		}
	}
}

func TestSEC014CancelledLoginDoesNotAuthenticate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	form := url.Values{"username": {"user"}, "password": {"useruser"}, "csrf-token": {csrftoken.Generate(csrftoken.TypeLogin)}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode())).WithContext(ctx)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	showLogin(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_token" && c.Value != "" {
			t.Fatal("canceled login created a session")
		}
	}
}

// Exercise actual multipart storage and public download, beyond the configured
// 10 MiB in-memory threshold, while the request body spans the header deadline.
func TestSEC014LargeUploadAndSSE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sseReq, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:53843/uploadStatus", nil)
	sseReq.AddCookie(&http.Cookie{Name: "session_token", Value: "validsession"})
	stream, err := http.DefaultClient.Do(sseReq)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != http.StatusOK {
		t.Fatal("SSE authentication failed")
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	form := multipart.NewWriter(writer)
	contentType := form.FormDataContentType()
	block := bytes.Repeat([]byte("SEC014 synthetic file content.\n"), 1024)
	const copies = 550 // More than 16 MiB and above the fixture's multipart memory threshold.
	if len(block)*copies <= configuration.Get().MaxMemory*1024*1024 {
		t.Fatal("fixture no longer crosses the multipart memory threshold")
	}
	expected := sha256.New()
	for i := 0; i < copies; i++ {
		expected.Write(block)
	}
	producerDone := make(chan error, 1)
	producerExited := make(chan struct{})
	go func() {
		defer close(producerExited)
		err := form.WriteField("allowedDownloads", "3")
		if err == nil {
			err = form.WriteField("expiryDays", "1")
		}
		var part io.Writer
		if err == nil {
			part, err = form.CreateFormFile("file", "sec014-large.bin")
		}
		for i := 0; err == nil && i < copies; i++ {
			if i == copies/2 {
				select {
				case <-time.After(6 * time.Second):
				case <-ctx.Done():
					err = ctx.Err()
				}
			}
			if err == nil {
				_, err = part.Write(block)
			}
		}
		if err == nil {
			err = form.Close()
		}
		writer.CloseWithError(err)
		producerDone <- err
	}()
	defer func() { cancel(); reader.Close(); <-producerExited }()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:53843/api/files/add", reader)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("apikey", "validkeyid7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var result models.Result
	err = json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK || result.Result != "OK" || result.FileInfo.Id == "" {
		t.Fatalf("upload failed: status=%d decode=%v", resp.StatusCode, err)
	}
	// Remove only this fixture's record; TestMain removes its isolated data
	// directory. Do not launch global asynchronous GC from a transport test.
	t.Cleanup(func() { database.DeleteMetaData(result.FileInfo.Id) })
	if err := <-producerDone; err != nil {
		t.Fatal(err)
	}
	// The same authenticated SSE connection must still deliver a later event.
	sse.PublishNewStatus(models.UploadStatus{UserId: 7, ChunkId: "sec014-after-long-body", CurrentStatus: 1})
	scanner := bufio.NewScanner(stream.Body)
	found := false
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "sec014-after-long-body") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SSE stopped during body transfer: %v", scanner.Err())
	}
	downloadReq, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:53843/downloadFile?id="+url.QueryEscape(result.FileInfo.Id), nil)
	download, err := http.DefaultClient.Do(downloadReq)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Body.Close()
	got := sha256.New()
	n, err := io.Copy(got, download.Body)
	if err != nil || download.StatusCode != http.StatusOK || n != int64(len(block)*copies) || !bytes.Equal(got.Sum(nil), expected.Sum(nil)) {
		t.Fatalf("download differs: status=%d bytes=%d err=%v", download.StatusCode, n, err)
	}
}
