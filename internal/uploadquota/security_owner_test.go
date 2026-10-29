//go:build test && security_owner

package uploadquota

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
)

func TestSEC030OwnerCapacityAndNoop(t *testing.T) {
	source := models.File{Id: "source", UserId: 1, SizeBytes: 3, UploadRequestId: "request"}
	files := map[string]models.File{source.Id: source,
		"target-used": {Id: "target-used", UserId: 2, SizeBytes: 2},
		"over-source": {Id: "over-source", UserId: 1, SizeBytes: 20}}
	ledger := EmptyLedger()
	// Even a reservation colliding with the file ID must count in full.
	ledger.Records[source.Id] = Reservation{ID: source.Id, UserID: 2, State: Reserved, BytesKnown: true,
		Bytes: 3, StagingBytes: 6, CreatedAt: 1000, ExpiresAt: 4600}
	before, err := Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	initialFiles := make(map[string]models.File)
	for id, file := range files {
		initialFiles[id] = file
	}
	// Other scopes are already over their literal zero caps. Transfer creates
	// no instance/request/active/staging usage and must not charge them again.
	change := Change{Action: TransferOwner, Source: &source, Now: 1000,
		Upload: Reservation{ID: source.Id, UserID: 2}, Limits: Limits{User: Usage{Bytes: 8, Files: 3}}}
	next, record, metadata, err := Apply(ledger, files, change)
	if err != nil || record != (Reservation{}) || metadata != nil || !reflect.DeepEqual(next, ledger) {
		t.Fatalf("exact destination delta rejected or created state: %v", err)
	}
	change.Limits.User.Bytes--
	_, _, metadata, err = Apply(ledger, files, change)
	if !errors.Is(err, ErrQuota) || metadata != nil {
		t.Fatalf("receiving owner not charged with its active reservation: %v", err)
	}
	change.Upload.UserID, change.Limits.User = source.UserId, Usage{}
	if _, _, _, err := Apply(ledger, files, change); err != nil {
		t.Fatal("same-owner retry rejected because existing usage exceeds capacity", err)
	}
	changed := files[source.Id]
	changed.UserId = 3
	files[source.Id] = changed
	if _, _, _, err := Apply(ledger, files, change); !errors.Is(err, ErrConflict) {
		t.Fatal("stale source owner was accepted", err)
	}
	files[source.Id] = source
	after, err := Encode(ledger)
	if err != nil || string(before) != string(after) || !reflect.DeepEqual(initialFiles, files) {
		t.Fatal("owner admission mutated its inputs")
	}
}
