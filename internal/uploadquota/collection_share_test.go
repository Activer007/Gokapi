package uploadquota

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestCollectionShareQuota(t *testing.T) {
	source := models.File{Id: "collection", UserId: 1, UploadRequestId: "request", SHA1: "shared-blob", SizeBytes: 2,
		Encryption: models.EncryptionInfo{IsEncrypted: true, DecryptionKey: []byte("wrapped-key"), Nonce: []byte("nonce")}}
	share := source
	share.Id, share.UploadRequestId = "share", ""
	files := map[string]models.File{source.Id: source}
	change := Change{Action: DuplicateMetadata, Source: &source, File: &share, DetachCollection: true, Now: 1000,
		Upload: Reservation{ID: share.Id, UserID: 1, BytesKnown: true, Bytes: 2},
		Limits: Limits{User: Usage{Bytes: 4, Files: 2}, Instance: Usage{Bytes: 4, Files: 2}}}
	_, _, metadata, err := Apply(EmptyLedger(), files, change)
	if err != nil || metadata == nil || !reflect.DeepEqual(*metadata, share) || len(files) != 1 {
		t.Fatalf("ordinary share must preserve encryption and source: metadata=%v err=%v", metadata, err)
	}
	// The original request is already full (zero spare capacity above), but the
	// new ordinary share must still consume both enclosing logical quotas.
	for _, scope := range []string{"user", "instance"} {
		denied := change
		if scope == "user" {
			denied.Limits.User.Bytes = 3
		} else {
			denied.Limits.Instance.Bytes = 3
		}
		_, _, metadata, err = Apply(EmptyLedger(), files, denied)
		if !errors.Is(err, ErrQuota) || metadata != nil {
			t.Fatalf("%s quota bypassed: %v", scope, err)
		}
	}
	change.DetachCollection = false
	if _, _, metadata, err = Apply(EmptyLedger(), files, change); !errors.Is(err, ErrConflict) || metadata != nil {
		t.Fatal("ordinary duplicate silently detached a collection")
	}
	change.DetachCollection = true
	share.UploadRequestId, change.Upload.RequestID = "another-request", "another-request"
	if _, _, metadata, err = Apply(EmptyLedger(), files, change); !errors.Is(err, ErrConflict) || metadata != nil {
		t.Fatal("conversion moved a file into another collection")
	}
}
