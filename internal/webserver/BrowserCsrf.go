package webserver

import (
	"net/http"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/helper"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
)

const browserCSRFLength = 40

func browserCSRFCookieName() string {
	if configuration.UsesHTTPS() {
		return "__Host-gokapi_csrf"
	}
	return "gokapi_csrf"
}

func browserCSRFBinding(r *http.Request) string {
	cookies := r.CookiesNamed(browserCSRFCookieName())
	if len(cookies) != 1 || len(cookies[0].Value) != browserCSRFLength {
		return ""
	}
	for _, c := range cookies[0].Value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return ""
		}
	}
	return cookies[0].Value
}

func newBrowserCSRF(w http.ResponseWriter, r *http.Request, tokenType int) string {
	binding := browserCSRFBinding(r)
	if binding == "" {
		binding = helper.GenerateRandomString(browserCSRFLength)
	}
	w.Header().Set("Cache-Control", "no-store")
	http.SetCookie(w, &http.Cookie{Name: browserCSRFCookieName(), Value: binding, Path: "/", MaxAge: 1800,
		HttpOnly: true, Secure: configuration.UsesHTTPS(), SameSite: http.SameSiteLaxMode})
	return csrftoken.GenerateBound(tokenType, binding)
}
