//go:build test && !integration

package webserver

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/delivery"
)

func TestDeliveryPublicPageAndOriginalPassword(t *testing.T) {
	owner := models.User{Id: 7715, Name: "delivery page creator", UserLevel: models.UserLevelUser}
	database.SaveUser(owner, false)
	t.Cleanup(func() { database.DeleteUser(owner.Id) })
	ids := []string{"delivery-" + rand.Text(), "delivery-" + rand.Text(), "delivery-" + rand.Text()}
	blobID := "private-blob-" + rand.Text()
	blobPath := filepath.Join(configuration.Get().DataDir, blobID)
	if err := os.WriteFile(blobPath, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blobPath) })
	for i, id := range ids {
		file := models.File{Id: id, UserId: owner.Id, Name: "资料<script>filename.txt", Size: "3 B", SizeBytes: 3, SHA1: blobID, UnlimitedTime: true, DownloadsRemaining: 3}
		if i == 0 {
			file.PasswordHash = configuration.HashPassword("original-password", false, "")
		}
		database.SaveMetaData(file)
		t.Cleanup(func() { database.DeleteMetaData(id) })
	}
	r := httptest.NewRequest(http.MethodGet, "/delivery/new", nil)
	d, err := delivery.Create(r.Context(), owner.Id, delivery.Input{Title: "Project <unsafe>", Note: "Delivery note", FileIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	r.SetPathValue("token", d.Token)
	w := httptest.NewRecorder()
	showDelivery(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Project &lt;unsafe&gt;") {
		t.Fatal("public page missing escaped title", w.Code)
	}
	for _, id := range ids {
		if !strings.Contains(w.Body.String(), "d?id="+id) {
			t.Fatal("public page missing original download link")
		}
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("public capability response can be cached or referred")
	}
	if !strings.Contains(w.Body.String(), `<base href="/`) || strings.Contains(w.Body.String(), blobID) || strings.Contains(w.Body.String(), owner.Name) || strings.Contains(w.Body.String(), "original-password") || strings.Contains(w.Body.String(), `<script>filename`) {
		t.Fatal("public page leaked internal data or unescaped text")
	}
	pw := httptest.NewRecorder()
	showDownload(pw, httptest.NewRequest(http.MethodGet, "/d?id="+ids[0], nil))
	if pw.Code != http.StatusOK || !strings.Contains(pw.Body.String(), `id="passwordFile"`) {
		t.Fatal("delivery link bypassed original password challenge")
	}
	if found, err := database.RevokeDelivery(r.Context(), d.Token, owner.Id, time.Now().Unix()); err != nil || !found {
		t.Fatal(err)
	}
	revoked := httptest.NewRecorder()
	showDelivery(revoked, r)
	if revoked.Code != http.StatusNotFound || strings.Contains(revoked.Body.String(), ids[0]) || strings.Contains(revoked.Body.String(), d.Token) {
		t.Fatal("revoked page exposed file entries or token", revoked.Code)
	}
}
