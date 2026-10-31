//go:build test

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filerequest"
	"github.com/Activer007/SupaShare/internal/test"
)

func collectionShareFixture(t *testing.T) (models.File, []byte) {
	t.Helper()
	payload := []byte("collection share fixture " + rand.Text())
	hash := sha1.Sum(payload)
	file := models.File{Id: "m6-" + rand.Text(), UserId: idUser, Name: "collected.txt", ContentType: "text/plain",
		SHA1: hex.EncodeToString(hash[:]), SizeBytes: int64(len(payload)), Size: "small file", UploadRequestId: "request-" + rand.Text(),
		ExpireAt: time.Now().Add(time.Hour).Unix(), UploadDate: time.Now().Unix(), DownloadsRemaining: 3, DownloadCount: 2,
		PasswordHash: configuration.HashPassword("collection-password", false, "")}
	path := filepath.Join(configuration.Get().DataDir, file.SHA1)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	database.SaveMetaData(file)
	t.Cleanup(func() { database.DeleteMetaData(file.Id); _ = os.Remove(path) })
	return file, payload
}

func TestCollectionToShare(t *testing.T) {
	source, payload := collectionShareFixture(t)
	request := models.FileRequest{Id: source.UploadRequestId, UserId: source.UserId, AllowedDownloads: 3}
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	key := generateNewKey(true, idUser, "collection conversion", "")
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	w, r := getRecorder("/files/duplicate", key.Id, []test.Header{{Name: "id", Value: source.Id}, {Name: "asShare", Value: "true"}})
	Process(w, r)
	var output models.FileApiOutput
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &output) != nil || output.UrlDownload == "" || output.IsFileRequest {
		t.Fatalf("convert status=%d output=%s", w.Code, w.Body.String())
	}
	share, found := database.GetMetaDataById(output.Id)
	t.Cleanup(func() { database.DeleteMetaData(output.Id) })
	expected := source
	expected.Id, expected.UploadRequestId, expected.DownloadCount = output.Id, "", 0
	if !found || share.Id == source.Id || !reflect.DeepEqual(share, expected) {
		t.Fatal("conversion changed content, password or rules, or retained the request association")
	}
	unchanged, found := database.GetMetaDataById(source.Id)
	if !found || !reflect.DeepEqual(unchanged, source) {
		t.Fatal("conversion changed the original collection file")
	}
	visible := false
	user, _ := database.GetUser(idUser)
	for _, file := range getFilesForUser(user, false, false) {
		visible = visible || file.Id == share.Id
	}
	if !visible {
		t.Fatal("ordinary file list omitted the new share")
	}
	if err := filerequest.DeleteContext(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, found := database.GetMetaDataById(source.Id); found {
		t.Fatal("collection deletion did not retire the source")
	}
	share, found = storage.GetFile(share.Id)
	if !found {
		t.Fatal("collection deletion removed the ordinary share's blob")
	}
	download := httptest.NewRecorder()
	if !storage.ServeFile(share, download, httptest.NewRequest(http.MethodGet, "/downloadFile?id="+share.Id, nil), true, true, false, true) ||
		download.Code != http.StatusOK || string(download.Body.Bytes()) != string(payload) {
		t.Fatal("ordinary share content failed after collection deletion")
	}
}

func TestCollectionToShareRejections(t *testing.T) {
	source, _ := collectionShareFixture(t)
	key := generateNewKey(true, idUser, "collection conversion", "")
	other := generateNewKey(true, idAdmin, "unprivileged actor", "")
	otherUser, _ := database.GetUser(idAdmin)
	database.SaveUser(models.User{Id: idAdmin, Name: "Unprivileged fixture", UserLevel: models.UserLevelUser}, false)
	t.Cleanup(func() {
		database.SaveUser(otherUser, false)
		database.DeleteApiKey(key.Id)
		database.DeleteApiKey(other.Id)
	})
	checks := []struct {
		name   string
		key    string
		edit   func(*models.File)
		extra  []test.Header
		status int
	}{
		{name: "unauthorized source", key: other.Id, status: http.StatusUnauthorized},
		{name: "pending source", key: key.Id, edit: func(f *models.File) { f.PendingDeletion = time.Now().Add(time.Hour).Unix() }, status: http.StatusNotFound},
		{name: "exhausted source", key: key.Id, edit: func(f *models.File) { f.DownloadsRemaining = 0 }, status: http.StatusNotFound},
		{name: "ordinary source", key: key.Id, edit: func(f *models.File) { f.UploadRequestId = "" }, status: http.StatusBadRequest},
		{name: "changing rules", key: key.Id, extra: []test.Header{{Name: "allowedDownloads", Value: "9"}}, status: http.StatusBadRequest},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			current := source
			if check.edit != nil {
				check.edit(&current)
			}
			database.SaveMetaData(current)
			before := len(database.GetAllMetadata())
			headers := append([]test.Header{{Name: "id", Value: source.Id}, {Name: "asShare", Value: "true"}}, check.extra...)
			w, r := getRecorder("/files/duplicate", check.key, headers)
			Process(w, r)
			if w.Code != check.status || len(database.GetAllMetadata()) != before {
				t.Fatalf("status=%d want=%d; failed conversion must leave no share", w.Code, check.status)
			}
		})
	}
}
