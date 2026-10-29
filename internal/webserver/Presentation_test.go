//go:build test && !integration

package webserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/i18n"
	"github.com/Activer007/Gokapi/internal/models"
)

func TestUIPresentationAndCookie(t *testing.T) {
	config := configuration.Get()
	previous := config.UI
	defer func() { config.UI = previous }()
	config.UI = &models.UIConfig{DefaultLanguage: "zh-CN", Title: `<script>alert("title")</script>`,
		FooterText: `<img src=x onerror=alert(1)>`, FooterLinks: []models.FooterLink{{Text: "Help", URL: "https://example.com/help"}}}
	request := httptest.NewRequest("GET", "/login?lang=zh-CN", nil)
	response := httptest.NewRecorder()
	withLanguagePreference(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := renderPage(w, r, "login", &LoginView{PublicName: config.PublicName}); err != nil {
			t.Fatal(err)
		}
	})).ServeHTTP(response, request)
	body := response.Body.String()
	for _, want := range []string{`lang="zh-CN"`, "用户名", "登录 · &lt;script&gt;", "&lt;img", `https://example.com/help`, `window.gokapiMessages = {`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing rendered output %q", want)
		}
	}
	for _, forbidden := range []string{`<script>alert`, `<img src=x`, `"SaltAdmin"`, `"Authentication"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("unsafe/private output %q", forbidden)
		}
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != i18n.CookieName || cookies[0].Value != "zh-CN" || !cookies[0].HttpOnly || cookies[0].Path == "" {
		t.Fatalf("invalid language cookie: %v", cookies)
	}
	if response.Header().Get("Cache-Control") != "no-store, no-cache" {
		t.Fatal("language-specific pages must not be shared in caches")
	}
	config.UI = nil
	response = httptest.NewRecorder()
	if err := renderPage(response, httptest.NewRequest("GET", "/login", nil), "login", &LoginView{PublicName: config.PublicName}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Body.String(), `lang="en"`) || !strings.Contains(response.Body.String(), "Powered by") {
		t.Fatal("legacy defaults were not preserved")
	}
}

func TestUIPasswordTitlesHideFilename(t *testing.T) {
	for _, name := range []string{"download_password", "redirect_filename"} {
		response := httptest.NewRecorder()
		var err error
		if name == "download_password" {
			err = renderPage(response, httptest.NewRequest("GET", "/d?lang=zh-CN", nil), name,
				&DownloadView{Name: "secret-file.txt", IsPasswordView: true, IsDownloadView: true})
		} else {
			err = renderPage(response, httptest.NewRequest("GET", "/d/id/name?lang=zh-CN", nil), name,
				&redirectValues{Name: "secret-file.txt", PasswordRequired: true})
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(response.Body.String(), "secret-file.txt") || !strings.Contains(response.Body.String(), "需要密码") {
			t.Errorf("password-protected %s exposes filename or has incorrect title", name)
		}
	}
}

func TestUIPublicPages(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		for _, tc := range []struct {
			name   string
			view   interface{ setUI(pageUI) }
			en, zh string
		}{
			{"forgotpw", &genericView{}, "If you forgot your user password", "如果忘记了用户密码"},
			{"changepw", &genericView{ErrorMessage: "password.tooShort"}, "Password is too short", "密码过短"},
			{"download", &DownloadView{Name: "example.txt", IsDownloadView: true}, "Download", "下载文件"},
			{"download", &DownloadView{Name: "ciphertext", Id: "encrypted", IsDownloadView: true, ClientSideDecryption: true, EndToEndEncryption: true}, "Decrypting...", "正在解密"},
			{"publicUpload", &publicUploadView{FileRequest: &models.FileRequest{Notes: "Literal note", MaxFiles: 2, MaxSize: 10}}, "Upload restrictions", "上传限制"},
			{"error", &genericView{IsGenericError: true, ErrorId: 0}, "File not found", "文件不存在"},
			{"error", &genericView{IsGenericError: true, ErrorId: 1}, "Unable to upload files", "无法上传文件"},
			{"error", &genericView{IsGenericError: true, ErrorId: 2}, "Missing or invalid decryption key", "解密密钥缺失或无效"},
			{"error", &genericView{IsGenericError: true, ErrorId: 3}, "Unauthorised user", "无权访问"},
			{"error", &genericView{ErrorTitle: "error.unknownID.title", ErrorMessage: "error.unknownID.help"}, "Unknown error ID", "错误信息已失效"},
		} {
			response := httptest.NewRecorder()
			if err := renderPage(response, httptest.NewRequest("GET", "/?lang="+language, nil), tc.name, tc.view); err != nil {
				t.Fatalf("%s %s: %v", language, tc.name, err)
			}
			want := tc.en
			if language == "zh-CN" {
				want = tc.zh
			}
			if !strings.Contains(response.Body.String(), want) {
				t.Errorf("%s %s is missing %q", language, tc.name, want)
			}
			if strings.Contains(response.Body.String(), "ZgotmplZ") {
				t.Errorf("%s %s has a contextual template error", language, tc.name)
			}
		}
	}
}

func TestUIUploadManagementContexts(t *testing.T) {
	user := models.User{Id: 5, Name: "fixture", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, language := range []string{"en", "zh-CN"} {
		for _, name := range []string{"admin", "uploadreq"} {
			view := &AdminView{IsAdminView: true, ActiveUser: user, UserMap: map[int]*models.User{5: &user},
				Items: []models.FileApiOutput{{Id: "file1", Name: "literal-name.txt", UploaderId: 5, UnlimitedTime: true, UnlimitedDownloads: true}},
				FileRequests: []models.FileRequest{{Id: "request1", Name: "Literal request", UserId: 5, UploadedFiles: 1,
					FileIdList: []string{"file2"}, Files: []models.File{{Id: "file2", Name: "literal-upload.txt", UserId: 5}}},
					{Id: "request2", Name: "Zip request", UserId: 5, UploadedFiles: 2, FileIdList: []string{"file3", "file4"}}}}
			response := httptest.NewRecorder()
			if err := renderPage(response, httptest.NewRequest("GET", "/?lang="+language, nil), name, view); err != nil {
				t.Fatalf("%s %s: %v", language, name, err)
			}
			body := response.Body.String()
			for _, key := range []string{"common.delete", "common.saveChanges"} {
				if name == "uploadreq" && key == "common.saveChanges" {
					key = "request.deleteTitle"
				}
				if !strings.Contains(body, i18n.New(language).T(key)) {
					t.Errorf("%s %s missing subtemplate message %s", language, name, key)
				}
			}
			if strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "<no value>") {
				t.Errorf("%s %s has invalid template output", language, name)
			}
		}
	}
}

func TestUIAccountAndStatusPages(t *testing.T) {
	user := models.User{Id: 5, Name: "Fixture", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	for _, language := range []string{"en", "zh-CN"} {
		for _, name := range []string{"users", "api", "logs", "e2esetup"} {
			response := httptest.NewRecorder()
			r := httptest.NewRequest("GET", "/?lang="+language, nil)
			var err error
			if name == "e2esetup" {
				err = renderPage(response, r, name, &e2ESetupView{})
			} else {
				view := &AdminView{IsAdminView: true, ActiveUser: user, UserMap: map[int]*models.User{5: &user},
					Users: []userInfo{{User: user}}, ApiKeys: []models.ApiKey{{Id: "fixture-key", PublicId: "public-key", UserId: 5}}}
				err = renderPage(response, r, name, view)
			}
			if err != nil {
				t.Fatalf("%s %s: %v", language, name, err)
			}
			key := map[string]string{"users": "users.group", "api": "api.help", "logs": "status.uptime", "e2esetup": "e2e.backup"}[name]
			if !strings.Contains(response.Body.String(), i18n.New(language).T(key)) || strings.Contains(response.Body.String(), "ZgotmplZ") {
				t.Errorf("%s %s missing localized content or invalid output", language, name)
			}
			if name == "users" && !strings.Contains(response.Body.String(), i18n.New(language).T("user.level.0")) {
				t.Error("group name was not translated")
			}
		}
	}
}
