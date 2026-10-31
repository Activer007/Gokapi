//go:build test && !integration

package webserver

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestUILoginArtworkCatalog(t *testing.T) {
	for _, language := range []string{"en", "zh-CN"} {
		response := httptest.NewRecorder()
		view := &LoginView{PublicName: "Team", User: "<test>", IsFailedLogin: true}
		if err := renderPage(response, httptest.NewRequest("GET", "/login?lang="+language, nil), "login", view); err != nil {
			t.Fatal(err)
		}
		body := response.Body.String()
		for _, want := range []string{`data-retry="true"`, `aria-describedby="loginError"`, `value="&lt;test&gt;"`, `autocomplete="current-password"`, `name="csrf-token"`} {
			if !strings.Contains(body, want) {
				t.Errorf("missing login contract %s", want)
			}
		}
		tagline := "Connect files. Connect collaboration."
		if language == "zh-CN" {
			tagline = "连接文件，连接协作。"
		}
		if !strings.Contains(body, `class="login-tagline">`+tagline+`</p>`) {
			t.Fatal("missing translated login tagline")
		}
		entries := regexp.MustCompile(`data-login-art-id="([a-z0-9-]+)" data-login-art-src="\./(assets/brand/login/[a-z0-9-]+\.webp)"`).FindAllStringSubmatch(body, -1)
		if len(entries) == 0 {
			t.Fatal("empty artwork catalog")
		}
		ids := map[string]bool{}
		for _, entry := range entries {
			if ids[entry[1]] {
				t.Fatalf("duplicate artwork ID %s", entry[1])
			}
			ids[entry[1]] = true
			data, err := staticFolderEmbedded.ReadFile("web/static/" + entry[2])
			if err != nil {
				t.Fatal(err)
			}
			if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
				t.Fatalf("invalid WebP %s", entry[2])
			}
			if len(data) > 400*1024 {
				t.Fatalf("oversized login artwork %s", entry[2])
			}
		}
	}
}
