//go:build test

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test"
)

type onboardingCreateResponse struct {
	models.User
	TemporaryPassword string `json:"password"`
	LoginURL          string `json:"loginUrl"`
}

func onboardingAdminKey(t *testing.T) models.ApiKey {
	t.Helper()
	key := generateNewKey(false, idSuperAdmin, "onboarding test", "")
	key.Permissions = models.ApiPermManageUsers
	database.SaveApiKey(key)
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	return key
}

func assertCredentialHeaders(t *testing.T, headers http.Header) {
	t.Helper()
	if headers.Get("Cache-Control") != "no-store" || headers.Get("Pragma") != "no-cache" || headers.Get("Expires") != "0" {
		t.Fatal("credential response is cacheable")
	}
}

func TestUserOnboardingCredentials(t *testing.T) {
	oldMethod, oldURL := configuration.Get().Authentication.Method, configuration.Get().ServerUrl
	t.Cleanup(func() {
		configuration.Get().Authentication.Method = oldMethod
		configuration.Get().ServerUrl = oldURL
	})
	configuration.Get().Authentication.Method = models.AuthenticationInternal
	configuration.Get().ServerUrl = "https://share.example/portal/"
	key := onboardingAdminKey(t)
	w, r := getRecorder("/user/create", key.Id, []test.Header{{Name: "username", Value: "OnboardingCreator"}})
	r.Host = "untrusted.example"
	r.Header.Set("X-Forwarded-Host", "another-untrusted.example")
	Process(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create HTTP status %d", w.Code)
	}
	assertCredentialHeaders(t, w.Header())
	var response onboardingCreateResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.DeleteUser(response.Id) })
	if response.Name != "onboardingcreator" || !response.ResetPassword || len(response.TemporaryPassword) < 20 || response.LoginURL != "https://share.example/portal/login" {
		t.Fatal("creation did not deliver the canonical login URL, persisted name and first-login credentials")
	}
	stored, ok := database.GetUser(response.Id)
	if !ok || stored.Password == response.TemporaryPassword || !stored.ResetPassword {
		t.Fatal("user was not initially stored with a hash and reset requirement")
	}
	if valid, legacy := configuration.VerifyPassword(response.TemporaryPassword, stored.Password, ""); !valid || legacy {
		t.Fatal("delivered password cannot authenticate")
	}
	serialized, err := json.Marshal(database.GetAllUsers())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(serialized), response.TemporaryPassword) || strings.Contains(string(serialized), stored.Password) || strings.Contains(string(serialized), `"password"`) {
		t.Fatal("normal user listing serialization exposed a password")
	}

	// Duplicate and invalid requests must not disclose credentials or rewrite an account.
	for _, name := range []string{"ONBOARDINGCREATOR", "x"} {
		failed, request := getRecorder("/user/create", key.Id, []test.Header{{Name: "username", Value: name}})
		Process(failed, request)
		if failed.Code == http.StatusOK || strings.Contains(failed.Body.String(), `"password"`) || strings.Contains(failed.Body.String(), `"loginUrl"`) {
			t.Fatal("failed creation disclosed credentials")
		}
	}
	unchanged, _ := database.GetUser(response.Id)
	if unchanged.Password != stored.Password {
		t.Fatal("duplicate creation overwrote the user's password")
	}

	// A new temporary password revokes old sessions and uses the same delivery fields.
	const sessionID = "onboarding-old-session"
	database.SaveSession(sessionID, models.Session{UserId: response.Id, RenewAt: time.Now().Add(time.Minute).Unix(), ValidUntil: time.Now().Add(time.Hour).Unix()})
	w, r = getRecorder("/user/resetPassword", key.Id, []test.Header{
		{Name: "userid", Value: strconv.Itoa(response.Id)}, {Name: "generateNewPassword", Value: "true"},
	})
	Process(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("reset HTTP status %d", w.Code)
	}
	assertCredentialHeaders(t, w.Header())
	var reset struct {
		Result   string `json:"Result"`
		Password string `json:"password"`
		Username string `json:"username"`
		LoginURL string `json:"loginUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reset); err != nil {
		t.Fatal(err)
	}
	if reset.Result != "OK" || reset.Username != stored.Name || reset.LoginURL != response.LoginURL || len(reset.Password) < 20 || reset.Password == response.TemporaryPassword {
		t.Fatal("reset did not deliver a fresh password with full login information")
	}
	stored, _ = database.GetUser(response.Id)
	if valid, _ := configuration.VerifyPassword(reset.Password, stored.Password, ""); !valid {
		t.Fatal("reset password does not authenticate")
	}
	if valid, _ := configuration.VerifyPassword(response.TemporaryPassword, stored.Password, ""); valid {
		t.Fatal("previous temporary password still authenticates")
	}
	if _, exists := database.GetSession(sessionID); exists {
		t.Fatal("password regeneration retained an old session")
	}
	if !stored.ResetPassword {
		t.Fatal("regenerated password must still require a first-login change")
	}
}

func TestUserOnboardingUnauthorizedDoesNotCreateOrReset(t *testing.T) {
	limited := generateNewKey(false, idUser, "onboarding unauthorized test", "")
	limited.Permissions = models.ApiPermNone
	database.SaveApiKey(limited)
	t.Cleanup(func() { database.DeleteApiKey(limited.Id) })
	for _, key := range []string{"", "invalid", limited.Id} {
		w, r := getRecorder("/user/create", key, []test.Header{{Name: "username", Value: "no-credentials-for-unauthorized"}})
		Process(w, r)
		if w.Code == http.StatusOK || strings.Contains(w.Body.String(), `"password"`) || strings.Contains(w.Body.String(), `"loginUrl"`) {
			t.Fatal("unauthorized creation returned credentials")
		}
	}
	if _, exists := database.GetUserByName("no-credentials-for-unauthorized"); exists {
		t.Fatal("unauthorized request created an account")
	}
	before, _ := database.GetUser(idAdmin)
	w, r := getRecorder("/user/resetPassword", "", []test.Header{
		{Name: "userid", Value: strconv.Itoa(idAdmin)}, {Name: "generateNewPassword", Value: "true"},
	})
	Process(w, r)
	after, _ := database.GetUser(idAdmin)
	if w.Code == http.StatusOK || strings.Contains(w.Body.String(), `"password"`) || before.Password != after.Password || before.ResetPassword != after.ResetPassword {
		t.Fatal("unauthorized reset changed or disclosed credentials")
	}
}

func TestUserOnboardingExternalAuthenticationUnchanged(t *testing.T) {
	oldMethod := configuration.Get().Authentication.Method
	t.Cleanup(func() { configuration.Get().Authentication.Method = oldMethod })
	key := onboardingAdminKey(t)
	for _, method := range []int{models.AuthenticationOAuth2, models.AuthenticationHeader, models.AuthenticationDisabled} {
		configuration.Get().Authentication.Method = method
		name := "external-onboarding-" + strconv.Itoa(method)
		w, r := getRecorder("/user/create", key.Id, []test.Header{{Name: "username", Value: name}})
		Process(w, r)
		var response onboardingCreateResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || response.ResetPassword || response.TemporaryPassword != "" || response.LoginURL != "" || strings.Contains(w.Body.String(), `"password"`) {
			t.Fatal("external-auth creation was incorrectly assigned internal credentials")
		}
		stored, ok := database.GetUser(response.Id)
		if !ok || stored.Password != "" {
			t.Fatal("external-auth user must remain passwordless")
		}
		database.DeleteUser(response.Id)
	}
}
