//go:build test && !integration

package webserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestPublicUploadURLFormsShareLiveCredentialChecks(t *testing.T) {
	for _, length := range []int{22, 30} {
		for _, state := range []string{"valid", "ordinary-key", "wrong-request", "wrong-owner", "rotated-key", "expired-request", "expired-key", "revoked", "missing-owner", "full"} {
			t.Run(state+"-"+strings.Repeat("x", length), func(t *testing.T) {
				request := models.FileRequest{Id: helper.GenerateRandomString(15), UserId: 5, Name: "URL fixture",
					ApiKey: helper.GenerateRandomString(length), MaxFiles: 1, Expiry: time.Now().Add(time.Hour).Unix()}
				if length == 22 {
					request.ApiKey = "_-" + request.ApiKey[2:]
				}
				key := models.ApiKey{Id: request.ApiKey, PublicId: helper.GenerateRandomString(35), UserId: request.UserId, UploadRequestId: request.Id}
				switch state {
				case "ordinary-key":
					key.UploadRequestId = ""
				case "wrong-request":
					key.UploadRequestId = "different-request"
				case "wrong-owner":
					key.UserId = 7
				case "rotated-key":
					request.ApiKey = helper.GenerateRandomString(length)
				case "expired-request":
					request.Expiry = time.Now().Add(-time.Hour).Unix()
				case "expired-key":
					key.Expiry = time.Now().Add(-time.Hour).Unix()
				case "missing-owner":
					request.UserId, key.UserId = 1900000000, 1900000000
				case "full":
					file := models.File{Id: helper.GenerateRandomString(15), UserId: request.UserId, UploadRequestId: request.Id}
					database.SaveMetaData(file)
					t.Cleanup(func() { database.DeleteMetaData(file.Id) })
				}
				database.SaveFileRequest(request)
				t.Cleanup(func() { database.DeleteFileRequest(request) })
				if state != "revoked" {
					database.SaveApiKey(key)
				}
				t.Cleanup(func() { database.DeleteApiKey(key.Id) })
				mux := http.NewServeMux()
				mux.HandleFunc("/r/{token}", showPublicUpload)
				mux.HandleFunc("/publicUpload", showPublicUpload)
				for _, path := range []string{"/r/" + key.Id, "/publicUpload?id=" + request.Id + "&key=" + key.Id} {
					response := httptest.NewRecorder()
					mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
					if state == "valid" {
						if response.Code != 200 || !strings.Contains(response.Body.String(), `id="publicUploadTitle"`) ||
							!strings.Contains(response.Body.String(), key.Id) {
							t.Fatalf("valid route failed: %s status=%d", path, response.Code)
						}
					} else if response.Code == 200 || strings.Contains(response.Body.String(), `id="publicUploadTitle"`) || strings.Contains(response.Body.String(), key.Id) {
						t.Fatalf("invalid collection admitted: state=%s path=%s status=%d", state, path, response.Code)
					}
				}
			})
		}
	}
}

func TestPublicUploadShortRouteResolvesSubdirectoryAssetsAndAPI(t *testing.T) {
	config := configuration.Get()
	oldURL := config.ServerUrl
	t.Cleanup(func() { config.ServerUrl = oldURL })
	config.ServerUrl = "https://example.test/team/share/"
	request := models.FileRequest{Id: helper.GenerateRandomString(15), UserId: 5, Name: "Subdirectory fixture", ApiKey: helper.GenerateRandomString(22)}
	database.SaveFileRequest(request)
	database.SaveApiKey(models.ApiKey{Id: request.ApiKey, PublicId: helper.GenerateRandomString(35), UserId: 5, UploadRequestId: request.Id})
	t.Cleanup(func() { database.DeleteFileRequest(request); database.DeleteApiKey(request.ApiKey) })
	mux := http.NewServeMux()
	mux.HandleFunc("/r/{token}", showPublicUpload)
	proxy := http.StripPrefix("/team/share", mux)
	response := httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest("GET", "/team/share/r/"+request.ApiKey, nil))
	body := response.Body.String()
	if response.Code != 200 || !strings.Contains(body, `<base href="/team/share/">`) {
		t.Fatalf("subdirectory page lost its configured base: status=%d", response.Code)
	}
	base, _ := url.Parse("https://browser.test" + publicUploadBasePath())
	for _, resource := range []string{"./assets/dist/js/jquery.min.js", "./js/min/public_upload.min.js", "./api/uploadrequest/chunk/reserve"} {
		relative, _ := url.Parse(resource)
		resolved := base.ResolveReference(relative)
		if resolved.Host != "browser.test" || !strings.HasPrefix(resolved.Path, "/team/share/") || strings.Contains(resolved.Path, "/r/") {
			t.Fatalf("incorrect resource resolution: %s", resolved)
		}
		if !strings.Contains(body, resource) && !strings.Contains(body, `"./api/uploadrequest/chunk/"`) {
			t.Fatalf("resource fixture does not match the page: %s", resource)
		}
	}
	response = httptest.NewRecorder()
	proxy.ServeHTTP(response, httptest.NewRequest("GET", "/team/share/r/unknown-key", nil))
	if response.Code != 404 || !strings.Contains(response.Body.String(), `<base href="/team/share/">`) {
		t.Fatal("invalid short link must render its error with working subdirectory assets")
	}
}

func TestDownloadMetadataAppearsForPlainAndEncryptedFiles(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		for _, encrypted := range []bool{false, true} {
			response := httptest.NewRecorder()
			view := &DownloadView{Name: "fixture.bin", Id: "fixture", Size: "42 B", UploadDate: 1748180908,
				IsDownloadView: true, ClientSideDecryption: encrypted, EndToEndEncryption: encrypted}
			if err := renderPage(response, httptest.NewRequest("GET", "/d?lang="+language, nil), "download", view); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			for _, want := range []string{`class="download-metadata"`, "42 B", `id="downloadUploadDate"`, `insertLocalUploadDate(`, "1748180908"} {
				if !strings.Contains(body, want) {
					t.Errorf("language=%s encrypted=%v: missing %q", language, encrypted, want)
				}
			}
			label := "Uploaded"
			if language == "zh-CN" {
				label = "上传时间"
			}
			if !strings.Contains(body, label) {
				t.Fatal("upload date label was not localized")
			}
		}
	}
}

func TestCollectionTableUsesSameShortURLForOpenAndCopy(t *testing.T) {
	owner := models.User{Id: 5, UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	view := &AdminView{IsAdminView: true, ActiveUser: owner, UserMap: map[int]*models.User{5: &owner}, ServerUrl: "https://example.test/team/",
		FileRequests: []models.FileRequest{{Id: "internal-id", ApiKey: "collection-token", UserId: 5, Name: "Fixture collection"}}}
	response := httptest.NewRecorder()
	if err := renderPage(response, httptest.NewRequest("GET", "/filerequests", nil), "uploadreq", view); err != nil {
		t.Fatal(err)
	}
	body := response.Body.String()
	for _, want := range []string{`href="https://example.test/team/r/collection-token"`, `data-clipboard-text="https://example.test/team/r/collection-token"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("collection link missing: %s", want)
		}
	}
	if strings.Contains(body, "publicUpload?id=") {
		t.Fatal("collection table still generates legacy links")
	}
}
