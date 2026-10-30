//go:build test && !integration

package webserver

import (
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

// The create form receives policy independently of the list it happens to show.
// Use the actual view conversion and template together: a hand-built view with
// assigned limits would conceal the first-collection initialization failure.
func TestCollectionInitialLimits(t *testing.T) {
	saved := *configuration.Get()
	previousDB, err := database.ParseUrl(saved.DatabaseUrl, false)
	if err != nil {
		t.Fatal("initial-limits setup could not retain the original database", err)
	}
	database.Close()
	t.Cleanup(func() {
		// All quota subtests have restored their OS environment by this point.
		// Reload its parsed copy as well, then restore the original settings/DB.
		database.Close()
		configuration.Load()
		*configuration.Get() = saved
		database.Connect(previousDB)
	})
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "initial-limits.sqlite")}
			if backend == "redis" {
				connection = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "initial_limits_"}
			}
			database.Connect(connection)
			t.Cleanup(database.Close)
			ordinary := models.User{Id: 9702, Name: "Synthetic collection owner", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
			other := models.User{Id: 9703, Name: "Synthetic other owner", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
			admin := models.User{Id: 9704, Name: "Synthetic administrator", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}
			super := models.User{Id: 9705, Name: "Synthetic super administrator", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
			for _, user := range []models.User{ordinary, other, admin, super} {
				database.SaveUser(user, false)
			}
			for _, limits := range []struct {
				name          string
				files, sizeMB int
			}{
				{"finite", 7, 11}, {"files-unlimited", 0, 11},
				{"size-unlimited", 7, 0}, {"both-unlimited", 0, 0},
			} {
				t.Run(limits.name, func(t *testing.T) {
					t.Setenv("GOKAPI_MAX_FILES_GUESTUPLOAD", strconv.Itoa(limits.files))
					t.Setenv("GOKAPI_MAX_SIZE_GUESTUPLOAD", strconv.Itoa(limits.sizeMB))
					configuration.Load()
					env := configuration.GetEnvironment()
					if env.MaxFilesGuestUpload != limits.files || env.MaxSizeGuestUploadMb != limits.sizeMB {
						t.Fatal("initial-limits setup did not apply the configured server policy")
					}
					for _, list := range []struct {
						name  string
						owner int
					}{
						{"empty", 0}, {"other-owner", other.Id},
						{"orphan-owner", 9799}, {"visible-own", ordinary.Id},
					} {
						t.Run(list.name, func(t *testing.T) {
							if list.owner != 0 {
								request := models.FileRequest{Id: "initial-" + list.name, Name: "Synthetic " + list.name, UserId: list.owner,
									ApiKey: "synthetic-initial-" + list.name, MaxFiles: 1, MaxSize: 1, AllowedDownloads: 1, Expiry: time.Now().Add(time.Hour).Unix()}
								database.SaveFileRequest(request)
								t.Cleanup(func() { database.DeleteFileRequest(request) })
							}
							for _, actor := range []struct {
								name string
								user models.User
							}{
								{"ordinary", ordinary}, {"administrator", admin}, {"super-administrator", super},
							} {
								t.Run(actor.name, func(t *testing.T) {
									wantFiles, wantSize := limits.files, limits.sizeMB
									if actor.user.IsAdmin() {
										wantFiles, wantSize = 0, 0
									}
									view := (&AdminView{}).convertGlobalConfig(ViewFileRequests, actor.user)
									wantVisible := 0
									if list.owner == ordinary.Id || (list.owner == other.Id && actor.user.IsAdmin()) {
										wantVisible = 1
									}
									if len(view.FileRequests) != wantVisible {
										t.Fatalf("initial-limits visibility precondition=%d, want %d", len(view.FileRequests), wantVisible)
									}
									if view.FileRequestMaxFiles != wantFiles || view.FileRequestMaxSize != wantSize {
										t.Errorf("FIRST_COLLECTION_POLICY: view limits=%d/%d, want %d/%d independently of visible collections",
											view.FileRequestMaxFiles, view.FileRequestMaxSize, wantFiles, wantSize)
									}
									response := httptest.NewRecorder()
									if err := renderPage(response, httptest.NewRequest("GET", "/filerequests?lang=en", nil), "uploadreq", view); err != nil {
										t.Fatal("initial-limits actual collection template failed", err)
									}
									body := response.Body.String()
									if !strings.Contains(body, `id="button-newfr"`) || strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "<no value>") {
										t.Fatal("initial-limits rendered collection form is absent or invalid")
									}
									for variable, want := range map[string]int{"limitMaxFiles": wantFiles, "limitMaxSize": wantSize} {
										pattern := regexp.MustCompile(`\bvar\s+` + variable + `\s*=\s*([0-9]+)\s*;`)
										match := pattern.FindStringSubmatch(body)
										if len(match) != 2 {
											t.Fatalf("initial-limits rendered template omitted numeric %s", variable)
										}
										got, err := strconv.Atoi(match[1])
										if err != nil || got != want {
											t.Errorf("FIRST_COLLECTION_POLICY: rendered %s=%s, want %s", variable, match[1], fmt.Sprint(want))
										}
									}
								})
							}
						})
					}
				})
			}
		})
	}
}
