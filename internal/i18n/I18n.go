// Package i18n provides the two built-in Web UI languages.
package i18n

import (
	"embed"
	"encoding/json"
	"net/http"
)

const English = "en"
const Chinese = "zh-CN"
const CookieName = "gokapi_language"

//go:embed locales/*.json
var resources embed.FS

var dictionaries = loadDictionaries()

func loadDictionaries() map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, language := range []string{English, Chinese} {
		data, err := resources.ReadFile("locales/" + language + ".json")
		if err != nil {
			panic(err)
		}
		var messages map[string]string
		if err = json.Unmarshal(data, &messages); err != nil {
			panic(err)
		}
		result[language] = messages
	}
	return result
}

func Supported(language string) bool {
	return language == English || language == Chinese
}

// Locale is request-local; the shared dictionaries are never changed.
type Locale struct {
	Language string
}

func New(language string) Locale {
	if !Supported(language) {
		language = English
	}
	return Locale{Language: language}
}

// Select chooses an explicit URL preference, then a cookie, then the site default.
func Select(r *http.Request, defaultLanguage string) Locale {
	if language := r.URL.Query().Get("lang"); Supported(language) {
		return New(language)
	}
	if cookie, err := r.Cookie(CookieName); err == nil && Supported(cookie.Value) {
		return New(cookie.Value)
	}
	return New(defaultLanguage)
}

// RememberPreference runs before redirects/authentication, so an explicit choice
// survives navigation to login or an error page without changing redirect URLs.
func RememberPreference(next http.Handler, cookiePath string, secure bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if language := r.URL.Query().Get("lang"); Supported(language) {
			http.SetCookie(w, &http.Cookie{Name: CookieName, Value: language,
				Path: cookiePath, MaxAge: 365 * 24 * 60 * 60, HttpOnly: true,
				SameSite: http.SameSiteLaxMode, Secure: secure})
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (l Locale) T(key string) string {
	if message := dictionaries[l.Language][key]; message != "" {
		return message
	}
	if message := dictionaries[English][key]; message != "" {
		return message
	}
	return key
}

// Messages returns a separate map suitable for html/template's JSON encoding.
func (l Locale) Messages() map[string]string {
	result := make(map[string]string, len(dictionaries[English]))
	for key := range dictionaries[English] {
		result[key] = l.T(key)
	}
	return result
}
