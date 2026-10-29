//go:build test

package securityfixture_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisdb "github.com/Activer007/Gokapi/internal/configuration/database/provider/redis"
	"github.com/Activer007/Gokapi/internal/configuration/database/provider/sqlite"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// These are provider contracts over synthetic metadata. They do not exercise
// HTTP uploads, reserve real disk space, or prove runtime upload enforcement.
type sec030QuotaStore interface {
	ApplyUploadQuota(context.Context, uploadquota.Change) (uploadquota.Reservation, error)
	ReadUploadQuota(context.Context) (uploadquota.Ledger, error)
	SaveMetaData(models.File)
	GetAllMetadata() map[string]models.File
	Close()
}

const sec030Now int64 = 1000

func sec030StoreFactory(t *testing.T, backend string) func() sec030QuotaStore {
	t.Helper()
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "quota.sqlite")}
	if backend == "redis" {
		config = models.DbConnection{HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "sec030_" + rand.Text() + "_"}
	}
	return func() sec030QuotaStore {
		t.Helper()
		var db sec030QuotaStore
		var err error
		if backend == "redis" {
			db, err = redisdb.New(config)
		} else {
			db, err = sqlite.New(config)
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		return db
	}
}

func sec030Limits() uploadquota.Limits {
	return uploadquota.Limits{
		User:       uploadquota.Usage{Bytes: 1000000, Files: 1000},
		Request:    uploadquota.Usage{Bytes: 1000000, Files: 1000},
		Instance:   uploadquota.Usage{Bytes: 1000000, Files: 1000},
		UserActive: 1000, RequestActive: 1000, InstanceActive: 1000,
		StagingBytes: 1000000,
	}
}

func sec030Upload(id string, bytes int64) uploadquota.Reservation {
	return uploadquota.Reservation{ID: id, UserID: 1, RequestID: "request", BytesKnown: true,
		Bytes: bytes, StagingBytes: bytes, ExpiresAt: sec030Now + 3600}
}

func sec030Change(action uploadquota.Action, upload uploadquota.Reservation) uploadquota.Change {
	return uploadquota.Change{Action: action, Upload: upload, Limits: sec030Limits(), Now: sec030Now}
}

func sec030Apply(t *testing.T, db sec030QuotaStore, change uploadquota.Change) uploadquota.Reservation {
	t.Helper()
	got, err := db.ApplyUploadQuota(context.Background(), change)
	if err != nil {
		t.Fatalf("%s failed: %v", change.Action, err)
	}
	return got
}

