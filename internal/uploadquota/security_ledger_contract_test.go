package uploadquota

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
)

func ledgerContractLimits() Limits {
	return Limits{User: Usage{Bytes: math.MaxInt64, Files: 10000}, Request: Usage{Bytes: math.MaxInt64, Files: 10000},
		Instance: Usage{Bytes: math.MaxInt64, Files: 10000}, UserActive: 10000, RequestActive: 10000,
		InstanceActive: 10000, StagingBytes: math.MaxInt64}
}

func ledgerContractRecord(id string) Reservation {
	return Reservation{ID: id, UserID: 1, BytesKnown: true, Bytes: 1, StagingBytes: 1,
		CreatedAt: 1000, ExpiresAt: 4600, State: Reserved}
}

func ledgerContractChange(action Action, upload Reservation, now int64) Change {
	return Change{Action: action, Upload: upload, Now: now, Limits: ledgerContractLimits()}
}

func ledgerContractApply(t *testing.T, ledger Ledger, files map[string]models.File, change Change) (Ledger, Reservation, *models.File) {
	t.Helper()
	next, record, metadata, err := Apply(ledger, files, change)
	if err != nil {
		t.Fatalf("%s: %v", change.Action, err)
	}
	return next, record, metadata
}

func ledgerContractUnchanged(t *testing.T, ledger Ledger, files map[string]models.File, change Change, want error) {
	t.Helper()
	beforeLedger, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	beforeFiles, err := json.Marshal(files)
	if err != nil {
		t.Fatal(err)
	}
	next, record, metadata, err := Apply(ledger, files, change)
	if err == nil || (want != nil && !errors.Is(err, want)) {
		t.Fatalf("%s error = %v; want %v", change.Action, err, want)
	}
	if next.Records != nil || record != (Reservation{}) || metadata != nil {
		t.Fatal("failed transition exposed partial state")
	}
	afterLedger, _ := json.Marshal(ledger)
	afterFiles, _ := json.Marshal(files)
	if !bytes.Equal(beforeLedger, afterLedger) || !bytes.Equal(beforeFiles, afterFiles) {
		t.Fatal("failed transition mutated input ledger or metadata")
	}
}

func TestSEC030LedgerBoundedRecordsAllowCleanup(t *testing.T) {
	for _, kind := range []string{"instance", "user", "request"} {
		t.Run(kind, func(t *testing.T) {
			ledger := EmptyLedger()
			count := MaxRecords
			if kind == "user" {
				count = MaxUserRecords
			}
			if kind == "request" {
				count = MaxRequestRecords
			}
			for i := 0; i < count; i++ {
				r := ledgerContractRecord(fmt.Sprintf("upload-%04d", i))
				switch kind {
				case "instance":
					r.UserID, r.RequestID = i/128+1, fmt.Sprintf("request-%d", i/64)
				case "user":
					r.RequestID = ""
				case "request":
					r.UserID, r.RequestID = i/32+1, "full-request"
				}
				ledger.Records[r.ID] = r
			}
			if _, err := Encode(ledger); err != nil {
				t.Fatalf("direct valid fixture: %v", err)
			}
			candidate := ledgerContractRecord("new-upload")
			candidate.UserID = 9001
			if kind == "user" {
				candidate.UserID = 1
			}
			if kind == "request" {
				candidate.RequestID = "full-request"
			}
			ledgerContractUnchanged(t, ledger, nil, ledgerContractChange(Reserve, candidate, 1000), ErrFull)
			existing := ledger.Records["upload-0000"]
			abort := ledgerContractChange(Abort, existing, 1001)
			abort.Terminal, abort.CleanupConfirmed = Aborted, true
			next, got, _ := ledgerContractApply(t, ledger, nil, abort)
			if got.State != Aborted || !got.StagingReleased || len(next.Records) != count {
				t.Fatal("full receipt ledger blocked safe cleanup or dropped receipts")
			}
			ledgerContractUnchanged(t, next, nil, ledgerContractChange(Reserve, candidate, 1001), ErrFull)
		})
	}
}

