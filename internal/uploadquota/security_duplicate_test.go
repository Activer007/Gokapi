//go:build test && security_duplicate

package uploadquota

import (
	"errors"
	"reflect"
	"testing"

	"github.com/forceu/gokapi/internal/models"
)

func TestSEC030DuplicateLogicalOnly(t *testing.T) {
	source := models.File{Id: "source", UserId: 1, SizeBytes: 2, SHA1: "shared", UnlimitedTime: true, UnlimitedDownloads: true}
	file := source
	file.Id = "copy"
	files := map[string]models.File{source.Id: source}
	limits := durableLimits()
	upload := Reservation{ID: "copy", UserID: 1, BytesKnown: true, Bytes: 3, StagingBytes: 6, ExpiresAt: 1100}
	ledger, _, _, err := Apply(EmptyLedger(), files, Change{Action: Reserve, Upload: upload, Limits: limits, Now: 1000})
	if err != nil {
		t.Fatal(err)
	}
	before, err := Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	// An existing reservation deliberately shares the candidate's ID. It must
	// still count, and a metadata-only operation must need no active/staging cap.
	limits.User = Usage{Bytes: 7, Files: 3}
	limits.UserActive, limits.InstanceActive, limits.StagingBytes = 0, 0, 0
	change := Change{Action: DuplicateMetadata, Source: &source, File: &file, Now: 1000,
		Upload: Reservation{ID: file.Id, UserID: file.UserId, BytesKnown: true, Bytes: file.SizeBytes}, Limits: limits}
	next, receipt, metadata, err := Apply(ledger, files, change)
	if err != nil || receipt != (Reservation{}) || metadata == nil || !reflect.DeepEqual(*metadata, file) {
		t.Fatalf("logical-only copy: receipt=%+v metadata=%v err=%v", receipt, metadata, err)
	}
	after, err := Encode(next)
	if err != nil || string(after) != string(before) || len(files) != 1 {
		t.Fatal("copy mutated the ledger or input metadata")
	}
	// Used 2 + reserved 3 + requested 2 exceeds six: the colliding reservation
	// cannot be skipped by the shared upload-capacity evaluator.
	change.Limits.User.Bytes = 6
	_, _, metadata, err = Apply(ledger, files, change)
	if !errors.Is(err, ErrQuota) || metadata != nil {
		t.Fatalf("outstanding reservation not charged: metadata=%v err=%v", metadata, err)
	}
	files[file.Id] = file
	change.Limits.User.Bytes = 100
	_, _, metadata, err = Apply(ledger, files, change)
	if !errors.Is(err, ErrConflict) || metadata != nil {
		t.Fatalf("target collision accepted: metadata=%v err=%v", metadata, err)
	}
}

func TestSEC030DuplicateManagementRequestOwnership(t *testing.T) {
	// Ownership transfers and cross-owner management copies retain the original
	// request charge. Its parent need not own the current authorized source.
	source := models.File{Id: "copy", UserId: 2, UploadRequestId: "request"}
	change := Change{Action: DuplicateMetadata, Source: &source, Now: 1000, CheckRequestPolicy: true,
		Upload: Reservation{ID: "another-copy", UserID: 2, RequestID: "request", BytesKnown: true, Bytes: 3}, Limits: durableLimits()}
	policy := RequestPolicy{UserID: 1, MaxFiles: 4, MaxSizeMB: 1, Expiry: 999}
	got, err := ApplyRequestPolicy(EmptyLedger(), change, policy)
	if err != nil || got.Limits.Request.Files != policy.MaxFiles {
		t.Fatalf("management copy lost the retained request charge: limit=%d err=%v", got.Limits.Request.Files, err)
	}
}