func sec030Ledger(t *testing.T, db sec030QuotaStore) uploadquota.Ledger {
	t.Helper()
	got, err := db.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sec030RejectUnchanged(t *testing.T, db sec030QuotaStore, change uploadquota.Change, want error) {
	t.Helper()
	beforeLedger := sec030Ledger(t, db)
	beforeFiles := db.GetAllMetadata()
	_, err := db.ApplyUploadQuota(context.Background(), change)
	if err == nil || (want != nil && !errors.Is(err, want)) {
		t.Fatalf("%s error = %v; want %v", change.Action, err, want)
	}
	if !reflect.DeepEqual(beforeLedger, sec030Ledger(t, db)) || !reflect.DeepEqual(beforeFiles, db.GetAllMetadata()) {
		t.Fatalf("rejected %s changed ledger or metadata", change.Action)
	}
}

func TestSEC030DurableLastCapacity(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			for _, contenders := range []int{10, 50} {
				for _, dimension := range []string{"user-bytes", "request-bytes", "instance-bytes", "user-files", "request-files", "instance-files", "user-active", "request-active", "instance-active", "staging"} {
					t.Run(fmt.Sprintf("%s/%d", dimension, contenders), func(t *testing.T) {
						open := sec030StoreFactory(t, backend)
						observer := open()
						limits := sec030Limits()
						switch dimension {
						case "user-bytes":
							limits.User.Bytes = 1
						case "request-bytes":
							limits.Request.Bytes = 1
						case "instance-bytes":
							limits.Instance.Bytes = 1
						case "user-files":
							limits.User.Files = 1
						case "request-files":
							limits.Request.Files = 1
						case "instance-files":
							limits.Instance.Files = 1
						case "user-active":
							limits.UserActive = 1
						case "request-active":
							limits.RequestActive = 1
						case "instance-active":
							limits.InstanceActive = 1
						case "staging":
							limits.StagingBytes = 1
						}
						// Each contender has its own provider/pool, with a common DB.
						dbs := make([]sec030QuotaStore, contenders)
						for i := range dbs {
							dbs[i] = open()
						}
						start := make(chan struct{})
						outcomes := make(chan error, contenders)
						var wg sync.WaitGroup
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						for i, db := range dbs {
							wg.Add(1)
							go func(i int, db sec030QuotaStore) {
								defer wg.Done()
								<-start
								change := sec030Change(uploadquota.Reserve, sec030Upload(fmt.Sprintf("upload-%d", i), 1))
								change.Limits = limits
								_, err := db.ApplyUploadQuota(ctx, change)
								outcomes <- err
							}(i, db)
						}
						close(start)
						wg.Wait()
						close(outcomes)
						successes := 0
						for err := range outcomes {
							if err == nil {
								successes++
								continue
							}
							if !errors.Is(err, uploadquota.ErrQuota) && !errors.Is(err, uploadquota.ErrRetry) {
								t.Errorf("unexpected contention error: %v", err)
							}
						}
						ledger := sec030Ledger(t, observer)
						if successes != 1 || len(ledger.Records) != 1 || len(observer.GetAllMetadata()) != 0 {
							t.Fatalf("capacity oversold or corrupted: successes=%d records=%d files=%d", successes, len(ledger.Records), len(observer.GetAllMetadata()))
						}
						for _, winner := range ledger.Records {
							abort := sec030Change(uploadquota.Abort, winner)
							abort.Terminal, abort.CleanupConfirmed = uploadquota.Aborted, true
							sec030Apply(t, observer, abort)
						}
						retry := sec030Change(uploadquota.Reserve, sec030Upload("after-cleanup", 1))
						retry.Limits = limits
						sec030Apply(t, observer, retry)
					})
				}
			}
		})
	}
}

func TestSEC030DurableScopesAndMetadata(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			for _, dimension := range []string{"user", "request", "instance"} {
				t.Run(dimension, func(t *testing.T) {
					db := sec030StoreFactory(t, backend)()
					// Shared hashes remain separate logical charges. Expired and
					// pending metadata remains charged until actually removed.
					for _, file := range []models.File{
						{Id: "first", UserId: 1, UploadRequestId: "request", SizeBytes: 2, SHA1: "shared", PendingDeletion: 1},
						{Id: "copy", UserId: 1, UploadRequestId: "other-request", SizeBytes: 2, SHA1: "shared", ExpireAt: 1},
						{Id: "other-owner", UserId: 2, UploadRequestId: "request", SizeBytes: 3, SHA1: "shared"},
					} {
						db.SaveMetaData(file)
					}
					change := sec030Change(uploadquota.Reserve, sec030Upload("bounded", 1))
					switch dimension {
					case "user":
						change.Limits.User.Bytes = 4
					case "request":
						change.Limits.Request.Bytes = 5
					case "instance":
						change.Limits.Instance.Bytes = 7
					}
					sec030RejectUnchanged(t, db, change, uploadquota.ErrQuota)
					change = sec030Change(uploadquota.Reserve, sec030Upload("fits", 1))
					change.Limits.User.Bytes, change.Limits.Request.Bytes, change.Limits.Instance.Bytes = 5, 6, 8
					sec030Apply(t, db, change)
				})
			}
		})
	}
}

