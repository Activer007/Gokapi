//go:build test

package sqlite

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestCollectionPolicyMigrationAndRoundTrip(t *testing.T) {
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "collection.sqlite")}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	legacy := models.FileRequest{Id: "legacy", ApiKey: "legacy-key", UserId: 42, Name: "Existing", MaxFiles: 4, Expiry: 12345}
	p.SaveFileRequest(legacy)
	// Recreate an actual schema-17 source, including a missing policy column.
	if _, err := p.sqliteDb.Exec("ALTER TABLE UploadRequests DROP COLUMN allowedDownloads"); err != nil {
		t.Fatal(err)
	}
	p.SetDbVersion(17)
	if got, ok := p.GetFileRequest(legacy.Id); !ok || !reflect.DeepEqual(got, legacy) {
		t.Fatal("old source cannot be read without changing its schema")
	}
	if got := p.GetAllFileRequests(); len(got) != 1 || !reflect.DeepEqual(got[0], legacy) || p.GetDbVersion() != 17 {
		t.Fatal("offline legacy reader changed data or schema version")
	}
	p.Upgrade(17)
	p.SetDbVersion(p.GetSchemaVersion())
	p.Upgrade(17) // Startup retries must preserve an already-added column.
	if got, ok := p.GetFileRequest(legacy.Id); !ok || !reflect.DeepEqual(got, legacy) {
		t.Fatal("migration changed existing collection semantics")
	}
	modern := models.FileRequest{Id: "modern", ApiKey: "modern-key", UserId: 42, Name: "New", AllowedDownloads: 10}
	p.SaveFileRequest(modern)
	p.Close()
	p, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := p.GetFileRequest(modern.Id); !ok || !reflect.DeepEqual(got, modern) {
		t.Fatal("download policy did not survive reopening")
	}
	for _, got := range p.GetAllFileRequests() {
		if got.Id == modern.Id && !reflect.DeepEqual(got, modern) {
			t.Fatal("collection list lost the download policy")
		}
	}
}
