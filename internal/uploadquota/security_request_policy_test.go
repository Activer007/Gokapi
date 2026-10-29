package uploadquota

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func sec030RequestChange() Change {
	return Change{Action: Reserve, CheckRequestPolicy: true, Now: 1000,
		Upload: Reservation{ID: "request-upload", UserID: 1, RequestID: "request", BytesKnown: true,
			Bytes: 2 << 20, StagingBytes: 4 << 20, ExpiresAt: 4600},
		Limits: Limits{User: Usage{Bytes: 100 << 20, Files: 100}, Request: Usage{Bytes: 100 << 20, Files: 99},
			Instance: Usage{Bytes: 200 << 20, Files: 20}, UserActive: 10, RequestActive: 2, InstanceActive: 20, StagingBytes: 200 << 20}}
}

func TestSEC030RequestPolicyResolution(t *testing.T) {
	for _, count := range []int64{0, 3, 100} {
		change := sec030RequestChange()
		before := change
		policy := RequestPolicy{UserID: 1, MaxFiles: count, MaxSizeMB: 8, Expiry: 4600}
		got, err := ApplyRequestPolicy(EmptyLedger(), change, policy)
		want := min(count, change.Limits.Instance.Files)
		if count == 0 {
			want = change.Limits.Instance.Files
		}
		if err != nil || got.Limits.Request.Files != want || got.Limits.Request.Bytes != change.Limits.Request.Bytes || got.Limits.RequestActive != change.Limits.RequestActive {
			t.Fatal("request policy did not resolve finite counts without replacing supplied total/active capacities", err)
		}
		if !reflect.DeepEqual(change, before) {
			t.Fatal("request policy changed its input")
		}
	}
}

func TestSEC030RequestPolicyRejectsCurrentPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy RequestPolicy
		want   error
	}{
		{"owner-mismatch", RequestPolicy{UserID: 2, MaxFiles: 10, MaxSizeMB: 8}, ErrConflict},
		{"invalid-owner", RequestPolicy{MaxFiles: 10, MaxSizeMB: 8}, ErrInvalid},
		{"expired", RequestPolicy{UserID: 1, MaxFiles: 10, MaxSizeMB: 8, Expiry: 999}, ErrExpired},
		{"negative-count", RequestPolicy{UserID: 1, MaxFiles: -1}, ErrInvalid},
		{"negative-size", RequestPolicy{UserID: 1, MaxSizeMB: -1}, ErrInvalid},
		{"negative-expiry", RequestPolicy{UserID: 1, Expiry: -1}, ErrInvalid},
		{"size-conversion-overflow", RequestPolicy{UserID: 1, MaxSizeMB: math.MaxInt64/(1<<20) + 1}, ErrInvalid},
		{"single-file", RequestPolicy{UserID: 1, MaxFiles: 10, MaxSizeMB: 1}, ErrQuota},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ApplyRequestPolicy(EmptyLedger(), sec030RequestChange(), tc.policy); !errors.Is(err, tc.want) {
				t.Fatalf("request policy error=%v; want %v", err, tc.want)
			}
		})
	}
}

func TestSEC030RequestPolicyBoundBytesAndLegacyZero(t *testing.T) {
	policy := RequestPolicy{UserID: 1, MaxSizeMB: 1, Expiry: 1000}
	change := sec030RequestChange()
	bound := change.Upload
	bound.CreatedAt, bound.State, bound.InitiallyUnbound = 900, Uploading, true
	ledger := EmptyLedger()
	ledger.Records[bound.ID] = bound
	for _, action := range []Action{Reserve, Claim, Commit} {
		change.Action = action
		change.Upload.BytesKnown, change.Upload.Bytes, change.Upload.StagingBytes = false, 0, 0
		if _, err := ApplyRequestPolicy(ledger, change, policy); !errors.Is(err, ErrQuota) {
			t.Fatalf("%s ignored the stored bound length when checking current MaxSize", action)
		}
	}
	change = sec030RequestChange()
	policy.MaxSizeMB = 0
	if _, err := ApplyRequestPolicy(EmptyLedger(), change, policy); err != nil {
		t.Fatal("legacy zero layers or exact expiry second changed", err)
	}
	change.Upload.BytesKnown, change.Upload.Bytes, change.Upload.StagingBytes = false, 0, 0
	policy.MaxSizeMB = 1
	if _, err := ApplyRequestPolicy(EmptyLedger(), change, policy); err != nil {
		t.Fatal("unknown-size reservation was mistaken for a declared large file", err)
	}
}

func TestSEC030RequestPolicyCleanupAndOptIn(t *testing.T) {
	for _, action := range []Action{Abort, ReleaseStaging, Reserve} {
		change := sec030RequestChange()
		change.Action = action
		if action == Reserve {
			change.CheckRequestPolicy = false
		}
		got, err := ApplyRequestPolicy(EmptyLedger(), change, RequestPolicy{})
		if err != nil || !reflect.DeepEqual(got, change) || change.RequiresRequestPolicy() {
			t.Fatal("cleanup or existing foundation command unexpectedly required current request policy", err)
		}
	}
}
