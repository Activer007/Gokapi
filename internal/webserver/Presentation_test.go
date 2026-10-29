//go:build test && !integration

package webserver

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/i18n"
	"github.com/forceu/gokapi/internal/models"
)

func TestUIPresentationAndCookie(t *testing.T) {
	config := configuration.Get()
	previous := config.UI
	defer func() { config.UI = previous }()
	config.UI = &models.UIConfig{DefaultLanguage: "zh-CN", Title: `<script>alert("title")</script>`,
		FooterText: `<img src=x onerror=alert(1)>`, FooterLinks: []models.FooterLink{{Text: "Help", URL: "https://example.com/help"}}}
	request := httptest.NewRequest("GET", "/login?lang=zh-CN", nil)
	response := httptest.NewRecorder()
	if err := renderPage(response, request, "login", &LoginView{PublicName: config.PublicName}); err != nil {
		t.Fatal(err)
	}
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
