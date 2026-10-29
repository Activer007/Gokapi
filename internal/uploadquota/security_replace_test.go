//go:build test && security_replace

package uploadquota

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
)

func TestSEC030ReplaceGrowthAndShrink(t *testing.T) {
	target := models.File{Id: "target", UserId: 1, SizeBytes: 1, SHA1: "old", UnlimitedTime: true, UnlimitedDownloads: true}
	source := models.File{Id: "source", UserId: 2, SizeBytes: 2, SHA1: "new", UploadRequestId: "original-request", UnlimitedTime: true, UnlimitedDownloads: true}
	files := map[string]models.File{target.Id: target, source.Id: source, "used": {Id: "used", UserId: 1, SizeBytes: 2}}
	ledger := EmptyLedger()
	ledger.Records[target.Id] = Reservation{ID: target.Id, UserID: 1, State: Reserved, BytesKnown: true,
		Bytes: 1, StagingBytes: 4, CreatedAt: 1000, ExpiresAt: 4600}
	before, err := Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	change := Change{Action: ReplaceMetadata, Target: &target, Source: &source, Now: 1000,
		Upload: Reservation{ID: target.Id, UserID: target.UserId}, Limits: Limits{User: Usage{Bytes: 5}, Instance: Usage{Bytes: 7}}}
	next, record, metadata, err := Apply(ledger, files, change)
	if err != nil || record != (Reservation{}) || metadata != nil || !reflect.DeepEqual(next, ledger) {
		t.Fatalf("exact growth rejected or created state: %v", err)
	}
	change.Limits.User.Bytes = 4
	if _, _, _, err := Apply(ledger, files, change); !errors.Is(err, ErrQuota) {
		t.Fatal("target owner growth/hold was not charged", err)
	}
	change.Limits.User.Bytes, change.Limits.Instance.Bytes, change.DeleteSource = 5, 6, true
	if _, _, _, err := Apply(ledger, files, change); !errors.Is(err, ErrQuota) {
		t.Fatal("instance growth was not charged or source deletion was credited early", err)
	}
	change.DeleteSource, change.Limits = false, Limits{}
	source.SizeBytes = 1
	files[source.Id] = source
	if _, _, _, err := Apply(ledger, files, change); err != nil {
		t.Fatal("equal-sized content required new capacity", err)
	}
	source.SizeBytes = 0
	files[source.Id] = source
	if _, _, _, err := Apply(ledger, files, change); err != nil {
		t.Fatal("shrinking an existing over-limit file was blocked", err)
	}
	changed := target
	changed.UserId = 3
	files[target.Id] = changed
	if _, _, _, err := Apply(ledger, files, change); !errors.Is(err, ErrConflict) {
		t.Fatal("stale target owner accepted", err)
	}
	after, err := Encode(ledger)
	if err != nil || string(before) != string(after) {
		t.Fatal("replacement admission mutated the ledger")
	}
}
