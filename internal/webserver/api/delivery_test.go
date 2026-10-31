//go:build test

package api

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func deliveryAPIRequest(t *testing.T, path, method, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "/api/deliveries/"+path, bytes.NewReader(encoded))
	r.Header.Set("apikey", key)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	Process(w, r)
	return w
}

func TestDeliveryAPIOwnershipAndMethods(t *testing.T) {
	key := generateNewKey(true, idUser, "delivery fixture", "")
	other := generateNewKey(true, idAdmin, "delivery other", "")
	viewOnly := generateNewKey(false, idUser, "view-only delivery", "")
	viewOnly.Permissions = models.ApiPermView
	database.SaveApiKey(viewOnly)
	t.Cleanup(func() {
		database.DeleteApiKey(key.Id)
		database.DeleteApiKey(other.Id)
		database.DeleteApiKey(viewOnly.Id)
	})
	ids := []string{"m7-" + rand.Text(), "m7-" + rand.Text(), "m7-" + rand.Text()}
	for _, id := range ids {
		database.SaveMetaData(models.File{Id: id, Name: "ordinary.txt", UserId: idUser, SHA1: "existing-blob", Size: "1 B", SizeBytes: 1, UnlimitedDownloads: true, UnlimitedTime: true})
		t.Cleanup(func() { database.DeleteMetaData(id) })
	}
	input := map[string]any{"title": "Delivery", "note": "Only existing shares", "fileIds": ids, "expiresAt": 0}
	for _, candidate := range []struct {
		key, method string
		status      int
	}{
		{"", http.MethodPost, http.StatusUnauthorized},
		{viewOnly.Id, http.MethodPost, http.StatusUnauthorized},
		{key.Id, http.MethodGet, http.StatusBadRequest},
		{other.Id, http.MethodPost, http.StatusBadRequest},
	} {
		w := deliveryAPIRequest(t, "create", candidate.method, candidate.key, input)
		if w.Code != candidate.status {
			t.Fatalf("unauthorized/method boundary status=%d want=%d", w.Code, candidate.status)
		}
	}
	w := deliveryAPIRequest(t, "create", http.MethodPost, key.Id, input)
	var d deliveryOutput
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &d) != nil || len(d.FileIDs) != 3 || d.URL == "" {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "ownerId") || strings.Contains(w.Body.String(), "existing-blob") {
		t.Fatal("management output disclosed internal metadata")
	}
	otherList := deliveryAPIRequest(t, "list", http.MethodGet, other.Id, nil)
	if otherList.Code != http.StatusOK || strings.Contains(otherList.Body.String(), d.Token) {
		t.Fatal("other user's list exposed delivery")
	}
	ownerList := deliveryAPIRequest(t, "list", http.MethodGet, key.Id, nil)
	if ownerList.Code != http.StatusOK || !strings.Contains(ownerList.Body.String(), d.Token) {
		t.Fatal("creator cannot retrieve delivery")
	}
	if got := deliveryAPIRequest(t, "revoke", http.MethodPost, other.Id, map[string]string{"token": d.Token}); got.Code != http.StatusNotFound {
		t.Fatal("other user revoked delivery", got.Code)
	}
	if got := deliveryAPIRequest(t, "revoke", http.MethodGet, key.Id, map[string]string{"token": d.Token}); got.Code != http.StatusBadRequest {
		t.Fatal("GET revoked delivery", got.Code)
	}
	if got := deliveryAPIRequest(t, "revoke", http.MethodPost, key.Id, map[string]string{"token": d.Token}); got.Code != http.StatusOK {
		t.Fatal("creator revoke failed", got.Code)
	}
}

func TestDeliveryJSONBodyBound(t *testing.T) {
	for _, body := range []string{
		`{"title":"x","unknown":"ignored"}`,
		`{"title":"x"} {}`,
		`{"title":"x"}` + strings.Repeat(" ", 16<<10),
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/deliveries/create", strings.NewReader(body))
		if err := (&paramDeliveryCreate{}).ParseRequest(r); err == nil {
			t.Fatal("invalid or oversized JSON accepted")
		}
	}
}
