//go:build test && noaws && security_deletion

package api

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
)

func TestSEC040DeleteUserKeepsFailedCleanupAnchor(t *testing.T) {
	actor, found := database.GetUser(idSuperAdmin)
	if !found || !actor.IsSuperAdmin() || !actor.HasPermissionManageUsers() {
		t.Fatal("positive user-management actor is unavailable")
	}
	name := "sec040-delete-user-" + rand.Text()
	database.SaveUser(models.User{Name: name, UserLevel: models.UserLevelUser}, true)
	owner, found := database.GetUserByName(name)
	if !found || owner.Id <= 0 || owner.Id == actor.Id {
		t.Fatal("positive isolated owner could not be created")
	}
	t.Cleanup(func() { database.DeleteUser(owner.Id) })
	key := generateNewKey(false, actor.Id, "synthetic deletion contract", "")
	key.GrantPermission(models.ApiPermManageUsers)
	database.SaveApiKey(key)
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	if authenticated, _, ok := isValidApiKey(key.Id, true, models.ApiPermManageUsers); !ok || authenticated.Id != actor.Id {
		t.Fatal("positive API authorization precondition failed")
	}

	source, err := os.CreateTemp(configuration.Get().DataDir, "sec040-delete-user-")
	if err != nil {
		t.Fatal("isolated source could not be created")
	}
	sourcePath := source.Name()
	markerPath := filepath.Join(sourcePath, "preserve.bin")
	t.Cleanup(func() {
		// Only remove this test's known child and then its own file or empty
		// directory; never recursively remove the configured data directory.
		_ = os.Remove(markerPath)
		_ = os.Remove(sourcePath)
	})
	const payload = "synthetic deletion retry source"
	_, writeErr := source.WriteString(payload)
	closeErr := source.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("isolated source could not be written")
	}
	file := models.File{
		Id:                 "sec040-delete-user-" + rand.Text(),
		Name:               "synthetic.bin",
		SHA1:               filepath.Base(sourcePath),
		HotlinkId:          "sec040-delete-user-" + rand.Text() + ".png",
		UserId:             owner.Id,
		SizeBytes:          int64(len(payload)),
		UnlimitedTime:      true,
		UnlimitedDownloads: true,
	}
	database.SaveMetaData(file)
	database.SaveHotlink(file)
	t.Cleanup(func() {
		database.DeleteHotlink(file.HotlinkId)
		database.DeleteMetaData(file.Id)
	})
	if current, ok := storage.GetFile(file.Id); !ok || current.UserId != owner.Id {
		t.Fatal("live isolated share precondition failed")
	}
	// Replace only our own regular source with a nonempty directory. Checked
	// physical cleanup must refuse it and keep the revoked management anchor.
	if err := os.Remove(sourcePath); err != nil {
		t.Fatal("isolated source fault could not be prepared")
	}
	if err := os.Mkdir(sourcePath, 0700); err != nil {
		t.Fatal("isolated directory fault could not be created")
	}
	if err := os.WriteFile(markerPath, []byte(payload), 0600); err != nil {
		t.Fatal("isolated directory marker could not be created")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/user/delete", nil).WithContext(t.Context())
	request.Header.Set("apikey", key.Id)
	request.Header.Set("userid", strconv.Itoa(owner.Id))
	request.Header.Set("deleteFiles", "true")
	response := httptest.NewRecorder()
	Process(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Errorf("failed physical cleanup must remain retryable: got HTTP %d, want 503", response.Code)
	}
	if current, ok := database.GetUser(owner.Id); !ok || current.Id != owner.Id || current.Name != owner.Name {
		t.Error("failed cleanup removed the user needed for retry")
	}
	current, ok := database.GetMetaDataById(file.Id)
	if !ok {
		t.Error("failed cleanup removed its metadata anchor")
	} else {
		if current.UserId != owner.Id || current.SHA1 != file.SHA1 || current.HotlinkId != file.HotlinkId {
			t.Error("failed cleanup changed ownership or physical identity")
		}
		if current.UnlimitedTime || current.ExpireAt != 0 {
			t.Error("failed cleanup did not revoke the retained share")
		}
	}
	if id, ok := database.GetHotlink(file.HotlinkId); !ok || id != file.Id {
		t.Error("failed cleanup removed its hotlink management anchor")
	}
	if _, ok := storage.GetFile(file.Id); ok {
		t.Error("failed cleanup left the public share available")
	}
	if info, err := os.Lstat(sourcePath); err != nil || !info.IsDir() {
		t.Error("failed cleanup removed or replaced the source directory")
	}
	if data, err := os.ReadFile(markerPath); err != nil || string(data) != payload {
		t.Error("failed cleanup changed the source directory contents")
	}
}
