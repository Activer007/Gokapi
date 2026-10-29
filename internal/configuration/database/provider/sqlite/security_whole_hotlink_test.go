//go:build test

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func sec030SQLiteWholeHotlinkCommit(t *testing.T, db DatabaseProvider) uploadquota.Change {
	t.Helper()
	change := sqliteQuotaReserve()
	change.Upload.RequestID = ""
	for _, action := range []uploadquota.Action{uploadquota.Reserve, uploadquota.Claim} {
		change.Action, change.FileID = action, "whole-hotlink-file"
		if _, err := db.ApplyUploadQuota(context.Background(), change); err != nil {
			t.Fatal(err)
		}
	}
	file := models.File{Id: change.FileID, UserId: change.Upload.UserID,
		SizeBytes: change.Upload.Bytes, SHA1: "synthetic-blob", HotlinkId: "whole-hotlink"}
	change.Action, change.File = uploadquota.Commit, &file
	return change
}

func TestSEC030WholeHotlinkSQLiteCommitAndReplay(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new-mapping"
		if existing {
			name = "existing-same-target"
		}
		t.Run(name, func(t *testing.T) {
			db := newQuotaDatabase(t)
			commit := sec030SQLiteWholeHotlinkCommit(t, db)
			if existing {
				db.SaveHotlink(*commit.File)
			}
			if _, err := db.ApplyUploadQuota(context.Background(), commit); err != nil {
				t.Fatal(err)
			}
			if target, ok := db.GetHotlink(commit.File.HotlinkId); !ok || target != commit.File.Id {
				t.Fatal("committed metadata has no matching hotlink mapping")
			}
			before, err := db.ReadUploadQuota(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			files := db.GetAllMetadata()
			db.DeleteHotlink(commit.File.HotlinkId)
			if _, err := db.ApplyUploadQuota(context.Background(), commit); err != nil {
				t.Fatal(err)
			}
			if _, ok := db.GetHotlink(commit.File.HotlinkId); ok {
				t.Fatal("committed replay resurrected a subsequently deleted hotlink")
			}
			after, err := db.ReadUploadQuota(context.Background())
			if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(files, db.GetAllMetadata()) {
				t.Fatal("committed replay changed ledger or metadata", err)
			}
		})
	}
}

func TestSEC030WholeHotlinkSQLiteCollisionAtomic(t *testing.T) {
	db := newQuotaDatabase(t)
	commit := sec030SQLiteWholeHotlinkCommit(t, db)
	db.SaveHotlink(models.File{Id: "protected-other-file", HotlinkId: commit.File.HotlinkId})
	before, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := db.GetAllMetadata()
	if _, err := db.ApplyUploadQuota(context.Background(), commit); !errors.Is(err, uploadquota.ErrConflict) {
		t.Errorf("hotlink collision error=%v; want reservation conflict", err)
	}
	after, err := db.ReadUploadQuota(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(files, db.GetAllMetadata()) {
		t.Fatal("rejected hotlink collision partially committed metadata or ledger", err)
	}
	if target, ok := db.GetHotlink(commit.File.HotlinkId); !ok || target != "protected-other-file" {
		t.Fatal("hotlink collision overwrote the protected mapping")
	}
}

func TestSEC030WholeHotlinkSQLiteWriteFailureAtomic(t *testing.T) {
	db := newQuotaDatabase(t)
	commit := sec030SQLiteWholeHotlinkCommit(t, db)
	before, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := db.GetAllMetadata()
	if _, err := db.sqliteDb.Exec("DROP TABLE Hotlinks"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyUploadQuota(context.Background(), commit); err == nil {
		t.Error("commit ignored unavailable hotlink storage")
	}
	after, err := db.ReadUploadQuota(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(files, db.GetAllMetadata()) {
		t.Fatal("hotlink write failure partially committed metadata or ledger", err)
	}
}

func TestSEC030WholeHotlinkSQLiteMetadataFailureRollsBackMap(t *testing.T) {
	db := newQuotaDatabase(t)
	commit := sec030SQLiteWholeHotlinkCommit(t, db)
	before, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files := db.GetAllMetadata()
	if _, err := db.sqliteDb.Exec(`CREATE TRIGGER reject_whole_metadata BEFORE INSERT ON FileMetaData
		BEGIN SELECT RAISE(ABORT, 'synthetic metadata write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyUploadQuota(context.Background(), commit); err == nil {
		t.Error("commit ignored a real metadata write failure")
	}
	after, err := db.ReadUploadQuota(context.Background())
	if err != nil || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(files, db.GetAllMetadata()) {
		t.Fatal("metadata write failure partially committed metadata or ledger", err)
	}
	if _, ok := db.GetHotlink(commit.File.HotlinkId); ok {
		t.Fatal("metadata write failure retained an uncommitted hotlink mapping")
	}
}
