//go:build test

package securityfixture_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/Activer007/Gokapi/internal/configuration"
	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage"
	"github.com/Activer007/Gokapi/internal/test/securityfixture"
	"github.com/Activer007/Gokapi/internal/test/testconfiguration"
	"github.com/Activer007/Gokapi/internal/webserver/api"
	"github.com/Activer007/Gokapi/internal/webserver/ratelimiter"
	"golang.org/x/oauth2"
)

type globalStore struct{}

func (globalStore) SaveUser(v models.User, fresh bool)      { database.SaveUser(v, fresh) }
func (globalStore) SaveApiKey(v models.ApiKey)              { database.SaveApiKey(v) }
func (globalStore) SaveSession(id string, v models.Session) { database.SaveSession(id, v) }
func (globalStore) SaveMetaData(v models.File)              { database.SaveMetaData(v) }
func (globalStore) SaveFileRequest(v models.FileRequest)    { database.SaveFileRequest(v) }

// This is a starter matrix through the real API authorization path, not a
// replacement for future route/action, browser, or concurrent deletion tests.
func TestM0ActorBoundaries(t *testing.T) {
	t.Chdir(t.TempDir()) // Existing upstream helper uses relative "test/" paths.
	t.Setenv("GOKAPI_CONFIG_DIR", "test")
	t.Setenv("GOKAPI_DATA_DIR", "test/data")
	testconfiguration.Create(false)
	configuration.Load()
	ratelimiter.SetUnitTestMode(true)
	t.Cleanup(func() { ratelimiter.SetUnitTestMode(false) })
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := securityfixture.New(t, configuration.HashPassword("synthetic-password", false, ""))
			cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "fixture.sqlite"), Type: 0}
			if backend == "redis" {
				cfg = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "m0_"}
			}
			database.Connect(cfg)
			t.Cleanup(database.Close)
			s.Seed(globalStore{})
			// Match ordinary-key permissions so the route's credential-type check,
			// rather than a missing permission, must reject management access.
			requestKey := s.Keys["request"]
			requestKey.Permissions = s.Keys["owner"].Permissions
			database.SaveApiKey(requestKey)
			oldDir := configuration.Get().DataDir
			configuration.Get().DataDir = s.DataDir
			t.Cleanup(func() { configuration.Get().DataDir = oldDir })
			for _, tc := range []struct {
				name, key, file string
				want            int
			}{
				{"anonymous", "", "own", 401},
				{"owner", "owner", "own", 200},
				{"admin", "admin", "other", 200},
				{"cross-owner", "owner", "other", 401},
				{"shared-blob-is-not-authority", "owner", "dedup", 401},
				{"temporary", "temporary", "own", 200},
				{"expired-key", "expired", "own", 401},
				{"request-key-on-management", "request", "own", 401},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := httptest.NewRequest(http.MethodGet, "/api/files/list/"+s.Files[tc.file].Id, nil)
					r.Header.Set("apikey", s.Keys[tc.key].Id)
					w := httptest.NewRecorder()
					api.Process(w, r)
					if w.Code != tc.want {
						t.Fatalf("status = %d, want %d", w.Code, tc.want)
					}
				})
			}
			t.Run("ordinary-key-on-request-route", func(t *testing.T) {
				r := httptest.NewRequest(http.MethodPost, "/api/uploadrequest/chunk/reserve", nil)
				r.Header.Set("apikey", s.Keys["owner"].Id)
				// Omit parameters: credential type must be rejected before parsing.
				// Without the type gate this becomes a 400, not the required 401.
				w := httptest.NewRecorder()
				api.Process(w, r)
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("ordinary key entered request route: status %d", w.Code)
				}
			})
			for id, want := range s.Sessions {
				got, ok := database.GetSession(id)
				if !ok || got != want {
					t.Fatal("session did not round-trip")
				}
			}
			file, ok := database.GetMetaDataById(s.Files["protected"].Id)
			valid, _ := configuration.VerifyPassword("synthetic-password", file.PasswordHash, "")
			if !ok || !valid {
				t.Fatal("protected fixture lost its password hash")
			}
			if !storage.IsExpiredFile(s.Files["expired"], s.Files["expired"].ExpireAt+1) {
				t.Fatal("expired fixture accepted")
			}
			database.DeleteApiKey(s.Keys["owner"].Id)
			r := httptest.NewRequest(http.MethodGet, "/api/files/list", nil)
			r.Header.Set("apikey", s.Keys["owner"].Id)
			w := httptest.NewRecorder()
			api.Process(w, r)
			if w.Code != 401 {
				t.Fatal("revoked key accepted")
			}
		})
	}
}

// Verify the mock's signed tokens with the same upstream OIDC library. This
// proves fixture quality; it does NOT claim Gokapi verifies ID tokens or binds sub.
func TestM0IDPProtocol(t *testing.T) {
	ctx := context.Background()
	var first *oidc.Provider
	for i, subject := range []string{"subject-a", "subject-b"} {
		server := securityfixture.NewIDP(t, securityfixture.Identity{Subject: subject, Email: "person@example.invalid", EmailVerified: i == 0})
		provider, err := oidc.NewProvider(ctx, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		client := oauth2.Config{ClientID: "fixture-client", Endpoint: provider.Endpoint()}
		token, err := client.Exchange(ctx, "synthetic-code")
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := token.Extra("id_token").(string)
		if !ok {
			t.Fatal("missing ID token")
		}
		verified, err := provider.Verifier(&oidc.Config{ClientID: "fixture-client"}).Verify(ctx, raw)
		if err != nil || verified.Subject != subject {
			t.Fatal("fixture token could not be verified")
		}
		info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil || info.Subject != subject || info.EmailVerified != (i == 0) {
			t.Fatal("fixture claims did not round-trip")
		}
		if i == 0 {
			first = provider
		} else if _, err := first.Verifier(&oidc.Config{ClientID: "fixture-client"}).Verify(ctx, raw); err == nil {
			t.Fatal("different issuer accepted by verifier")
		}
	}
}
