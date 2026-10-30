//go:build test

package redis

import (
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestCollectionPolicyLegacyAndRoundTrip(t *testing.T) {
	scene := sec030RedisNew(t)
	legacy := models.FileRequest{Id: "legacy", ApiKey: "legacy-key", UserId: 42, Name: "Existing"}
	scene.db.SaveFileRequest(legacy)
	sec030RedisDo(t, scene.conn, "HDEL", scene.config.RedisPrefix+prefixFileRequests+legacy.Id, "alloweddownloads")
	if got, ok := scene.db.GetFileRequest(legacy.Id); !ok || !reflect.DeepEqual(got, legacy) {
		t.Fatal("missing legacy field no longer means unlimited")
	}
	modern := models.FileRequest{Id: "modern", ApiKey: "modern-key", UserId: 42, Name: "New", AllowedDownloads: 10}
	scene.db.SaveFileRequest(modern)
	if got, ok := scene.db.GetFileRequest(modern.Id); !ok || !reflect.DeepEqual(got, modern) {
		t.Fatal("stored download policy did not round trip")
	}
	listed := scene.db.GetAllFileRequests()
	if len(listed) != 2 {
		t.Fatalf("collection list has %d entries, want 2", len(listed))
	}
	for _, got := range listed {
		if got.Id == modern.Id && !reflect.DeepEqual(got, modern) {
			t.Fatal("collection list lost the download policy")
		}
	}
}
