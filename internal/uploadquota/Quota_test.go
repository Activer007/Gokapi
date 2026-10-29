package uploadquota

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestMeasureLogicalUsage(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	files := map[string]models.File{
		"original":    {Id: "original", UserId: 1, SizeBytes: gib, SHA1: "shared"},
		"copy":        {Id: "copy", UserId: 1, SizeBytes: gib, SHA1: "shared", UploadRequestId: "request"},
		"other-owner": {Id: "other-owner", UserId: 2, SizeBytes: gib, SHA1: "shared", UploadRequestId: "request"},
		"encrypted":   {Id: "encrypted", UserId: 2, SizeBytes: 17, Size: "1 B", Encryption: models.EncryptionInfo{IsEncrypted: true}},
		"e2e":         {Id: "e2e", UserId: 2, SizeBytes: 31, Size: "999 TB", Encryption: models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}},
		"pending":     {Id: "pending", UserId: 3, SizeBytes: 5, PendingDeletion: 1, UploadRequestId: "request"},
		"expired":     {Id: "expired", UserId: 3, SizeBytes: 7, ExpireAt: 1, UploadRequestId: "request"},
		"empty":       {Id: "empty", UserId: 3, SizeBytes: 0},
	}
	before, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Measure(files)
	if err != nil {
		t.Fatal(err)
	}
	want := Snapshot{
		Users:    map[int]Usage{1: {Bytes: 2 * gib, Files: 2}, 2: {Bytes: gib + 48, Files: 3}, 3: {Bytes: 12, Files: 3}},
		Requests: map[string]Usage{"request": {Bytes: 2*gib + 12, Files: 4}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("logical usage = %#v; want %#v", got, want)
	}
	after, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("measurement changed source metadata")
	}
	got.Users[1] = Usage{}
	got.Requests["request"] = Usage{}
	again, err := Measure(files)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal("returned maps affected subsequent measurement")
	}
}

func TestMeasureOwnershipTransfer(t *testing.T) {
	files := map[string]models.File{"file": {Id: "file", UserId: 1, SizeBytes: 23, UploadRequestId: "original-request"}}
	if got, err := Measure(files); err != nil || got.Users[1] != (Usage{Bytes: 23, Files: 1}) {
		t.Fatalf("initial owner usage = %#v, error = %v", got, err)
	}
	file := files["file"]
	file.UserId = 2
	files["file"] = file
	got, err := Measure(files)
	want := Snapshot{Users: map[int]Usage{2: {Bytes: 23, Files: 1}}, Requests: map[string]Usage{"original-request": {Bytes: 23, Files: 1}}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("transferred usage = %#v, error = %v; want %#v", got, err, want)
	}
	delete(files, "file")
	got, err = Measure(files)
	if err != nil || len(got.Users) != 0 || len(got.Requests) != 0 {
		t.Fatalf("removed metadata retained usage: %#v, %v", got, err)
	}
}

func TestMeasureEmptyAndInvalidMetadata(t *testing.T) {
	got, err := Measure(nil)
	if err != nil || got.Users == nil || got.Requests == nil || len(got.Users) != 0 || len(got.Requests) != 0 {
		t.Fatalf("empty metadata = %#v, error = %v", got, err)
	}
	for name, files := range map[string]map[string]models.File{
		"negative-size":  {"file": {Id: "file", UserId: 1, SizeBytes: -1}},
		"missing-owner":  {"file": {Id: "file", UserId: 0}},
		"negative-owner": {"file": {Id: "file", UserId: -1}},
		"missing-id":     {"file": {UserId: 1}},
		"missing-key":    {"": {Id: "file", UserId: 1}},
		"mismatched-id":  {"different": {Id: "file", UserId: 1}},
		"owner-overflow": {
			"large": {Id: "large", UserId: 1, SizeBytes: math.MaxInt64},
			"extra": {Id: "extra", UserId: 1, SizeBytes: 1},
		},
		"request-overflow": {
			"large": {Id: "large", UserId: 1, SizeBytes: math.MaxInt64, UploadRequestId: "request"},
			"extra": {Id: "extra", UserId: 2, SizeBytes: 1, UploadRequestId: "request"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Measure(files)
			if err == nil {
				t.Fatal("invalid metadata produced an accepted snapshot")
			}
			if got.Users != nil || got.Requests != nil {
				t.Fatal("invalid metadata exposed a partial snapshot")
			}
		})
	}
}

