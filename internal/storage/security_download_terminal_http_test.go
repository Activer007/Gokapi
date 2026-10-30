//go:build test

package storage

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/logging/serverstats"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
)

var fix03OutputError = errors.New("synthetic FIX03 output failure")

func fix03RequireAbort(t *testing.T, result fix03HTTPResult, files []models.File) {
	t.Helper()
	if result.returned || result.panicValue != http.ErrAbortHandler {
		t.Errorf("FIX03_TERMINAL failed stream returned normally or used a non-abort panic: returned=%v panic=%v", result.returned, result.panicValue)
	}
	if result.doErr == nil && result.readErr == nil {
		t.Error("FIX03_HTTP failed stream ended with a successful HTTP body")
	}
	for _, file := range files {
		if !downloadstatus.IsCurrentlyDownloading(file) {
			t.Error("FIX03_STATUS failed transfer was marked complete")
		}
	}
	for _, text := range []string{"Error getting file handler", "Error decrypting file", "Internal error - Error decrypting"} {
		if bytes.Contains(result.body, []byte(text)) {
			t.Error("FIX03_HTTP failure text became attachment bytes")
		}
	}
	if bytes.Contains(result.body, []byte("PK\x05\x06")) {
		t.Error("FIX03_ZIP failed archive acquired a successful end-of-directory footer")
	}
}

type fix03HTTPResult struct {
	status                   int
	disposition              string
	body                     []byte
	doErr, readErr, faultErr error
	returned, injected       bool
	panicValue               any
	accepted                 int64
	activeAtFault            []bool
}

// No ReaderFrom is forwarded: all actual output passes through Write, including
// ServeContent's CopyN. Fault timing uses handler events, never scheduling sleeps.
type fix03FaultWriter struct {
	http.ResponseWriter
	mode         string
	files        []models.File
	beforeHeader func(int) error
	result       *fix03HTTPResult
}

func (writer *fix03FaultWriter) WriteHeader(code int) {
	if writer.beforeHeader != nil {
		writer.result.faultErr = writer.beforeHeader(code)
	}
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *fix03FaultWriter) inject() {
	writer.result.injected = true
	for _, file := range writer.files {
		writer.result.activeAtFault = append(writer.result.activeAtFault, downloadstatus.IsCurrentlyDownloading(file))
	}
}

func (writer *fix03FaultWriter) Write(data []byte) (int, error) {
	if writer.mode == "first-write" && !writer.result.injected {
		writer.inject()
		accepted := min(len(data), 128)
		n, err := writer.ResponseWriter.Write(data[:accepted])
		writer.result.accepted += int64(n)
		if err != nil {
			return n, err
		}
		if err := http.NewResponseController(writer.ResponseWriter).Flush(); err != nil {
			writer.result.faultErr = err
			return n, err
		}
		return n, fix03OutputError
	}
	if writer.mode == "zip-footer" && bytes.Contains(data, []byte("PK\x01\x02")) {
		writer.inject()
		return 0, fix03OutputError
	}
	n, err := writer.ResponseWriter.Write(data)
	writer.result.accepted += int64(n)
	return n, err
}

func (writer *fix03FaultWriter) FlushError() error {
	if writer.mode == "http-flush" {
		writer.inject()
		return fix03OutputError
	}
	return http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *fix03FaultWriter) Flush()                      { _ = writer.FlushError() }
func (writer *fix03FaultWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func fix03ServeHTTP(t *testing.T, files []models.File, archive bool, method, byteRange, mode string, beforeHeader func(int) error, drainTraffic bool, extraHeaders ...map[string]string) fix03HTTPResult {
	t.Helper()
	beforeTraffic, _ := serverstats.GetCurrentTraffic()
	wantTraffic := beforeTraffic
	for _, file := range files {
		wantTraffic += uint64(file.SizeBytes)
	}
	finished := make(chan fix03HTTPResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := fix03HTTPResult{}
		writer := w
		if mode != "" || beforeHeader != nil {
			writer = &fix03FaultWriter{ResponseWriter: w, mode: mode, files: files, beforeHeader: beforeHeader, result: &result}
		}
		defer func() {
			result.panicValue = recover()
			finished <- result
			if result.panicValue != nil {
				// Keep the real net/http H1 abort behavior after observing unwind.
				panic(result.panicValue)
			}
		}()
		if archive {
			ServeFilesAsZip(files, "synthetic-fix03", writer, r)
		} else {
			file := files[0]
			ServeFile(file, writer, r, true, false, file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted, false)
		}
		result.returned = true
	}))
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	t.Cleanup(func() { server.Close(); transport.CloseIdleConnections() })
	request, err := http.NewRequestWithContext(t.Context(), method, server.URL+"/synthetic-download", nil)
	if err != nil {
		t.Fatal(err)
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	for _, headers := range extraHeaders {
		for name, value := range headers {
			request.Header.Set(name, value)
		}
	}
	response, doErr := client.Do(request)
	var body []byte
	var readErr error
	if response != nil {
		if response.ProtoMajor != 1 {
			t.Fatal("FIX03_SETUP expected real HTTP/1")
		}
		body, readErr = io.ReadAll(response.Body)
		_ = response.Body.Close()
	}
	var result fix03HTTPResult
	select {
	case result = <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("FIX03_HTTP handler did not terminate")
	}
	server.Close() // Drain net/http cleanup before inspecting final state.
	result.doErr, result.readErr, result.body = doErr, readErr, body
	if response != nil {
		result.status, result.disposition = response.StatusCode, response.Header.Get("Content-Disposition")
	}
	if drainTraffic {
		deadline := time.NewTimer(2 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			current, _ := serverstats.GetCurrentTraffic()
			if current >= wantTraffic {
				break
			}
			select {
			case <-ticker.C:
			case <-deadline.C:
				t.Fatal("FIX03_TRAFFIC finite traffic work did not drain")
			}
		}
	}
	return result
}
