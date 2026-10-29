package filerequest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

func displayReservationLedger(t *testing.T) uploadquota.Ledger {
	t.Helper()
	ledger := uploadquota.EmptyLedger()
	add := func(id, request string, state uploadquota.State) {
		record := uploadquota.Reservation{ID: id, UserID: 1, RequestID: request,
			BytesKnown: true, Bytes: 1, StagingBytes: 1, CreatedAt: 1000, ExpiresAt: 4600, State: state}
		switch state {
		case uploadquota.Committed:
			record.FileID, record.FinishedAt, record.CommitDigest = "committed-file", 1001, strings.Repeat("0", 64)
		case uploadquota.Aborted:
			record.StagingReleased, record.FinishedAt = true, 1001
		case uploadquota.Expired:
			record.StagingReleased, record.FinishedAt = true, 4600
		}
		ledger.Records[id] = record
	}
	add("expired-active", "request-a", uploadquota.Reserved)
	add("completing", "request-a", uploadquota.Uploading)
	record := ledger.Records["completing"]
	record.Completing, record.FileID = true, "claimed-file"
	ledger.Records[record.ID] = record
	add("other-request", "request-b", uploadquota.Uploading)
	add("metadata-already-counted", "request-a", uploadquota.Committed)
	add("aborted", "request-a", uploadquota.Aborted)
	add("expired-clean", "request-a", uploadquota.Expired)
	add("ordinary-upload", "", uploadquota.Reserved)
	if _, err := uploadquota.Encode(ledger); err != nil {
		t.Fatalf("invalid synthetic display fixture: %v", err)
	}
	return ledger
}

func TestSEC030RequestReservationDisplay(t *testing.T) {
	ledger := displayReservationLedger(t)
	requests := []models.FileRequest{
		{Id: "request-a", ReservedUploads: 1, UploadedFiles: 7, MaxFiles: 10, TotalFileSize: 90},
		{Id: "request-b"},
		{Id: "legacy-only", ReservedUploads: 3},
	}
	want := append([]models.FileRequest{}, requests...)
	want[0].ReservedUploads, want[1].ReservedUploads = 2, 1
	reads := 0
	populateReservationDisplay(requests, func(ctx context.Context) (uploadquota.Ledger, error) {
		reads++
		deadline, bounded := ctx.Deadline()
		if !bounded || ctx.Err() != nil || time.Until(deadline) > time.Second {
			t.Fatal("display ledger read lacks the finite request deadline")
		}
		return ledger, nil
	})
	if reads != 1 || !reflect.DeepEqual(requests, want) {
		t.Fatal("display lost active/legacy reservations, double-counted committed metadata or changed other fields")
	}
	// A larger old in-memory count stays visible without adding possibly
	// duplicated legacy/durable representations of the same upload.
	requests[0].ReservedUploads = 5
	populateReservationDisplay(requests[:1], func(context.Context) (uploadquota.Ledger, error) { return ledger, nil })
	if requests[0].ReservedUploads != 5 {
		t.Fatal("durable display erased or summed the legacy transition count")
	}
}

func TestSEC030RequestReservationDisplayUnavailable(t *testing.T) {
	ledger := displayReservationLedger(t)
	for _, scenario := range []string{"read-error", "unknown-version", "missing-records"} {
		t.Run(scenario, func(t *testing.T) {
			requests := []models.FileRequest{{Id: "request-a", ReservedUploads: 1, UploadedFiles: 7}}
			before := append([]models.FileRequest{}, requests...)
			populateReservationDisplay(requests, func(context.Context) (uploadquota.Ledger, error) {
				copy := ledger
				switch scenario {
				case "read-error":
					return copy, errors.New("synthetic display read failure")
				case "unknown-version":
					copy.Version++
				case "missing-records":
					copy.Records = nil
				}
				return copy, nil
			})
			if !reflect.DeepEqual(requests, before) {
				t.Fatal("unavailable durable display overwrote the non-authoritative legacy fallback")
			}
		})
	}
	populateReservationDisplay(nil, func(context.Context) (uploadquota.Ledger, error) {
		t.Fatal("empty request listing unnecessarily read the ledger")
		return ledger, nil
	})
}