func TestCanReserveBoundaries(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	for _, tc := range []struct {
		name                             string
		used, reserved, requested, limit Usage
		want                             bool
		wantErr                          bool
	}{
		{name: "zero-capacity-empty", want: true},
		{name: "zero-bytes-not-unlimited", requested: Usage{Bytes: 1}, want: false},
		{name: "zero-files-not-unlimited", requested: Usage{Files: 1}, want: false},
		{name: "exact-both", used: Usage{Bytes: 8, Files: 2}, reserved: Usage{Bytes: 4, Files: 1}, requested: Usage{Bytes: 3, Files: 1}, limit: Usage{Bytes: 15, Files: 4}, want: true},
		{name: "bytes-exceeded", used: Usage{Bytes: 8, Files: 2}, reserved: Usage{Bytes: 4, Files: 1}, requested: Usage{Bytes: 4, Files: 1}, limit: Usage{Bytes: 15, Files: 4}},
		{name: "files-exceeded", used: Usage{Bytes: 8, Files: 2}, reserved: Usage{Bytes: 4, Files: 1}, requested: Usage{Bytes: 3, Files: 2}, limit: Usage{Bytes: 15, Files: 4}},
		{name: "used-already-exceeded", used: Usage{Bytes: 16}, limit: Usage{Bytes: 15}},
		{name: "zero-byte-file", requested: Usage{Files: 1}, limit: Usage{Files: 1}, want: true},
		{name: "two-gib-accounting", used: Usage{Bytes: gib, Files: 1}, reserved: Usage{Bytes: gib, Files: 1}, requested: Usage{Bytes: 2 * gib, Files: 1}, limit: Usage{Bytes: 4 * gib, Files: 3}, want: true},
		{name: "max-boundary", used: Usage{Bytes: math.MaxInt64 - 2, Files: math.MaxInt64 - 2}, reserved: Usage{Bytes: 1, Files: 1}, requested: Usage{Bytes: 1, Files: 1}, limit: Usage{Bytes: math.MaxInt64, Files: math.MaxInt64}, want: true},
		{name: "used-negative-bytes", used: Usage{Bytes: -1}, wantErr: true},
		{name: "reserved-negative-files", reserved: Usage{Files: -1}, wantErr: true},
		{name: "requested-negative-bytes", requested: Usage{Bytes: -1}, wantErr: true},
		{name: "limit-negative-files", limit: Usage{Files: -1}, wantErr: true},
		{name: "bytes-used-reserved-overflow", used: Usage{Bytes: math.MaxInt64}, reserved: Usage{Bytes: 1}, limit: Usage{Bytes: math.MaxInt64}, wantErr: true},
		{name: "bytes-requested-overflow", reserved: Usage{Bytes: math.MaxInt64}, requested: Usage{Bytes: 1}, limit: Usage{Bytes: math.MaxInt64}, wantErr: true},
		{name: "files-used-reserved-overflow", used: Usage{Files: math.MaxInt64}, reserved: Usage{Files: 1}, limit: Usage{Files: math.MaxInt64}, wantErr: true},
		{name: "files-requested-overflow", reserved: Usage{Files: math.MaxInt64}, requested: Usage{Files: 1}, limit: Usage{Files: math.MaxInt64}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanReserve(tc.used, tc.reserved, tc.requested, tc.limit)
			if got != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("CanReserve = %v, error = %v; want %v, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
