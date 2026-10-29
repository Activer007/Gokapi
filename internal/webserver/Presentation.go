package webserver

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/i18n"
	"github.com/forceu/gokapi/internal/models"
)

type pageUI struct {
	i18n.Locale
	Title       string
	Description string
	FooterText  string
	FooterLinks []models.FooterLink
	ShowVersion bool
}

type pageView struct {
	UI pageUI
}

func (p *pageView) setUI(ui pageUI) { p.UI = ui }

// renderPage adds request-local UI data without exposing the server configuration.
func renderPage(w http.ResponseWriter, r *http.Request, name string, view interface{ setUI(pageUI) }) error {
	config := configuration.Get()
	settings := models.UIConfig{}
	if config.UI != nil {
		settings = *config.UI
	}
	locale := i18n.Select(r, settings.DefaultLanguage)
	if language := r.URL.Query().Get("lang"); i18n.Supported(language) {
		cookiePath := "/"
		if base, err := url.Parse(config.ServerUrl); err == nil && strings.HasPrefix(base.Path, "/") {
			cookiePath = base.Path
		}
		http.SetCookie(w, &http.Cookie{Name: i18n.CookieName, Value: locale.Language,
			Path: cookiePath, MaxAge: 365 * 24 * 60 * 60, HttpOnly: true,
			SameSite: http.SameSiteLaxMode, Secure: configuration.UsesHTTPS()})
	}
	baseTitle := settings.Title
	if baseTitle == "" {
		baseTitle = config.PublicName
	}
	key := map[string]string{"admin": "nav.upload", "uploadreq": "nav.requests", "users": "nav.users",
		"api": "nav.api", "logs": "nav.status", "download_password": "page.password", "redirect_filename": "page.download"}[name]
	if key == "" {
		key = "page." + name
	}
	label := locale.T(key)
	description := ""
	showVersion := name == "admin" || name == "uploadreq" || name == "users" || name == "api" || name == "logs" || name == "e2esetup"
	switch data := view.(type) {
	case *DownloadView:
		if data.IsPasswordView {
			label = locale.T("page.password")
			description = label
		} else {
			label, description = data.Name, data.Size
		}
	case *redirectValues:
		if data.PasswordRequired {
			label = locale.T("page.password")
			description = label
		} else {
			label, description = data.Name, data.Size
		}
	}
	view.setUI(pageUI{Locale: locale, Title: label + " · " + baseTitle, Description: description,
		FooterText: settings.FooterText, FooterLinks: settings.FooterLinks, ShowVersion: showVersion})
	addNoCacheHeader(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return templateFolder.ExecuteTemplate(w, name, view)
}