func TestSEC030LedgerPrunesOnlyCleanOldTerminals(t *testing.T) {
	ledger := EmptyLedger()
	for _, id := range []string{"active-old", "committed-pending", "committed-clean", "aborted-clean", "expired-clean"} {
		r := ledgerContractRecord(id)
		r.CreatedAt, r.ExpiresAt = 1, 3
		if id != "active-old" {
			r.FinishedAt = 2
			r.StagingReleased = id != "committed-pending"
			switch id {
			case "committed-pending", "committed-clean":
				r.State, r.FileID, r.CommitDigest = Committed, "file-"+id, strings.Repeat("a", 64)
			case "aborted-clean":
				r.State = Aborted
			case "expired-clean":
				r.State, r.FinishedAt = Expired, 3
			}
		}
		ledger.Records[id] = r
	}
	before, err := Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	now := ReceiptLifetime + 4
	candidate := ledgerContractRecord("new")
	candidate.ExpiresAt = now + 100
	next, _, _ := ledgerContractApply(t, ledger, nil, ledgerContractChange(Reserve, candidate, now))
	if len(next.Records) != 3 {
		t.Fatalf("pruned wrong records: %#v", next.Records)
	}
	for _, id := range []string{"active-old", "committed-pending", "new"} {
		if _, ok := next.Records[id]; !ok {
			t.Fatalf("unsafe removal of %s", id)
		}
	}
	after, _ := Encode(ledger)
	if !bytes.Equal(before, after) {
		t.Fatal("pruning mutated caller ledger")
	}
}

func TestSEC030LedgerRecoveryPages(t *testing.T) {
	ledger := EmptyLedger()
	for _, id := range []string{"a-active", "b-pending", "c-active", "d-aborted", "e-clean"} {
		r := ledgerContractRecord(id)
		if id == "b-pending" || id == "e-clean" {
			r.State, r.FileID, r.CommitDigest, r.FinishedAt = Committed, "file-"+id, strings.Repeat("b", 64), 1001
			r.StagingReleased = id == "e-clean"
		}
		if id == "d-aborted" {
			r.State, r.StagingReleased, r.FinishedAt = Aborted, true, 1001
		}
		ledger.Records[id] = r
	}
	var got []string
	cursor := ""
	for page := 0; page < 4; page++ {
		batch, next, err := ledger.RecoveryBatch(cursor, 2)
		if err != nil || len(batch) > 2 {
			t.Fatalf("unbounded/invalid recovery page: %d %v", len(batch), err)
		}
		for _, record := range batch {
			got = append(got, record.ID)
		}
		if next == "" {
			break
		}
		if next <= cursor {
			t.Fatal("recovery cursor did not advance")
		}
		cursor = next
	}
	if !reflect.DeepEqual(got, []string{"a-active", "b-pending", "c-active"}) {
		t.Fatalf("recovery included clean receipts or missed outstanding records: %v", got)
	}
	for _, tc := range []struct {
		after string
		limit int
	}{{"", 0}, {"", -1}, {"", 129}, {"../path", 1}} {
		if _, _, err := ledger.RecoveryBatch(tc.after, tc.limit); err == nil {
			t.Fatal("invalid recovery bounds accepted")
		}
	}
	batch, next, err := ledger.RecoveryBatch("z-after-all", 128)
	if err != nil || len(batch) != 0 || next != "" {
		t.Fatal("empty recovery tail is invalid")
	}
	batch, _, err = ledger.RecoveryBatch("", 1)
	if err != nil {
		t.Fatal(err)
	}
	batch[0].Bytes = 1234
	if ledger.Records["a-active"].Bytes != 1 {
		t.Fatal("recovery result aliases stored records")
	}
}

func TestSEC030LedgerExactIntegerEncoding(t *testing.T) {
	for _, n := range []int64{1<<53 + 1, math.MaxInt64} {
		ledger := EmptyLedger()
		r := ledgerContractRecord("precise")
		r.Bytes, r.StagingBytes = n, n
		ledger.Records[r.ID] = r
		data, err := Encode(ledger)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(data)
		if err != nil || !reflect.DeepEqual(decoded, ledger) {
			t.Fatalf("lost exact int64 %d: %#v, %v", n, decoded, err)
		}
	}
}

