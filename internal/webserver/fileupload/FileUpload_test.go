package fileupload

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage"
	"github.com/Activer007/Gokapi/internal/storage/chunking"
	"github.com/Activer007/Gokapi/internal/storage/chunking/chunksession"
	"github.com/Activer007/Gokapi/internal/test"
	"github.com/Activer007/Gokapi/internal/test/testconfiguration"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func TestMain(m *testing.M) {
	testconfiguration.Create(false)
	configuration.Load()
	configuration.ConnectDatabase()
	exitVal := m.Run()
	testconfiguration.Delete()
	os.Exit(exitVal)
}

func TestParseConfig(t *testing.T) {
	data := testData{
		allowedDownloads: "9",
		expiryDays:       "5",
		password:         "123",
		isE2E:            "",
		realSize:         "",
	}
	config, err := parseConfig(data)
	test.IsNil(t, err)
	test.IsEqualBool(t, config.IsEndToEndEncrypted, false)
	test.IsEqualInt64(t, config.RealSize, 0)

	test.IsEqualInt(t, config.AllowedDownloads, 9)
	test.IsEqualString(t, config.Password, "123")
	test.IsEqualInt(t, config.Expiry, 5)

	config, err = parseConfig(data)
	test.IsNil(t, err)

	data.allowedDownloads = ""
	data.expiryDays = "invalid"

	config, err = parseConfig(data)
	test.IsNil(t, err)
	test.IsEqualInt(t, config.AllowedDownloads, 1)
	test.IsEqualInt(t, config.Expiry, 14)
	test.IsEqualBool(t, config.UnlimitedTime, false)
	test.IsEqualBool(t, config.UnlimitedDownload, false)

	data.allowedDownloads = "0"
	data.expiryDays = "0"
	config, err = parseConfig(data)
	test.IsNil(t, err)
	test.IsEqualBool(t, config.UnlimitedTime, true)
	test.IsEqualBool(t, config.UnlimitedDownload, true)

	data.isE2E = "true"
	data.realSize = "200"
	config, err = parseConfig(data)
	test.IsNil(t, err)
	test.IsEqualBool(t, config.IsEndToEndEncrypted, true)
	test.IsEqualInt64(t, config.RealSize, 200)
}

func TestProcess(t *testing.T) {
	w, r := test.GetRecorder("POST", "/upload", nil, nil, strings.NewReader("invalid§$%&%§"))
	err := ProcessCompleteFile(w, r, 9, 20)
	test.IsNotNil(t, err)

	w = httptest.NewRecorder()
	r = getFileUploadRecorder(false)
	err = ProcessCompleteFile(w, r, 9, 20)
	test.IsNil(t, err)
	resp := w.Result()
	body, _ := io.ReadAll(resp.Body)
	result := models.Result{}
	err = json.Unmarshal(body, &result)
	test.IsNil(t, err)
	test.IsEqualString(t, result.Result, "OK")
	test.IsEqualString(t, result.FileInfo.UrlDownload, "http://127.0.0.1:53843/d?id="+result.FileInfo.Id)
	test.IsEqualString(t, result.FileInfo.UrlHotlink, "http://127.0.0.1:53843/downloadFile?id="+result.FileInfo.Id)
	test.IsEqualString(t, result.FileInfo.Name, "testFile")
	test.IsEqualString(t, result.FileInfo.Size, "11 B")
	test.IsEqualBool(t, result.FileInfo.UnlimitedTime, false)
	test.IsEqualBool(t, result.FileInfo.UnlimitedDownloads, false)
	test.IsEqualInt(t, result.FileInfo.UploaderId, 9)
}

func TestProcessNewChunk(t *testing.T) {
	w, r := test.GetRecorder("POST", "/uploadChunk", nil, nil, strings.NewReader("invalid§$%&%§"))
	_, err := ProcessNewChunk(w, r, false, "", 9, 100*1000*1000)
	test.IsNotNil(t, err)

	w = httptest.NewRecorder()
	r = getFileUploadRecorder(false)
	_, err = ProcessNewChunk(w, r, false, "", 9, 100*1000*1000)
	test.IsNotNil(t, err)

	w = httptest.NewRecorder()
	r = getFileUploadRecorder(true)
	_, err = ProcessNewChunk(w, r, false, "", 9, 100*1000*1000)
	test.IsNil(t, err)
	response, err := io.ReadAll(w.Result().Body)
	test.IsNil(t, err)
	test.IsEqualString(t, string(response), "{\"result\":\"OK\"}")
	assertReceivedChunkSession(t, chunksession.Scope{ClientID: "randomchunkuuid", UserID: 9}, []byte("testContentxx"))
}

