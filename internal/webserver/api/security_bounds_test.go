//go:build test

package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
)

func TestSEC014CancelledAPIKeyDoesNotAuthorize(t *testing.T) {
	key := models.ApiKey{Id: "synthetic-sec014-key", UserId: idUser, Permissions: models.ApiPermView}
	database.SaveApiKey(key)
	t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	r := httptest.NewRequest("GET", "/api/files/list", nil)
	r.Header.Set("apikey", key.Id)
	route := apiRoute{ApiPerm: models.ApiPermView}
	if _, _, ok := isAuthorisedForApi(r, route); !ok {
		t.Fatal("control API key was rejected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, ok := isAuthorisedForApi(r.WithContext(ctx), route); ok {
		t.Fatal("canceled request was authorized")
	}
}
