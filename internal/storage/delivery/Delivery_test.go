package delivery

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func deliveryScene(t *testing.T) []models.File {
	t.Helper()
	database.Connect(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "delivery.sqlite")})
	t.Cleanup(database.Close)
	database.SaveUser(models.User{Id: 1, Name: "creator", UserLevel: models.UserLevelUser}, false)
	files := []models.File{
		{Id: "first", UserId: 1, Name: "资料一.txt", Size: "3 B", SizeBytes: 3, SHA1: "shared-blob", UnlimitedTime: true, DownloadsRemaining: 3, PasswordHash: "existing-password-hash"},
		{Id: "second", UserId: 1, Name: "second.txt", Size: "3 B", SizeBytes: 3, SHA1: "shared-blob", UnlimitedTime: true, UnlimitedDownloads: true},
		{Id: "third", UserId: 1, Name: "third.txt", Size: "3 B", SizeBytes: 3, SHA1: "shared-blob", UnlimitedTime: true, UnlimitedDownloads: true},
	}
	for _, f := range files {
		database.SaveMetaData(f)
	}
	return files
}

func TestDeliveryLifecycle(t *testing.T) {
	files := deliveryScene(t)
	ctx := context.Background()
	d, err := Create(ctx, 1, Input{Title: "Three ordinary shares", Note: "Keep original rules", FileIDs: []string{"first", "second", "first", "third"}})
	if err != nil || !ValidToken(d.Token) || !reflect.DeepEqual(d.FileIDs, []string{"first", "second", "third"}) {
		t.Fatal("create/order/dedup", err)
	}
	view, err := GetPublic(ctx, d.Token)
	if err != nil || view.Token != d.Token {
		t.Fatal("public read", err)
	}
	items, err := PublicFiles(ctx, view, "https://example.test/share/", false)
	if err != nil || len(items) != 3 || !items[0].Available || !items[0].PasswordRequired || items[0].URL != "https://example.test/share/d?id=first" {
		t.Fatal("public projection", items, err)
	}
	for _, want := range files {
		got, found, err := database.ReadRecoveryFile(ctx, want.Id)
		if err != nil || !found || !reflect.DeepEqual(got, want) {
			t.Fatal("delivery changed source metadata or quota", err)
		}
	}
	// Download count and expiry are read live, not copied into a permission.
	spent := files[0]
	spent.DownloadsRemaining = 0
	database.SaveMetaData(spent)
	database.DeleteMetaData(files[1].Id)
	items, err = PublicFiles(ctx, d, "https://example.test/share/", false)
	if err != nil || items[0].Available || items[0].URL != "" || items[1].Available || items[1].Name != "" || !items[2].Available {
		t.Fatal("one unavailable share affected others", items, err)
	}
	if found, err := database.RevokeDelivery(ctx, d.Token, 2, time.Now().Unix()); err != nil || found {
		t.Fatal("other owner revoked delivery", err)
	}
	if _, err := GetPublic(ctx, d.Token); err != nil {
		t.Fatal("unauthorized revoke changed delivery", err)
	}
	if found, err := database.RevokeDelivery(ctx, d.Token, 1, time.Now().Unix()); err != nil || !found {
		t.Fatal("owner revoke", err)
	}
	if _, err := GetPublic(ctx, d.Token); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked capability remained public", err)
	}
	if _, found, err := database.ReadRecoveryFile(ctx, files[2].Id); err != nil || !found {
		t.Fatal("revoke deleted original share", err)
	}
}

func TestDeliveryRejectsUnsafeSourcesAndExpiry(t *testing.T) {
	files := deliveryScene(t)
	ctx := context.Background()
	input := Input{Title: "Delivery", FileIDs: []string{"first", "second"}}
	for _, change := range []func(*models.File){
		func(f *models.File) { f.UserId = 2 },
		func(f *models.File) { f.UploadRequestId = "private-collection" },
		func(f *models.File) { f.Encryption.IsEndToEndEncrypted = true },
		func(f *models.File) { f.UnlimitedTime = false; f.ExpireAt = 1 },
		func(f *models.File) { f.PendingDeletion = 1 },
	} {
		f := files[1]
		change(&f)
		database.SaveMetaData(f)
		if _, err := Create(ctx, 1, input); !errors.Is(err, ErrFileUnavailable) {
			t.Fatal("unsafe source accepted", err)
		}
		list, err := database.ListDeliveries(ctx, 1)
		if err != nil || len(list) != 0 {
			t.Fatal("failed create left partial record", err)
		}
	}
	database.SaveMetaData(files[1])
	if _, err := Create(ctx, 1, Input{Title: "Expired", FileIDs: input.FileIDs, ExpiresAt: 1}); !errors.Is(err, ErrInvalid) {
		t.Fatal("past expiry accepted", err)
	}
	d, err := Create(ctx, 1, input)
	if err != nil {
		t.Fatal(err)
	}
	entropy := make([]byte, 32)
	entropy[0] = 1
	d.Token = base64.RawURLEncoding.EncodeToString(entropy)
	d.ExpiresAt = 1
	if inserted, err := database.InsertDelivery(ctx, d); err != nil || !inserted {
		t.Fatal("expiry fixture", err)
	}
	if _, err := GetPublic(ctx, d.Token); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired capability remained public", err)
	}
	if _, err := GetPublic(ctx, "guessed-id"); !errors.Is(err, ErrNotFound) {
		t.Fatal("internal identifier accepted", err)
	}
}
