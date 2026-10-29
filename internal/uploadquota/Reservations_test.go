package uploadquota

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
)

func durableLimits() Limits {
	return Limits{User: Usage{Bytes: 100, Files: 10}, Request: Usage{Bytes: 100, Files: 10}, Instance: Usage{Bytes: 1000, Files: 100}, UserActive: 10, RequestActive: 10, InstanceActive: 100, StagingBytes: 1000}
}

func TestSEC030LedgerLifecycle(t *testing.T) {
	ledger := EmptyLedger()
	upload := Reservation{ID: "upload-1", UserID: 1, RequestID: "request", ExpiresAt: 4600}
	apply := func(change Change) (Reservation, *models.File) {
		t.Helper()
		change.Now = 1000
		change.Upload = upload
		change.Limits = durableLimits()
		next, result, metadata, err := Apply(ledger, nil, change)
		if err != nil {
			t.Fatal(err)
		}
		ledger = next
		return result, metadata
	}
	r, _ := apply(Change{Action: Reserve})
	if r.State != Reserved || r.BytesKnown {
		t.Fatal("unbound reservation was not retained")
	}
	upload.BytesKnown, upload.Bytes, upload.StagingBytes = true, 3, 6
	r, _ = apply(Change{Action: Bind})
	if r.State != Uploading || r.Bytes != 3 {
		t.Fatal("size binding failed")
	}
	r, _ = apply(Change{Action: Claim, FileID: "file-1"})
	if !r.Completing || r.FileID != "file-1" {
		t.Fatal("completion not claimed")
	}
	_, _, _, err := Apply(ledger, nil, Change{Action: Claim, Upload: upload, Now: 1000, FileID: "file-1"})
	if !errors.Is(err, ErrInProgress) {
		t.Fatalf("duplicate claim=%v", err)
	}
	file := models.File{Id: "file-1", UserId: 1, UploadRequestId: "request", SizeBytes: 3, SHA1: "synthetic-shared-hash"}
	r, pending := apply(Change{Action: Commit, File: &file})
	if r.State != Committed || pending == nil || !reflect.DeepEqual(*pending, file) || r.StagingReleased {
		t.Fatal("commit did not retain staging and return metadata")
	}
	r, pending = apply(Change{Action: Commit, File: &file})
	if pending != nil || r.State != Committed {
		t.Fatal("replay attempted another metadata write")
	}
	apply(Change{Action: ReleaseStaging, CleanupConfirmed: true})
	if ledger.HasOutstanding() {
		t.Fatal("clean committed receipt is still outstanding")
	}
	data, err := Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil || !reflect.DeepEqual(decoded, ledger) {
		t.Fatalf("ledger roundtrip: %v", err)
	}
}

func TestSEC030LedgerCorruption(t *testing.T) {
	for name, data := range map[string][]byte{
		"missing":         nil,
		"null":            []byte("null"),
		"unknown-version": []byte(`{"Version":2,"Records":{}}`),
		"duplicate-key":   []byte(`{"Version":1,"Version":1,"Records":{}}`),
		"case-alias":      []byte(`{"Version":1,"version":1,"Records":{}}`),
		"unknown-field":   []byte(`{"Version":1,"Records":{},"IgnoreMe":1}`),
		"trailing":        []byte(`{"Version":1,"Records":{}} {}`),
		"oversize":        bytes.Repeat([]byte(" "), MaxLedgerBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(data); err == nil {
				t.Fatal("corrupt ledger accepted")
			}
		})
	}
	ledger := EmptyLedger()
	ledger.Records["wrong"] = Reservation{ID: "another", UserID: 1}
	if _, err := Encode(ledger); err == nil {
		t.Fatal("invalid record encoded")
	}
}

func TestSEC030LedgerExpiryAndImmutability(t *testing.T) {
	upload := Reservation{ID: "first", UserID: 1, BytesKnown: true, Bytes: 3, StagingBytes: 3, ExpiresAt: 1001}
	limit := durableLimits()
	limit.User.Bytes = 3
	ledger, _, _, err := Apply(EmptyLedger(), nil, Change{Action: Reserve, Upload: upload, Now: 1000, Limits: limit})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := Encode(ledger)
	second := upload
	second.ID, second.ExpiresAt = "second", 2000
	_, _, _, err = Apply(ledger, nil, Change{Action: Reserve, Upload: second, Now: 1002, Limits: limit})
	if !errors.Is(err, ErrQuota) {
		t.Fatalf("expired staging lost capacity: %v", err)
	}
	after, _ := Encode(ledger)
	if !bytes.Equal(before, after) {
		t.Fatal("failed transition mutated caller ledger")
	}
	ledger, _, _, err = Apply(ledger, nil, Change{Action: Abort, Upload: upload, Terminal: Expired, CleanupConfirmed: true, Now: 1002})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Apply(ledger, nil, Change{Action: Reserve, Upload: second, Now: 1002, Limits: limit}); err != nil {
		t.Fatal("cleanup did not restore capacity", err)
	}
	bad := second
	bad.ID = strings.Repeat("a", MaxIDLength+1)
	if _, _, _, err := Apply(ledger, nil, Change{Action: Reserve, Upload: bad, Now: 1002, Limits: limit}); err == nil {
		t.Fatal("unbounded ID accepted")
	}
}