func TestSEC030LedgerRejectsHostileRecords(t *testing.T) {
	base := EmptyLedger()
	r := ledgerContractRecord("record")
	base.Records[r.ID] = r
	data, err := Encode(base)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(Reservation) Reservation{
		"unknown-state":          func(r Reservation) Reservation { r.State = "unknown"; return r },
		"negative-bytes":         func(r Reservation) Reservation { r.Bytes = -1; return r },
		"negative-staging":       func(r Reservation) Reservation { r.StagingBytes = -1; return r },
		"unbound-nonzero":        func(r Reservation) Reservation { r.BytesKnown = false; return r },
		"staging-less-than-file": func(r Reservation) Reservation { r.StagingBytes = 0; return r },
		"active-released":        func(r Reservation) Reservation { r.StagingReleased = true; return r },
		"reserved-completing":    func(r Reservation) Reservation { r.Completing, r.FileID = true, "file"; return r },
		"claimed-without-id":     func(r Reservation) Reservation { r.State, r.Completing = Uploading, true; return r },
		"uploading-unbound":      func(r Reservation) Reservation { r.State, r.BytesKnown, r.Bytes = Uploading, false, 0; return r },
		"expired-before-expiry": func(r Reservation) Reservation {
			r.State, r.StagingReleased, r.FinishedAt = Expired, true, 1001
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			ledger := EmptyLedger()
			ledger.Records[r.ID] = mutate(r)
			corrupt, err := json.Marshal(ledger)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Decode(corrupt); err == nil {
				t.Fatal("invalid reservation decoded")
			}
			if _, err := Encode(ledger); err == nil {
				t.Fatal("invalid reservation encoded")
			}
		})
	}
	for name, corrupt := range map[string][]byte{
		"duplicate-record-field": bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":1,"Bytes":0`), 1),
		"field-case-alias":       bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":1,"bytes":0`), 1),
		"field-unknown":          bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":1,"Untrusted":1`), 1),
		"int64-overflow":         bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":9223372036854775808`), 1),
		"fractional-bytes":       bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":1.5`), 1),
		"nested-record-object":   bytes.Replace(data, []byte(`"Bytes":1`), []byte(`"Bytes":{"deep":{"nested":1}}`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(corrupt, data) {
				t.Fatal("mutation did not alter fixture")
			}
			if _, err := Decode(corrupt); err == nil {
				t.Fatal("hostile JSON accepted")
			}
		})
	}
}

func TestSEC030LedgerOverflowFailsClosed(t *testing.T) {
	for _, dimension := range []string{"logical-reserved", "staging-reserved", "logical-used"} {
		t.Run(dimension, func(t *testing.T) {
			ledger := EmptyLedger()
			files := map[string]models.File{}
			large := ledgerContractRecord("large")
			large.Bytes, large.StagingBytes = math.MaxInt64, math.MaxInt64
			switch dimension {
			case "logical-reserved":
				ledger.Records[large.ID] = large
			case "staging-reserved":
				large.Bytes = 0
				ledger.Records[large.ID] = large
			case "logical-used":
				files["large-file"] = models.File{Id: "large-file", UserId: 1, SizeBytes: math.MaxInt64}
			}
			candidate := ledgerContractRecord("extra")
			change := ledgerContractChange(Reserve, candidate, 1000)
			ledgerContractUnchanged(t, ledger, files, change, ErrInvalid)
		})
	}
}

