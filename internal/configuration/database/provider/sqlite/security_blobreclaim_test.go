//go:build test && security_reclaim

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func sec041SQLiteCandidate(t *testing.T, hash, bucket string) blobreclaim.Candidate {
	t.Helper()
	candidate, err := blobreclaim.New(hash, bucket)
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func sec041SQLiteChange(target, source models.File) uploadquota.Change {
	return uploadquota.Change{Action: uploadquota.ReplaceMetadata, Target: &target, Source: &source, Now: 1000,
		Upload: uploadquota.Reservation{ID: target.Id, UserID: target.UserId},
		Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 100}, Instance: uploadquota.Usage{Bytes: 100}}}
}

func TestSEC041SQLiteReplacementCandidateSurvivesReopenAndStaleAck(t *testing.T) {
	ctx := context.Background()
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "reclaims.sqlite")}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	if p.GetDbVersion() != 17 {
		t.Fatal("fresh database is not schema 17")
	}
	target := models.File{Id: "replacement-target", UserId: 1, SHA1: "old-blob", SizeBytes: 3,
		UnlimitedTime: true, UnlimitedDownloads: true}
	source := target
	source.Id, source.UserId, source.SHA1 = "replacement-source", 2, "new-blob"
	original := target
	original.Id = "original-source"
	for _, file := range []models.File{target, source, original} {
		p.SaveMetaData(file)
	}
	before, err := p.ReadUploadQuota(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyUploadQuota(ctx, sec041SQLiteChange(target, source)); err != nil {
		t.Fatal("valid replacement failed", err)
	}
	candidates, err := p.ReadBlobCandidates(ctx)
	if err != nil || len(candidates) != 1 || candidates[0].SHA1 != target.SHA1 || candidates[0].AwsBucket != target.AwsBucket {
		t.Fatal("replacement did not preserve its old locator", err)
	}
	first := candidates[0]
	p.Close()
	p, err = New(config)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := p.ReadBlobCandidates(ctx); err != nil || !reflect.DeepEqual(got, candidates) {
		t.Fatal("reconnect lost the candidate", err)
	}
	if got, found, err := p.ReadBlobCandidate(ctx, first.Key()); err != nil || !found || got != first {
		t.Fatal("fresh candidate lookup lost the exact token", err)
	}
	// Reusing the original file ID must count as a real reference to old-blob.
	for _, replacement := range []models.File{original, source} {
		current, found, err := p.ReadRecoveryFile(ctx, target.Id)
		if err != nil || !found {
			t.Fatal("could not read current replacement target", err)
		}
		if _, err := p.ApplyUploadQuota(ctx, sec041SQLiteChange(current, replacement)); err != nil {
			t.Fatal(err)
		}
		if replacement.SHA1 == first.SHA1 {
			if found, err := p.HasAnyBlobReference(ctx, first); err != nil || !found {
				t.Fatal("same target ID was excluded from live references", err)
			}
		}
	}
	candidates, err = p.ReadBlobCandidates(ctx)
	if err != nil || len(candidates) != 2 {
		t.Fatal("locator records were not deduplicated", err)
	}
	var renewed blobreclaim.Candidate
	for _, candidate := range candidates {
		if candidate.Key() == first.Key() {
			renewed = candidate
		}
	}
	if renewed.Token == "" || renewed.Token == first.Token {
		t.Fatal("a new replacement did not refresh the locator token")
	}
	if got, found, err := p.ReadBlobCandidate(ctx, renewed.Key()); err != nil || !found || got != renewed {
		t.Fatal("fresh lookup returned the retired generation", err)
	}
	if retired, err := p.RetireBlobCandidate(ctx, first); retired || !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal("old acknowledgement removed a newer intent", err)
	}
	current, _, err := p.ReadRecoveryFile(ctx, target.Id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyUploadQuota(ctx, sec041SQLiteChange(current, source)); err != nil {
		t.Fatal("same-locator replacement failed", err)
	}
	if got, err := p.ReadBlobCandidates(ctx); err != nil || !reflect.DeepEqual(got, candidates) {
		t.Fatal("same-locator replacement changed candidate intents", err)
	}
	if retired, err := p.RetireBlobCandidate(ctx, renewed); err != nil || !retired {
		t.Fatal("current intent was not acknowledged", err)
	}
	if retired, err := p.RetireBlobCandidate(ctx, renewed); err != nil || retired {
		t.Fatal("missing acknowledgement was not idempotent", err)
	}
	if after, err := p.ReadUploadQuota(ctx); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("replacement candidates changed the logical quota ledger", err)
	}
}

