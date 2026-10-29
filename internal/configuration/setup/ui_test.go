//go:build test

package setup

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/i18n"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestUISetupPresentation(t *testing.T) {
	previousInitial, previousSettings := isInitialSetup, *configuration.Get()
	t.Cleanup(func() { isInitialSetup = previousInitial; *configuration.Get() = previousSettings })
	isInitialSetup = true
	if language, title := setupPresentationDefaults(); language != "en" || title != "" {
		t.Fatal("first setup must not read presentation from an existing configuration")
	}
	isInitialSetup = false
	configuration.Get().PublicName = "Legacy title"
	configuration.Get().UI = nil
	if _, title := setupPresentationDefaults(); title != "Legacy title" {
		t.Fatal("legacy name fallback lost")
	}
	configuration.Get().UI = &models.UIConfig{DefaultLanguage: "zh-CN", Title: "团队文件中心"}
	if language, title := setupPresentationDefaults(); language != "zh-CN" || title != "团队文件中心" {
		t.Fatal("reconfiguration presentation lost")
	}
	response := httptest.NewRecorder()
	handleShowMaintenance(response, httptest.NewRequest("GET", "/", nil))
	if response.Body.String() != i18n.New("zh-CN").T("setup.maintenance") {
		t.Fatal("maintenance did not use site default")
	}
	response = httptest.NewRecorder()
	handleShowMaintenance(response, httptest.NewRequest("GET", "/?lang=en", nil))
	if response.Body.String() != i18n.New("en").T("setup.maintenance") {
		t.Fatal("explicit maintenance language lost")
	}
}

func TestUISetupTemplate(t *testing.T) {
	templates, err := template.ParseFS(templateFolderEmbedded, "templates/*.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	for _, initial := range []bool{true, false} {
		for _, language := range []string{"en", "zh-CN"} {
			view := setupView{UI: i18n.New(language), UITitle: "<script>team</script>", IsInitialSetup: initial,
				HasAwsFeature: true, MinPasswordLength: 8, ProtectedUrls: []string{"/admin"}}
			response := httptest.NewRecorder()
			if err := templates.ExecuteTemplate(response, "setup", view); err != nil {
				t.Fatal(err)
			}
			body := response.Body.String()
			if !strings.Contains(body, `<html lang="`+language+`">`) || !strings.Contains(body, view.UI.T("setup.storageChoose")) {
				t.Fatal("missing localized setup content")
			}
			if strings.Contains(body, "ZgotmplZ") || strings.Contains(body, "<title><script>") || !strings.Contains(body, "&lt;script&gt;") {
				t.Fatal("setup title was not escaped correctly")
			}
		}
	}
}
