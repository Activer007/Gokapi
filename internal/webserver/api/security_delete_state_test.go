//go:build test

package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
)

func TestSEC040DeletionParameters(t *testing.T) {
	maxSeconds := int64(int(^uint(0)>>1) / 1000)
	if durationMax := int64(1<<63-1) / int64(time.Second); durationMax < maxSeconds {
		maxSeconds = durationMax
	}
	for _, delay := range []string{"-1", strconv.Itoa(int(^uint(0) >> 1)), strconv.FormatInt(int64(1<<63-1)/int64(time.Second)+1, 10)} {
		request := httptest.NewRequest(http.MethodPost, "/api/files/delete", nil)
		request.Header.Set("id", "synthetic-file")
		request.Header.Set("delay", delay)
		if err := (&paramFilesDelete{}).ParseRequest(request); err == nil {
			t.Fatal("negative or overflowing delay was accepted")
		}
	}
	for _, delay := range []string{"0", "1", strconv.FormatInt(maxSeconds, 10)} {
		request := httptest.NewRequest(http.MethodPost, "/api/files/delete", nil)
		request.Header.Set("id", "synthetic-file")
		request.Header.Set("delay", delay)
		parsed := &paramFilesDelete{}
		if err := parsed.ParseRequest(request); err != nil || parsed.Request != request {
			t.Fatal("safe delay or originating HTTP context was lost")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/files/restore", nil)
	restore, userDelete, requestDelete := &paramFilesRestore{}, &paramUserDelete{}, &paramURequestDelete{}
	if restore.ProcessParameter(request) != nil || userDelete.ProcessParameter(request) != nil || requestDelete.ProcessParameter(request) != nil ||
		restore.Request != request || userDelete.Request != request || requestDelete.Request != request {
		t.Fatal("deletion parameter context was not retained")
	}
}

func TestSEC040FileStateAPI(t *testing.T) {
	dataDir, key := sec040APIStateSetup(t)
	call := func(ctx context.Context, route, id, delay string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/files/"+route, nil).WithContext(ctx)
		request.Header.Set("apikey", key.Id)
		request.Header.Set("id", id)
		if delay != "" {
			request.Header.Set("delay", delay)
		}
		response := httptest.NewRecorder()
		Process(response, request)
		return response
	}
	t.Run("accepted-and-restored", func(t *testing.T) {
		file := sec040APIStateFile(t, dataDir, "scheduled")
		t.Cleanup(func() {
			current, found := database.GetMetaDataById(file.Id)
			if found && current.PendingDeletion != 0 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if _, err := storage.CancelFileDeletionContext(ctx, current); err != nil {
					t.Error("scheduled fixture could not be canceled")
				}
			}
		})
		response := call(t.Context(), "delete", file.Id, "60")
		if response.Code != 200 || response.Body.Len() != 0 || response.Header().Get(deletionStatusHeader) != storage.DeletionAccepted {
			t.Fatal("scheduled deletion did not preserve empty success and accepted status")
		}
		pending, found := database.GetMetaDataById(file.Id)
		if !found || pending.PendingDeletion <= time.Now().Unix() {
			t.Fatal("accepted schedule was not persisted")
		}
		response = call(t.Context(), "restore", file.Id, "")
		var output models.Result
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &output) != nil || output.Result != "OK" || output.FileInfo.Id != file.Id || output.FileInfo.IsPendingDeletion {
			t.Fatal("restore did not preserve the existing successful JSON result")
		}
		current, found := database.GetMetaDataById(file.Id)
		if !found || !reflect.DeepEqual(current, file) {
			t.Fatal("restore changed fields beyond the pending state")
		}
		sec040APISourceUnchanged(t, dataDir, file)
	})
	for _, state := range []string{storage.DeletionDeleted, storage.DeletionShared} {
		t.Run(state, func(t *testing.T) {
			file := sec040APIStateFile(t, dataDir, state)
			other := file
			if state == storage.DeletionShared {
				other.Id, other.UserId, other.HotlinkId = file.Id+"-other", 9041, file.HotlinkId+"-other"
				database.SaveMetaData(other)
				database.SaveHotlink(other)
			}
			response := call(t.Context(), "delete", file.Id, "")
			if response.Code != 200 || response.Body.Len() != 0 || response.Header().Get(deletionStatusHeader) != state {
				t.Fatal("confirmed deletion did not preserve empty success and its confirmed status")
			}
			if _, found := database.GetMetaDataById(file.Id); found {
				t.Fatal("confirmed deletion left its metadata")
			}
			if state == storage.DeletionShared {
				current, found := database.GetMetaDataById(other.Id)
				if !found || !reflect.DeepEqual(current, other) {
					t.Fatal("shared deletion changed another owner's live share")
				}
				sec040APISourceUnchanged(t, dataDir, other)
			} else if _, err := os.Lstat(filepath.Join(dataDir, file.SHA1)); !os.IsNotExist(err) {
				t.Fatal("confirmed deletion left the original source")
			}
		})
	}
	t.Run("pending-storage-failure", func(t *testing.T) {
		file := sec040APIStateFile(t, dataDir, "pending")
		blockedRoot := filepath.Join(dataDir, "synthetic-not-a-directory")
		if err := os.WriteFile(blockedRoot, []byte("synthetic"), 0600); err != nil {
			t.Fatal("could not create the isolated I/O fault")
		}
		response := func() *httptest.ResponseRecorder {
			configuration.Get().DataDir = blockedRoot
			defer func() { configuration.Get().DataDir = dataDir }()
			return call(t.Context(), "delete", file.Id, "")
		}()
		if response.Code != 503 || response.Header().Get(deletionStatusHeader) != storage.DeletionPending || strings.Contains(response.Body.String(), blockedRoot) {
			t.Fatal("accepted physical failure lost pending status or disclosed its storage path")
		}
		current, found := database.GetMetaDataById(file.Id)
		if !found || current.UnlimitedTime || current.ExpireAt != 0 {
			t.Fatal("pending cleanup did not keep a revoked retryable record")
		}
		sec040APISourceUnchanged(t, dataDir, file)
	})
	t.Run("restore-conflict", func(t *testing.T) {
		file := sec040APIStateFile(t, dataDir, "conflict")
		file.UnlimitedTime = false
		file.ExpireAt = 0
		database.SaveMetaData(file)
		response := call(t.Context(), "restore", file.Id, "")
		current, found := database.GetMetaDataById(file.Id)
		if response.Code != 409 || !found || !reflect.DeepEqual(current, file) {
			t.Fatal("restore conflict was accepted or mutated the revoked share")
		}
		sec040APISourceUnchanged(t, dataDir, file)
	})
	t.Run("invalid-delay-and-cancellation", func(t *testing.T) {
		file := sec040APIStateFile(t, dataDir, "unchanged")
		if response := call(t.Context(), "delete", file.Id, "-1"); response.Code != 400 {
			t.Fatal("invalid delay was not an HTTP parse refusal")
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		for _, route := range []string{"delete", "restore"} {
			response := call(ctx, route, file.Id, "")
			// Cancellation can reject authentication before the operation runs.
			if (response.Code != http.StatusUnauthorized && response.Code != http.StatusServiceUnavailable) || response.Header().Get(deletionStatusHeader) != "" {
				t.Fatal("canceled request was accepted or reported a mutation")
			}
		}
		current, found := database.GetMetaDataById(file.Id)
		if !found || !reflect.DeepEqual(current, file) {
			t.Fatal("invalid or canceled request mutated metadata")
		}
		sec040APISourceUnchanged(t, dataDir, file)
	})
	for _, route := range []string{"delete", "restore"} {
		if response := call(t.Context(), route, "sec040-api-missing", ""); response.Code != 404 {
			t.Fatal("missing file status changed")
		}
	}
}

const sec040APIPayload = "synthetic SEC040 API object"

func sec040APIStateSetup(t *testing.T) (string, models.ApiKey) {
	t.Helper()
	config := configuration.Get()
	previousDir := config.DataDir
	previousDB, err := database.ParseUrl(config.DatabaseUrl, false)
	if err != nil {
		t.Fatal("existing test database could not be retained")
	}
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal("isolated data directory could not be created")
	}
	t.Cleanup(func() {
		database.Close()
		config.DataDir = previousDir
		filesystem.Init(previousDir)
		database.Connect(previousDB)
	})
	database.Close()
	database.Connect(models.DbConnection{HostUrl: filepath.Join(root, "state.sqlite")})
	config.DataDir = dataDir
	filesystem.Init(dataDir)
	for _, id := range []int{9040, 9041} {
		database.SaveUser(models.User{Id: id, Name: "synthetic-state-owner-" + strconv.Itoa(id), UserLevel: models.UserLevelUser}, false)
	}
	key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: 9040, Permissions: models.ApiPermDelete}
	database.SaveApiKey(key)
	if actor, _, ok := isValidApiKey(key.Id, false, models.ApiPermDelete); !ok || actor.Id != key.UserId {
		t.Fatal("positive API authorization precondition failed")
	}
	return dataDir, key
}

func sec040APIStateFile(t *testing.T, dataDir, label string) models.File {
	t.Helper()
	file := models.File{Id: "sec040-api-" + label, Name: "synthetic.bin", SHA1: "sec040-api-blob-" + label,
		UserId: 9040, SizeBytes: int64(len(sec040APIPayload)), UnlimitedTime: true, UnlimitedDownloads: true, HotlinkId: "sec040-api-" + label + ".png"}
	if err := os.WriteFile(filepath.Join(dataDir, file.SHA1), []byte(sec040APIPayload), 0600); err != nil {
		t.Fatal("isolated source could not be created")
	}
	database.SaveMetaData(file)
	database.SaveHotlink(file)
	return file
}

func sec040APISourceUnchanged(t *testing.T, dataDir string, file models.File) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dataDir, file.SHA1))
	if err != nil || string(data) != sec040APIPayload {
		t.Fatal("source bytes changed")
	}
}