// Inspect through the same coverage/Claim gate used by completion, without
// guessing a private payload path. This test never publishes storage work: once
// CloseInput has confirmed removal of its only resource, Abort is trustworthy.
func assertReceivedChunkSession(t *testing.T, scope chunksession.Scope, expected []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	limits, err := chunkUploadLimits()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := database.ReadUploadQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	record, found := ledger.Records[chunksession.ID(scope)]
	if !found || record.UserID != scope.UserID || record.RequestID != scope.RequestID || !record.BytesKnown || record.Bytes != int64(len(expected)) || record.Completing || record.StagingReleased {
		t.Fatal("received chunk lost its scoped total declaration")
	}
	lease, err := storage.ChunkSessions().Prepare(ctx, scope, chunking.FileHeader{Size: int64(len(expected))}, "receive-test-"+rand.Text(), limits)
	if err != nil {
		t.Fatal("received payload cannot pass complete coverage", err)
	}
	defer func() {
		defer lease.Release()
		if err := lease.CloseInput(); err != nil {
			t.Error("test input cleanup failed", err)
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := database.ApplyUploadQuota(cleanupCtx, uploadquota.Change{Action: uploadquota.Abort, Upload: lease.Upload, Now: time.Now().Unix(), Terminal: uploadquota.Aborted, CleanupConfirmed: true, CheckRequestPolicy: scope.RequestID != ""}); err != nil {
			t.Error("test-only unpublished claim cleanup failed", err)
		}
	}()
	actual, err := io.ReadAll(lease.Input)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatal("scoped confirmed chunk bytes differ", err)
	}
}

func TestCompleteChunk(t *testing.T) {
	body := strings.NewReader("%")
	r := httptest.NewRequest(http.MethodPost, "/upload", body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, _, err := ParseFileHeader(r)
	test.IsNotNil(t, err)

	w := httptest.NewRecorder()
	r = getFileUploadRecorder(false)
	_, _, _, err = ParseFileHeader(r)
	test.IsNotNil(t, err)

	data := url.Values{}
	data.Set("isE2E", "true")
	data.Set("realSize", "none")
	w, r = test.GetRecorder("POST", "/uploadComplete", nil, nil, strings.NewReader(data.Encode()))
	r.Header.Set("Content-type", "application/x-www-form-urlencoded")
	chunkId, header, config, err := ParseFileHeader(r)
	test.IsNotNil(t, err)

	data.Del("isE2E")
	data.Del("realSize")
	data.Set("allowedDownloads", "9")
	data.Set("expiryDays", "5")
	data.Set("password", "123")
	data.Set("chunkid", "randomchunkuuid")
	data.Set("filename", "random.file")
	data.Set("filesize", "13")
	w, r = test.GetRecorder("POST", "/uploadComplete", nil, nil, strings.NewReader(data.Encode()))
	r.Header.Set("Content-type", "application/x-www-form-urlencoded")
	chunkId, header, config, err = ParseFileHeader(r)
	test.IsNil(t, err)
	// This is the retained low-level legacy helper, not the runtime receive
	// path. Give it its own legacy fixture instead of another test's upload.
	err = chunking.NewChunk(strings.NewReader("testContentxx"), &multipart.FileHeader{Size: 13}, chunking.ChunkInfo{UUID: chunking.StorageID(chunkId, 9, config.FileRequestId), TotalFilesizeBytes: 13}, 100*1000*1000)
	test.IsNil(t, err)
	file, err := CompleteChunk(chunkId, header, 9, config)
	test.IsNil(t, err)
	test.IsEqualString(t, file.Name, "random.file")

	response, err := io.ReadAll(w.Result().Body)
	test.IsNil(t, err)
	test.IsEqualString(t, string(response), "")

	data.Set("chunkid", "invalid")
	w, r = test.GetRecorder("POST", "/uploadComplete", nil, nil, strings.NewReader(data.Encode()))
	r.Header.Set("Content-type", "application/x-www-form-urlencoded")
	chunkId, header, config, err = ParseFileHeader(r)
	test.IsNil(t, err)
	_, err = CompleteChunk(chunkId, header, 9, config)
	test.IsNotNil(t, err)
}

func getFileUploadRecorder(addChunkInfo bool) *http.Request {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if addChunkInfo {
		w.WriteField("dztotalfilesize", "13")
		w.WriteField("dzchunkbyteoffset", "0")
		w.WriteField("dzuuid", "randomchunkuuid")
	}
	writer, _ := w.CreateFormFile("file", "testFile")
	content := "testContent"
	if addChunkInfo {
		content += "xx"
	} // match the declared 13-byte last chunk
	io.WriteString(writer, content)
	w.Close()
	r := httptest.NewRequest("POST", "/upload", &b)
	r.Header.Set("Content-Type", w.FormDataContentType())
	if !addChunkInfo {
		r.Header.Add("allowedDownloads", "9")
		r.Header.Add("expiryDays", "5")
		r.Header.Add("password", "123")
	}
	return r
}

type testData struct {
	allowedDownloads, expiryDays, password, isE2E, realSize string
}

func (t testData) Get(key string) string {
	field := reflect.ValueOf(&t).Elem().FieldByName(key)
	if field.IsValid() {
		return field.String()
	}
	return ""
}
