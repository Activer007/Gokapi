//go:build test && !integration

package webserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/webserver/authentication"
	"github.com/forceu/gokapi/internal/webserver/authentication/csrftoken"
)

func TestSEC012FileCookieTransport(t *testing.T) {
	previous := configuration.Get().ServerUrl
	t.Cleanup(func() { configuration.Get().ServerUrl = previous })
	for _, tc := range []struct {
		url    string
		secure bool
	}{
		{"http://localhost:53842/", false}, {"https://files.example.invalid/", true},
	} {
		configuration.Get().ServerUrl = tc.url
		w := httptest.NewRecorder()
		writeFilePwCookie(w, models.File{Id: "synthetic-cookie"})
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("expected one download password cookie")
		}
		c := cookies[0]
		if c.Secure != tc.secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
			t.Errorf("public URL %s: Secure=%v HttpOnly=%v SameSite=%v", tc.url, c.Secure, c.HttpOnly, c.SameSite)
		}
	}
}

func TestSEC012PasswordChangeRevokesSessions(t *testing.T) {
	user := models.User{Id: 9012, Name: "synthetic-password-change", ResetPassword: true, Password: configuration.HashPassword("synthetic-old-password", false, "")}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })
	for _, id := range []string{"sec012-current", "sec012-other-device"} {
		database.SaveSession(id, models.Session{UserId: user.Id, ValidUntil: time.Now().Add(time.Hour).Unix()})
	}
	database.SaveSession("sec012-different-owner", models.Session{UserId: user.Id + 1, ValidUntil: time.Now().Add(time.Hour).Unix()})
	t.Cleanup(func() { database.DeleteSession("sec012-different-owner") })
	invalidForm := url.Values{"newpw": {"synthetic-new-password-123"}, "csrf-token": {"invalid"}}
	invalid := httptest.NewRequest(http.MethodPost, "/changePassword", strings.NewReader(invalidForm.Encode()))
	invalid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	changePassword(httptest.NewRecorder(), authentication.SetUserInRequest(invalid, user))
	unchanged, _ := database.GetUser(user.Id)
	if unchanged.Password != user.Password {
		t.Fatal("invalid form changed password")
	}
	if _, exists := database.GetSession("sec012-current"); !exists {
		t.Fatal("invalid form revoked session")
	}
	form := url.Values{"newpw": {"synthetic-new-password-123"}, "csrf-token": {csrftoken.GenerateBound(csrftoken.TypePasswordChange, sec023LegacyTestBinding)}}
	r := httptest.NewRequest(http.MethodPost, "/changePassword", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "session_token", Value: "sec012-current"})
	r.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: sec023LegacyTestBinding})
	w := httptest.NewRecorder()
	changePassword(w, authentication.SetUserInRequest(r, user))
	got, ok := database.GetUser(user.Id)
	valid, _ := configuration.VerifyPassword("synthetic-new-password-123", got.Password, "")
	if !ok || !valid || got.ResetPassword {
		t.Fatal("password change did not succeed")
	}
	for _, id := range []string{"sec012-current", "sec012-other-device"} {
		if _, exists := database.GetSession(id); exists {
			t.Errorf("old session survives password change: %s", id)
		}
	}
	if w.Code != http.StatusTemporaryRedirect || w.Header().Get("Location") != "/login" {
		t.Error("password change must require a fresh login")
	}
	if _, exists := database.GetSession("sec012-different-owner"); !exists {
		t.Error("password change revoked another owner's session")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "session_token" || cookies[0].Value != "" {
		t.Error("current browser cookie was not cleared")
	}
}