func TestSEC030DurableLifecycle(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			t.Run("identity-and-zero-binding", func(t *testing.T) {
				db := sec030StoreFactory(t, backend)()
				upload := sec030Upload("stable", 0)
				upload.BytesKnown = false
				first := sec030Apply(t, db, sec030Change(uploadquota.Reserve, upload))
				second := sec030Apply(t, db, sec030Change(uploadquota.Reserve, upload))
				if !reflect.DeepEqual(first, second) || len(sec030Ledger(t, db).Records) != 1 {
					t.Fatal("idempotent reserve changed reservation or added charge")
				}
				for _, mutate := range []func(*uploadquota.Reservation){
					func(u *uploadquota.Reservation) { u.UserID++ },
					func(u *uploadquota.Reservation) { u.RequestID = "other-request" },
					func(u *uploadquota.Reservation) { u.BytesKnown, u.Bytes = true, 1 },
				} {
					conflict := upload
					mutate(&conflict)
					sec030RejectUnchanged(t, db, sec030Change(uploadquota.Reserve, conflict), uploadquota.ErrConflict)
				}
				bound := upload
				bound.BytesKnown = true
				got := sec030Apply(t, db, sec030Change(uploadquota.Bind, bound))
				if !got.BytesKnown || got.Bytes != 0 {
					t.Fatal("zero-byte upload remained unbound")
				}
				sec030Apply(t, db, sec030Change(uploadquota.Bind, bound))
				bound.Bytes, bound.StagingBytes = 1, 1
				sec030RejectUnchanged(t, db, sec030Change(uploadquota.Bind, bound), uploadquota.ErrConflict)
			})
			t.Run("bind-rechecks-capacity", func(t *testing.T) {
				db := sec030StoreFactory(t, backend)()
				upload := sec030Upload("late-size", 0)
				upload.BytesKnown = false
				sec030Apply(t, db, sec030Change(uploadquota.Reserve, upload))
				upload.BytesKnown, upload.Bytes, upload.StagingBytes = true, 2, 2
				bind := sec030Change(uploadquota.Bind, upload)
				bind.Limits.User.Bytes = 1
				sec030RejectUnchanged(t, db, bind, uploadquota.ErrQuota)
				bind.Limits.User.Bytes = 2
				sec030Apply(t, db, bind)
			})
			t.Run("commit-and-replay", func(t *testing.T) {
				db := sec030StoreFactory(t, backend)()
				upload := sec030Upload("complete", 3)
				sec030Apply(t, db, sec030Change(uploadquota.Reserve, upload))
				claim := sec030Change(uploadquota.Claim, upload)
				claim.FileID = "final-file"
				got := sec030Apply(t, db, claim)
				if !got.Completing || got.FileID != claim.FileID {
					t.Fatal("completion claim was not persisted")
				}
				sec030RejectUnchanged(t, db, claim, uploadquota.ErrInProgress)
				other := sec030Upload("other-upload", 3)
				sec030Apply(t, db, sec030Change(uploadquota.Reserve, other))
				otherClaim := sec030Change(uploadquota.Claim, other)
				otherClaim.FileID = claim.FileID
				sec030RejectUnchanged(t, db, otherClaim, uploadquota.ErrConflict)
				file := models.File{Id: claim.FileID, UserId: upload.UserID, UploadRequestId: upload.RequestID, SizeBytes: upload.Bytes, SHA1: "synthetic-blob"}
				commit := sec030Change(uploadquota.Commit, upload)
				commit.FileID, commit.File = claim.FileID, &file
				for _, mutate := range []func(*models.File){
					func(f *models.File) { f.UserId++ },
					func(f *models.File) { f.UploadRequestId = "other-request" },
					func(f *models.File) { f.SizeBytes++ },
					func(f *models.File) { f.Id = "wrong-id" },
				} {
					bad := file
					mutate(&bad)
					invalid := commit
					invalid.File = &bad
					sec030RejectUnchanged(t, db, invalid, nil)
				}
				got = sec030Apply(t, db, commit)
				if got.State != uploadquota.Committed || got.CommitDigest == "" || got.StagingReleased {
					t.Fatal("commit lost state, receipt digest or outstanding staging charge")
				}
				if actual := db.GetAllMetadata(); len(actual) != 1 || !reflect.DeepEqual(actual[file.Id], file) {
					t.Fatal("metadata and committed record were not persisted together")
				}
				before := sec030Ledger(t, db)
				sec030Apply(t, db, commit)
				if !reflect.DeepEqual(before, sec030Ledger(t, db)) || len(db.GetAllMetadata()) != 1 {
					t.Fatal("commit replay changed receipt or duplicated metadata")
				}
				changedFile := file
				changedFile.Name = "changed-replay"
				changed := commit
				changed.File = &changedFile
				sec030RejectUnchanged(t, db, changed, uploadquota.ErrConflict)
				release := sec030Change(uploadquota.ReleaseStaging, upload)
				sec030RejectUnchanged(t, db, release, nil)
				release.CleanupConfirmed = true
				got = sec030Apply(t, db, release)
				if !got.StagingReleased {
					t.Fatal("staging release was not acknowledged")
				}
				sec030Apply(t, db, release)
				next := sec030Change(uploadquota.Reserve, sec030Upload("must-still-charge-metadata", 1))
				next.Limits.User.Bytes = file.SizeBytes
				sec030RejectUnchanged(t, db, next, uploadquota.ErrQuota)
			})
			t.Run("expired-retains-budget-until-cleanup", func(t *testing.T) {
				open := sec030StoreFactory(t, backend)
				db := open()
				upload := sec030Upload("expired", 1)
				reserve := sec030Change(uploadquota.Reserve, upload)
				reserve.Limits.UserActive = 1
				sec030Apply(t, db, reserve)
				// Reopen from an independent provider rather than inspecting only
				// the first provider's process-local state.
				db.Close()
				db = open()
				if len(sec030Ledger(t, db).Records) != 1 {
					t.Fatal("reopen lost reservation")
				}
				late := reserve
				late.Upload.ID, late.Now = "new-upload", upload.ExpiresAt+1
				late.Upload.ExpiresAt = late.Now + 3600
				sec030RejectUnchanged(t, db, late, uploadquota.ErrQuota)
				claim := sec030Change(uploadquota.Claim, upload)
				claim.Now, claim.FileID = late.Now, "too-late"
				sec030RejectUnchanged(t, db, claim, uploadquota.ErrExpired)
				abort := sec030Change(uploadquota.Abort, upload)
				abort.Now, abort.Terminal = late.Now, uploadquota.Expired
				sec030RejectUnchanged(t, db, abort, nil)
				abort.CleanupConfirmed = true
				got := sec030Apply(t, db, abort)
				if got.State != uploadquota.Expired || !got.StagingReleased {
					t.Fatal("expiry cleanup did not release capacity")
				}
				sec030Apply(t, db, abort)
				sec030Apply(t, db, late)
			})
			t.Run("abort-and-cancelled-context", func(t *testing.T) {
				db := sec030StoreFactory(t, backend)()
				upload := sec030Upload("cancel", 1)
				change := sec030Change(uploadquota.Reserve, upload)
				before := sec030Ledger(t, db)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if _, err := db.ApplyUploadQuota(ctx, change); !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled command error = %v", err)
				}
				if !reflect.DeepEqual(before, sec030Ledger(t, db)) || len(db.GetAllMetadata()) != 0 {
					t.Fatal("cancelled reserve mutated DB")
				}
				sec030Apply(t, db, change)
				abort := sec030Change(uploadquota.Abort, upload)
				abort.Terminal = uploadquota.Aborted
				sec030RejectUnchanged(t, db, abort, nil)
				abort.CleanupConfirmed = true
				got := sec030Apply(t, db, abort)
				if got.State != uploadquota.Aborted || !got.StagingReleased {
					t.Fatal("abort acknowledgement was not persisted")
				}
				sec030Apply(t, db, abort)
				claim := sec030Change(uploadquota.Claim, upload)
				claim.FileID = "aborted-file"
				sec030RejectUnchanged(t, db, claim, nil)
			})
		})
	}
}