func TestSEC030LedgerInvalidMetadataAndNoAliasing(t *testing.T) {
	for _, files := range []map[string]models.File{
		{"bad": {Id: "bad", UserId: 1, SizeBytes: -1}},
		{"wrong-key": {Id: "different", UserId: 1, SizeBytes: 1}},
		{"ownerless": {Id: "ownerless", UserId: 0, SizeBytes: 1}},
	} {
		ledgerContractUnchanged(t, EmptyLedger(), files, ledgerContractChange(Reserve, ledgerContractRecord("new"), 1000), ErrInvalid)
	}
	ledger := EmptyLedger()
	r := ledgerContractRecord("existing")
	ledger.Records[r.ID] = r
	next, _, _ := ledgerContractApply(t, ledger, nil, ledgerContractChange(Reserve, ledgerContractRecord("new"), 1000))
	next.Records["existing"] = Reservation{}
	delete(next.Records, "new")
	if ledger.Records["existing"] != r || len(ledger.Records) != 1 {
		t.Fatal("returned ledger aliases input")
	}
}

func TestSEC030LedgerReceiptReplayAfterOriginalExpiry(t *testing.T) {
	upload := ledgerContractRecord("replay")
	ledger, _, _ := ledgerContractApply(t, EmptyLedger(), nil, ledgerContractChange(Reserve, upload, 1000))
	claim := ledgerContractChange(Claim, upload, 1001)
	claim.FileID = "original-file"
	ledger, _, _ = ledgerContractApply(t, ledger, nil, claim)
	file := models.File{Id: claim.FileID, UserId: upload.UserID, SizeBytes: upload.Bytes, Name: "original"}
	commit := ledgerContractChange(Commit, upload, 1002)
	commit.File = &file
	ledger, _, _ = ledgerContractApply(t, ledger, nil, commit)
	// Current metadata may have been edited, transferred or removed. An old
	// completion receipt must not rewrite it during an idempotent retry.
	for name, files := range map[string]map[string]models.File{
		"removed": {},
		"edited":  {file.Id: {Id: file.Id, UserId: 2, SizeBytes: file.SizeBytes, Name: "edited"}},
	} {
		t.Run(name, func(t *testing.T) {
			replay := commit
			replay.Now = upload.ExpiresAt + 1
			beforeFiles, _ := json.Marshal(files)
			next, record, metadata := ledgerContractApply(t, ledger, files, replay)
			afterFiles, _ := json.Marshal(files)
			if metadata != nil || record.State != Committed || !reflect.DeepEqual(next, ledger) || !bytes.Equal(beforeFiles, afterFiles) {
				t.Fatal("receipt replay resurrected or rewrote current metadata")
			}
		})
	}
}

func TestSEC030LedgerOriginalDeclarationReplay(t *testing.T) {
	unbound := ledgerContractRecord("initially-unbound")
	unbound.BytesKnown, unbound.Bytes, unbound.StagingBytes = false, 0, 0
	ledger, _, _ := ledgerContractApply(t, EmptyLedger(), nil, ledgerContractChange(Reserve, unbound, 1000))
	bound := unbound
	bound.BytesKnown, bound.Bytes, bound.StagingBytes = true, 7, 14
	ledger, current, _ := ledgerContractApply(t, ledger, nil, ledgerContractChange(Bind, bound, 1001))
	retry := ledgerContractChange(Reserve, unbound, 1002)
	next, got, metadata := ledgerContractApply(t, ledger, nil, retry)
	if !reflect.DeepEqual(next, ledger) || got != current || metadata != nil || !got.BytesKnown || got.Bytes != 7 || got.StagingBytes != 14 {
		t.Fatal("original unbound Reserve replay changed or lost its binding")
	}
	// Reserve retries use the original declaration mode; changing declaration
	// is exclusively a Bind operation, even if it happens to equal stored size.
	ledgerContractUnchanged(t, ledger, nil, ledgerContractChange(Reserve, bound, 1002), ErrConflict)
	known := ledgerContractRecord("initially-known")
	ledger, _, _ = ledgerContractApply(t, ledger, nil, ledgerContractChange(Reserve, known, 1002))
	known.BytesKnown, known.Bytes, known.StagingBytes = false, 0, 0
	ledgerContractUnchanged(t, ledger, nil, ledgerContractChange(Reserve, known, 1003), ErrConflict)
}
