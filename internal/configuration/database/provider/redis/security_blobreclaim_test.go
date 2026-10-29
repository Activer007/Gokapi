//go:build test && security_reclaim

package redis

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Activer007/SupaShare/internal/blobreclaim"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

func sec041RedisReplacement(t *testing.T) (sec030RedisScene, uploadquota.Change) {
	t.Helper()
	s := sec030RedisNew(t)
	target := models.File{Id: "reclaim-target", UserId: 1, SHA1: "old-blob", Name: "old.txt", Size: "3 B", SizeBytes: 3,
		ContentType: "text/plain", ExpireAt: 2000, DownloadsRemaining: 10, HotlinkId: "reclaim-link"}
	source := models.File{Id: "reclaim-source", UserId: 2, SHA1: "new-blob", Name: "new.txt", Size: "5 B", SizeBytes: 5,
		ContentType: "text/plain", ExpireAt: 2000, DownloadsRemaining: 10}
	s.db.SaveMetaData(target)
	s.db.SaveMetaData(source)
	s.db.SaveHotlink(target)
	return s, uploadquota.Change{Action: uploadquota.ReplaceMetadata, Target: &target, Source: &source,
		Upload: uploadquota.Reservation{ID: target.Id, UserID: target.UserId}, Now: 1000,
		Limits: uploadquota.Limits{User: uploadquota.Usage{Bytes: 100}, Instance: uploadquota.Usage{Bytes: 100}}}
}

func sec041Candidates(t *testing.T, db DatabaseProvider) []blobreclaim.Candidate {
	t.Helper()
	candidates, err := db.ReadBlobCandidates(t.Context())
	if err != nil {
		t.Fatal("candidate collection read failed", err)
	}
	return candidates
}

func TestSEC041RedisCandidateRegistrationAndStaleAck(t *testing.T) {
	s, change := sec041RedisReplacement(t)
	if len(sec041Candidates(t, s.db)) != 0 || s.db.GetDbVersion() != DatabaseSchemeVersion {
		t.Fatal("fresh candidate schema is not empty")
	}
	if _, err := s.db.ApplyUploadQuota(t.Context(), change); err != nil {
		t.Fatal("replacement failed", err)
	}
	first := sec041Candidates(t, s.db)
	if len(first) != 1 || first[0].SHA1 != change.Target.SHA1 || first[0].AwsBucket != change.Target.AwsBucket {
		t.Fatal("replacement lost its old locator")
	}
	other, err := New(s.config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(other.Close)
	if !reflect.DeepEqual(first, sec041Candidates(t, other)) {
		t.Fatal("reconnect lost the candidate token")
	}
	got, found, err := other.ReadBlobCandidate(t.Context(), first[0].Key())
	if err != nil || !found || got != first[0] {
		t.Fatal("checked single read changed the token", err)
	}
	// A second real replacement of this locator refreshes, rather than appends,
	// its registration. An old worker cannot acknowledge that new obligation.
	secondTarget := *change.Target
	secondTarget.Id, secondTarget.HotlinkId = "reclaim-target-two", ""
	s.db.SaveMetaData(secondTarget)
	second := change
	second.Target, second.Upload.ID = &secondTarget, secondTarget.Id
	if _, err := s.db.ApplyUploadQuota(t.Context(), second); err != nil {
		t.Fatal("second replacement failed", err)
	}
	current := sec041Candidates(t, s.db)
	if len(current) != 1 || current[0].Key() != first[0].Key() || current[0].Token == first[0].Token {
		t.Fatal("deduplicated registration did not refresh its token")
	}
	if retired, err := s.db.RetireBlobCandidate(t.Context(), first[0]); retired || !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal("stale acknowledgement removed a newer registration", err)
	}
	if retired, err := s.db.RetireBlobCandidate(t.Context(), current[0]); err != nil || !retired {
		t.Fatal("current acknowledgement failed", err)
	}
	if retired, err := s.db.RetireBlobCandidate(t.Context(), current[0]); err != nil || retired {
		t.Fatal("missing acknowledgement was not idempotent", err)
	}
	if len(sec041Candidates(t, other)) != 0 {
		t.Fatal("retirement damaged the empty collection marker")
	}
}

func TestSEC041RedisCandidateReferencesIncludeEveryID(t *testing.T) {
	s, change := sec041RedisReplacement(t)
	candidate, err := blobreclaim.New(change.Target.SHA1, "")
	if err != nil {
		t.Fatal(err)
	}
	// The original target ID is the only reference: it must never be excluded.
	s.db.DeleteMetaData(change.Source.Id)
	if found, err := s.db.HasAnyBlobReference(t.Context(), candidate); err != nil || !found {
		t.Fatal("original target reference was excluded", err)
	}
	file := *change.Target
	file.UserId, file.UploadRequestId, file.ExpireAt, file.PendingDeletion = 2, "synthetic-request", 1, 1
	s.db.SaveMetaData(file)
	if found, err := s.db.HasAnyBlobReference(t.Context(), candidate); err != nil || !found {
		t.Fatal("expired pending cross-owner request reference was excluded", err)
	}
	file.Id, file.SHA1 = "malformed-reference", "./old-blob"
	s.db.SaveMetaData(file)
	if found, err := s.db.HasAnyBlobReference(t.Context(), candidate); found || !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("ambiguous local reference did not fail closed", err)
	}
}

