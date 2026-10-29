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