func TestSEC041SQLiteReplacementRegistrationFailureDoesNotPatch(t *testing.T) {
	for _, scenario := range []string{"full", "corrupt", "missing-schema"} {
		t.Run(scenario, func(t *testing.T) {
			p := newQuotaDatabase(t)
			ctx := context.Background()
			target := models.File{Id: "guard-target", UserId: 1, SHA1: "old-target", SizeBytes: 3,
				Name: "preserved", HotlinkId: "guard-link", UnlimitedTime: true, UnlimitedDownloads: true}
			source := models.File{Id: "guard-source", UserId: 2, SHA1: "replacement", SizeBytes: 3,
				UnlimitedTime: true, UnlimitedDownloads: true}
			p.SaveMetaData(target)
			p.SaveMetaData(source)
			p.SaveHotlink(target)
			want := uploadquota.ErrInvalid
			switch scenario {
			case "full":
				candidates := make([]blobreclaim.Candidate, blobreclaim.MaxCandidates)
				for i := range candidates {
					candidates[i] = sec041SQLiteCandidate(t, fmt.Sprintf("held-%04d", i), "")
				}
				if err := p.ImportBlobCandidates(ctx, candidates); err != nil {
					t.Fatal("could not prepare bounded full collection", err)
				}
				want = uploadquota.ErrFull
			case "corrupt":
				candidate := sec041SQLiteCandidate(t, "corrupt-locator", "")
				if _, err := p.sqliteDb.Exec("INSERT INTO BlobReclaims (Id, State) VALUES (?, ?)", candidate.Key(), []byte{0xff}); err != nil {
					t.Fatal(err)
				}
			case "missing-schema":
				if _, err := p.sqliteDb.Exec("DROP TABLE BlobReclaims"); err != nil {
					t.Fatal(err)
				}
				want = uploadquota.ErrSchema
			}
			before, err := p.ReadUploadQuota(ctx)
			if err != nil {
				t.Fatal(err)
			}
			change := sec041SQLiteChange(target, source)
			change.DeleteSource = true
			if _, err := p.ApplyUploadQuota(ctx, change); !errors.Is(err, want) {
				t.Fatalf("registration error=%v; want %v", err, want)
			}
			for _, expected := range []models.File{target, source} {
				if got, found := p.GetMetaDataById(expected.Id); !found || !reflect.DeepEqual(got, expected) {
					t.Fatal("failed registration partially patched or expired metadata")
				}
			}
			if linked, found := p.GetHotlink(target.HotlinkId); !found || linked != target.Id {
				t.Fatal("failed registration changed the hotlink")
			}
			if after, err := p.ReadUploadQuota(ctx); err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("failed registration changed the quota ledger", err)
			}
		})
	}
}

func TestSEC041SQLiteCandidateMigrationImportAndCheckedReads(t *testing.T) {
	p := newQuotaDatabase(t)
	ctx := context.Background()
	if _, err := p.sqliteDb.Exec("DROP TABLE BlobReclaims"); err != nil {
		t.Fatal(err)
	}
	p.SetDbVersion(16)
	if _, err := p.ReadBlobCandidates(ctx); !errors.Is(err, uploadquota.ErrSchema) || !errors.Is(err, uploadquota.ErrNotFound) {
		t.Fatal("legacy missing structure did not return its migration sentinel", err)
	}
	p.Upgrade(16)
	p.SetDbVersion(p.GetSchemaVersion())
	local := sec041SQLiteCandidate(t, "retained-blob", "")
	remote := sec041SQLiteCandidate(t, local.SHA1, "remote-bucket")
	if err := p.ImportBlobCandidates(ctx, []blobreclaim.Candidate{local, {}}); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("invalid batch was accepted", err)
	}
	if err := p.ImportBlobCandidates(ctx, []blobreclaim.Candidate{local, local}); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("duplicate locator import was accepted", err)
	}
	if got, err := p.ReadBlobCandidates(ctx); err != nil || len(got) != 0 {
		t.Fatal("invalid import partially committed", err)
	}
	if err := p.ImportBlobCandidates(ctx, []blobreclaim.Candidate{local, remote}); err != nil {
		t.Fatal(err)
	}
	if err := p.ImportBlobCandidates(ctx, nil); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal("nonempty candidate destination was reset", err)
	}
	if err := p.CheckEmptyForMigration(ctx); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal("migration preflight ignored detached content", err)
	}
	p.SaveMetaData(models.File{Id: "replacement-target", UserId: 1, SHA1: local.SHA1, Name: "display-only"})
	if found, err := p.HasAnyBlobReference(ctx, local); err != nil || !found {
		t.Fatal("current file reference was excluded", err)
	}
	if found, err := p.HasAnyBlobReference(ctx, remote); err != nil || found {
		t.Fatal("different buckets were treated as one locator", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.ReadBlobCandidates(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled candidate read was accepted", err)
	}
	if _, found, err := p.ReadBlobCandidate(ctx, "INVALID"); !errors.Is(err, uploadquota.ErrInvalid) || found {
		t.Fatal("unbounded or noncanonical candidate key was accepted", err)
	}
	if retired, err := p.RetireBlobCandidate(cancelled, local); !errors.Is(err, context.Canceled) || retired {
		t.Fatal("cancelled acknowledgement was accepted", err)
	}
	if _, err := p.sqliteDb.Exec("UPDATE BlobReclaims SET State = zeroblob(?) WHERE Id = ?", blobreclaim.MaxRecordBytes+1, local.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadBlobCandidates(ctx); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("oversized persistent candidate crossed the bounded read", err)
	}
	if _, found, err := p.ReadBlobCandidate(ctx, local.Key()); !errors.Is(err, uploadquota.ErrInvalid) || found {
		t.Fatal("fresh lookup accepted an oversized candidate", err)
	}
}