func TestSEC041RedisCandidateSchemaAndImport(t *testing.T) {
	t.Run("migration-bootstrap-only", func(t *testing.T) {
		s := sec030RedisNew(t)
		if err := s.db.CheckEmptyForMigration(t.Context()); err != nil {
			t.Fatal("legal empty candidate marker blocked migration", err)
		}
		candidate, err := blobreclaim.New("migration-only-blob", "")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.db.ImportBlobCandidates(t.Context(), []blobreclaim.Candidate{candidate}); err != nil {
			t.Fatal(err)
		}
		if err := s.db.CheckEmptyForMigration(t.Context()); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatal("candidate-only destination was considered empty", err)
		}
	})
	t.Run("upgrade-preserves-data", func(t *testing.T) {
		s := sec030RedisNew(t)
		file := models.File{Id: "protected-file", UserId: 1, SHA1: "protected-blob", SizeBytes: 1}
		s.db.SaveMetaData(file)
		sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyBlobCandidatesVersion, s.config.RedisPrefix+keyBlobCandidates)
		s.db.SetDbVersion(9)
		if _, err := s.db.ReadBlobCandidates(t.Context()); !errors.Is(err, uploadquota.ErrSchema) || !errors.Is(err, uploadquota.ErrNotFound) {
			t.Fatal("legacy missing structures were not distinguished", err)
		}
		s.db.Upgrade(9)
		s.db.SetDbVersion(DatabaseSchemeVersion)
		if len(sec041Candidates(t, s.db)) != 0 {
			t.Fatal("upgrade invented candidates")
		}
		if got, found := s.db.GetMetaDataById(file.Id); !found || !reflect.DeepEqual(got, file) {
			t.Fatal("upgrade changed existing metadata")
		}
		candidate, err := blobreclaim.New("folder/object", "synthetic-bucket")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.db.ImportBlobCandidates(t.Context(), []blobreclaim.Candidate{candidate}); err != nil {
			t.Fatal("offline import failed", err)
		}
		if err := s.db.ImportBlobCandidates(t.Context(), nil); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatal("import replaced a nonempty destination", err)
		}
		if got := sec041Candidates(t, s.db); len(got) != 1 || got[0] != candidate {
			t.Fatal("import did not retain exact locator/token")
		}
		if err := s.db.CheckEmptyForMigration(t.Context()); !errors.Is(err, uploadquota.ErrConflict) {
			t.Fatal("nonempty migration destination accepted", err)
		}
	})
	for _, damage := range []string{"missing-marker", "missing-hash", "corrupt-record", "wrong-key"} {
		t.Run(damage, func(t *testing.T) {
			s, change := sec041RedisReplacement(t)
			candidate, err := blobreclaim.New("protected-blob", "")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := blobreclaim.Encode(candidate)
			if err != nil {
				t.Fatal(err)
			}
			switch damage {
			case "missing-marker":
				sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyBlobCandidatesVersion)
			case "missing-hash":
				sec030RedisDo(t, s.conn, "DEL", s.config.RedisPrefix+keyBlobCandidates)
			case "corrupt-record":
				sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+keyBlobCandidates, candidate.Key(), "invalid-record")
			case "wrong-key":
				sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+keyBlobCandidates, strings.Repeat("a", 64), encoded)
			}
			if _, err := s.db.ReadBlobCandidates(t.Context()); err == nil {
				t.Fatal("damaged candidate collection was accepted")
			}
			other, err := New(s.config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(other.Close)
			if _, err := other.ReadBlobCandidates(t.Context()); err == nil {
				t.Fatal("ordinary reconnect healed damaged state")
			}
			before := s.db.GetAllMetadata()
			change.DeleteSource = true
			if _, err := s.db.ApplyUploadQuota(t.Context(), change); err == nil {
				t.Fatal("registration corruption did not reject replacement")
			}
			if !reflect.DeepEqual(before, s.db.GetAllMetadata()) {
				t.Fatal("failed registration patched content or expired its source")
			}
		})
	}
}

