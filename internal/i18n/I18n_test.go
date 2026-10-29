package i18n

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestSelectLanguage(t *testing.T) {
	for _, tc := range []struct{ query, cookie, site, want string }{
		{"zh-CN", "en", "en", Chinese}, {"en", "zh-CN", Chinese, English},
		{"invalid", "zh-CN", "en", Chinese}, {"", "invalid", Chinese, Chinese},
		{"", "", "", English}, {"", "", "invalid", English},
	} {
		r := httptest.NewRequest("GET", "/login?lang="+tc.query, nil)
		r.AddCookie(&http.Cookie{Name: CookieName, Value: tc.cookie})
		if got := Select(r, tc.site).Language; got != tc.want {
			t.Errorf("query=%q cookie=%q default=%q: got %s, want %s", tc.query, tc.cookie, tc.site, got, tc.want)
		}
	}
}

func TestDictionariesAndConcurrentLocales(t *testing.T) {
	for language, messages := range dictionaries {
		for key := range dictionaries[English] {
			if messages[key] == "" {
				t.Errorf("%s is missing %s", language, key)
			}
		}
		for key := range messages {
			if dictionaries[English][key] == "" {
				t.Errorf("%s has unknown key %s", language, key)
			}
		}
	}
	var group sync.WaitGroup
	for range 20 {
		for language, want := range map[string]string{English: "Login", Chinese: "登录"} {
			group.Go(func() {
				locale := New(language)
				messages := locale.Messages()
				messages["page.login"] = "changed"
				if locale.T("page.login") != want {
					t.Errorf("locale %s changed", language)
				}
			})
		}
	}
	group.Wait()
	if got := (Locale{Language: "invalid"}).T("page.login"); got != "Login" {
		t.Fatal("missing language must fall back to English")
	}
	if got := New(Chinese).T("unknown.key"); got != "unknown.key" {
		t.Fatal("unknown keys must remain identifiable")
	}
}