func TestSEC041RedisCandidateCapacity(t *testing.T) {
	s, change := sec041RedisReplacement(t)
	candidates := make([]blobreclaim.Candidate, blobreclaim.MaxCandidates)
	for i := range candidates {
		candidate, err := blobreclaim.New(fmt.Sprintf("capacity-%04d", i), "")
		if err != nil {
			t.Fatal(err)
		}
		candidates[i] = candidate
	}
	if err := s.db.ImportBlobCandidates(t.Context(), candidates); err != nil {
		t.Fatal("full collection setup failed", err)
	}
	before := s.db.GetAllMetadata()
	revision := sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)
	change.DeleteSource = true
	if _, err := s.db.ApplyUploadQuota(t.Context(), change); !errors.Is(err, uploadquota.ErrFull) {
		t.Fatal("full registration was not rejected", err)
	}
	if !reflect.DeepEqual(before, s.db.GetAllMetadata()) || !reflect.DeepEqual(revision, sec030RedisDo(t, s.conn, "GET", s.config.RedisPrefix+keyMetadataRevision)) {
		t.Fatal("full candidate collection partially patched metadata or revision")
	}
	// At capacity, an already registered locator still has room for its token.
	change.Target.SHA1 = candidates[0].SHA1
	s.db.SaveMetaData(*change.Target)
	if _, err := s.db.ApplyUploadQuota(t.Context(), change); err != nil {
		t.Fatal("deduplication consumed an extra slot", err)
	}
	current := sec041Candidates(t, s.db)
	if len(current) != blobreclaim.MaxCandidates {
		t.Fatal("capacity refresh changed the candidate count")
	}
	got, found, err := s.db.ReadBlobCandidate(t.Context(), candidates[0].Key())
	if err != nil || !found || got.Token == candidates[0].Token {
		t.Fatal("capacity refresh did not replace its token", err)
	}
}

type sec041BlobExecConn struct {
	redigo.Conn
	execs   *atomic.Int32
	before  func(int32) error
	unknown error
}

func (c *sec041BlobExecConn) DoContext(ctx context.Context, command string, args ...any) (any, error) {
	if !strings.EqualFold(command, "EXEC") {
		return redigo.DoContext(c.Conn, ctx, command, args...)
	}
	n := c.execs.Add(1)
	if err := c.before(n); err != nil {
		return nil, err
	}
	reply, err := redigo.DoContext(c.Conn, ctx, command, args...)
	if n == 1 && c.unknown != nil && err == nil && reply != nil {
		return nil, c.unknown
	}
	return reply, err
}
func (c *sec041BlobExecConn) ReceiveContext(ctx context.Context) (any, error) {
	return redigo.ReceiveContext(c.Conn, ctx)
}

func TestSEC041RedisCandidateWatchAndUnknown(t *testing.T) {
	for _, scenario := range []string{"candidate-watch-abort", "owner-changed", "committed-reply-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			s, change := sec041RedisReplacement(t)
			change.DeleteSource = true
			concurrent, err := blobreclaim.New("concurrent-blob", "")
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := blobreclaim.Encode(concurrent)
			if err != nil {
				t.Fatal(err)
			}
			var execs atomic.Int32
			unknown := errors.New("synthetic replacement EXEC outcome unknown")
			before := func(n int32) error {
				if n != 1 {
					return nil
				}
				if scenario == "candidate-watch-abort" {
					_, err := s.conn.Do("HSET", s.config.RedisPrefix+keyBlobCandidates, concurrent.Key(), encoded)
					return err
				}
				if scenario == "owner-changed" {
					_, err := s.conn.Do("HSET", s.config.RedisPrefix+prefixMetaData+change.Target.Id, "UserId", 3)
					return err
				}
				return nil
			}
			p := s.db
			dial := s.db.pool.DialContext
			p.pool = &redigo.Pool{MaxIdle: 1, DialContext: func(ctx context.Context) (redigo.Conn, error) {
				conn, err := dial(ctx)
				if err != nil {
					return nil, err
				}
				wrapped := &sec041BlobExecConn{Conn: conn, execs: &execs, before: before}
				if scenario == "committed-reply-unknown" {
					wrapped.unknown = unknown
				}
				return wrapped, nil
			}}
			t.Cleanup(func() { _ = p.pool.Close() })
			_, applyErr := p.ApplyUploadQuota(t.Context(), change)
			got, found, readErr := s.db.ReadRecoveryFile(t.Context(), change.Target.Id)
			if readErr != nil || !found {
				t.Fatal("final target unreadable", readErr)
			}
			candidates := sec041Candidates(t, s.db)
			switch scenario {
			case "candidate-watch-abort":
				if applyErr != nil || execs.Load() != 2 || len(candidates) != 2 || got.SHA1 != change.Source.SHA1 {
					t.Fatal("candidate WATCH did not revalidate and commit atomically", applyErr)
				}
			case "owner-changed":
				if !errors.Is(applyErr, uploadquota.ErrConflict) || execs.Load() != 1 || len(candidates) != 0 || got.UserId != 3 || got.SHA1 != change.Target.SHA1 {
					t.Fatal("real owner change was ignored or partially registered", applyErr)
				}
			case "committed-reply-unknown":
				if !errors.Is(applyErr, unknown) || execs.Load() != 1 || len(candidates) != 1 || got.SHA1 != change.Source.SHA1 {
					t.Fatal("unknown replacement outcome was retried or claimed rolled back", applyErr)
				}
			}
			source, found, err := s.db.ReadRecoveryFile(t.Context(), change.Source.Id)
			if err != nil || !found {
				t.Fatal("final source unreadable", err)
			}
			if scenario == "owner-changed" {
				if source.ExpireAt != change.Source.ExpireAt || source.UnlimitedTime != change.Source.UnlimitedTime {
					t.Fatal("aborted replacement expired its source")
				}
			} else if source.ExpireAt != 0 || source.UnlimitedTime {
				t.Fatal("committed replacement did not atomically expire its source")
			}
		})
	}
}
