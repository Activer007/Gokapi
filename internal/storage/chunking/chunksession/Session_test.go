//go:build test && security_chunk

package chunksession

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/SupaShare/internal/configuration/database/provider/redis"
	"github.com/Activer007/SupaShare/internal/configuration/database/provider/sqlite"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/chunking"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func testLimits() uploadquota.Limits {
	return uploadquota.Limits{User: uploadquota.Usage{Bytes: 32 << 20, Files: 16}, Request: uploadquota.Usage{Bytes: 32 << 20, Files: 16}, Instance: uploadquota.Usage{Bytes: 64 << 20, Files: 32}, UserActive: 16, RequestActive: 16, InstanceActive: 32, StagingBytes: 256 << 20}
}

func testStore(t *testing.T, backend string) *Store {
	t.Helper()
	s := New(t.TempDir())
	if backend == "redis" {
		p, err := redis.New(models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "session_"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		s.apply, s.read = p.ApplyUploadQuota, p.ReadUploadQuota
		p.SaveFileRequest(models.FileRequest{Id: "synthetic-request", UserId: 77, ApiKey: "synthetic-request-key", MaxFiles: 16, MaxSize: 32})
	} else {
		p, err := sqlite.New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "session.sqlite")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		s.apply, s.read = p.ApplyUploadQuota, p.ReadUploadQuota
		p.SaveFileRequest(models.FileRequest{Id: "synthetic-request", UserId: 77, ApiKey: "synthetic-request-key", MaxFiles: 16, MaxSize: 32})
	}
	return s
}

func testScopes() []Scope {
	return []Scope{{ClientID: "synthetic-client-uuid", UserID: 77}, {ClientID: "synthetic-server-uuid", UserID: 77, RequestID: "synthetic-request"}}
}

func testWrite(s *Store, scope Scope, total, offset int64, payload []byte) (uploadquota.Reservation, error) {
	return s.Write(context.Background(), scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: total, Offset: offset}, bytes.NewReader(payload), int64(len(payload)), 32<<20, testLimits())
}

func testReceipt(t *testing.T, s *Store, scope Scope) uploadquota.Reservation {
	t.Helper()
	ledger, err := s.read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := ledger.Records[ID(scope)]
	if !ok {
		t.Fatal("durable session receipt missing")
	}
	return r
}

func TestSEC030ChunkSessionCoverageAndReceipt(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, scope := range testScopes() {
			t.Run(backend+"/"+scope.RequestID, func(t *testing.T) {
				s := testStore(t, backend)
				if scope.RequestID != "" {
					if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
						t.Fatalf("unbound reserve: %v", err)
					}
				}
				r, err := testWrite(s, scope, 6, 3, []byte("def"))
				if err != nil {
					t.Fatalf("out-of-order first chunk: %v", err)
				}
				if r.Bytes != 6 || r.StagingBytes != 4*6+(1<<20) || !r.BytesKnown {
					t.Fatal("chunk was charged instead of fixed total declaration")
				}
				header := chunking.FileHeader{Filename: "synthetic.bin", ContentType: "application/octet-stream", Size: 6}
				if l, err := s.Prepare(context.Background(), scope, header, "fixed-file", testLimits()); err == nil {
					if l != nil {
						l.Release()
					}
					t.Fatal("preallocated hole was accepted as complete")
				}
				if testReceipt(t, s, scope).Completing {
					t.Fatal("incomplete upload consumed its completion claim")
				}
				if _, err := testWrite(s, scope, 6, 0, []byte("abc")); err != nil {
					t.Fatal(err)
				}
				l, err := s.Prepare(context.Background(), scope, header, "fixed-file", testLimits())
				if err != nil {
					t.Fatalf("complete coverage rejected: %v", err)
				}
				defer func() { _ = l.CloseInput(); l.Release() }()
				got, err := io.ReadAll(l.Input)
				if err != nil || string(got) != "abcdef" {
					t.Fatal("ordered readback changed", string(got), err)
				}
				if !l.Upload.Completing || l.Upload.FileID != "fixed-file" {
					t.Fatal("Prepare returned before durable fixed-ID Claim")
				}
				if second, err := s.Prepare(context.Background(), scope, header, "another-file", testLimits()); !errors.Is(err, uploadquota.ErrInProgress) {
					if second != nil {
						second.Release()
					}
					t.Fatalf("duplicate complete: %v", err)
				}
				file := models.File{Id: l.Upload.FileID, UserId: scope.UserID, UploadRequestId: scope.RequestID, SizeBytes: 6, Name: "synthetic.bin", SHA1: "synthetic-hash"}
				if _, err := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Commit, Upload: l.Upload, File: &file, Limits: testLimits(), Now: time.Now().Unix(), CheckRequestPolicy: scope.RequestID != ""}); err != nil {
					t.Fatal(err)
				}
				if err := l.CloseInput(); err != nil {
					t.Fatal(err)
				}
				if err := l.CloseInput(); err != nil {
					t.Fatal("CloseInput is not idempotent", err)
				}
				if _, err := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.ReleaseStaging, Upload: l.Upload, Now: time.Now().Unix(), CleanupConfirmed: true}); err != nil {
					t.Fatal(err)
				}
				l.Release()
				replay, err := s.Prepare(context.Background(), scope, header, "new-unused-id", testLimits())
				if err != nil {
					t.Fatalf("committed receipt replay: %v", err)
				}
				defer replay.Release()
				if !replay.AlreadyCommitted || replay.Input != nil || replay.Upload.FileID != "fixed-file" {
					t.Fatal("committed receipt regenerated input/identity")
				}
			})
		}
	}
}

func TestSEC030ChunkSessionRetryAndRanges(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			if _, err := testWrite(s, scope, 6, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			if _, err := testWrite(s, scope, 6, 0, []byte("abc")); err != nil {
				t.Fatal("exact retry rejected", err)
			}
			for _, tc := range []struct {
				total, offset int64
				data          string
			}{{6, 0, "XYZ"}, {6, 2, "XYZ"}, {7, 3, "def"}, {6, math.MaxInt64, "x"}, {6, -1, "x"}} {
				if _, err := testWrite(s, scope, tc.total, tc.offset, []byte(tc.data)); err == nil {
					t.Fatal("conflicting range/declaration accepted", tc)
				}
			}
			if _, err := testWrite(s, scope, 6, 3, []byte("def")); err != nil {
				t.Fatal(err)
			}
			l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 6}, "range-file", testLimits())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = l.CloseInput(); l.Release() }()
			got, err := io.ReadAll(l.Input)
			if err != nil || string(got) != "abcdef" {
				t.Fatal("confirmed data was modified by rejected retry", string(got), err)
			}
			if r := testReceipt(t, s, scope); r.Bytes != 6 {
				t.Fatal("same upload charged more than once")
			}
		})
	}
}

func TestSEC030ChunkSessionConcurrentNonOverlapping(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, scope := range testScopes() {
			for _, n := range []int{10, 50} {
				t.Run(backend+"/"+scope.RequestID+"/"+strconv.Itoa(n), func(t *testing.T) {
					s := testStore(t, backend)
					if scope.RequestID != "" {
						if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
							t.Fatal(err)
						}
					}
					start := make(chan struct{})
					errs := make(chan error, n)
					var wg sync.WaitGroup
					for i := range n {
						wg.Add(1)
						go func() {
							defer wg.Done()
							<-start
							_, err := testWrite(s, scope, int64(n), int64(n-i-1), []byte{byte(n - i - 1)})
							errs <- err
						}()
					}
					close(start)
					wg.Wait()
					close(errs)
					for err := range errs {
						if err != nil {
							t.Fatal("legal concurrent non-overlap rejected", err)
						}
					}
					l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: int64(n)}, "parallel-file", testLimits())
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = l.CloseInput(); l.Release() }()
					got, err := io.ReadAll(l.Input)
					if err != nil || len(got) != n {
						t.Fatal(err, len(got))
					}
					for i, v := range got {
						if v != byte(i) {
							t.Fatal("concurrent chunk bytes changed", i, v)
						}
					}
				})
			}
		}
	}
}

func TestSEC030ChunkSessionEmptyAndCancel(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			if _, err := testWrite(s, scope, 0, 0, nil); err != nil {
				t.Fatal(err)
			}
			l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 0}, "empty-file", testLimits())
			if err != nil {
				t.Fatal(err)
			}
			if got, err := io.ReadAll(l.Input); err != nil || len(got) != 0 {
				t.Fatal("zero-byte upload changed", err)
			}
			if err := l.CloseInput(); err != nil {
				t.Fatal(err)
			}
			l.Release()
			if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("uncertain claimed upload refunded", err)
			}
			other := scope
			other.ClientID = "synthetic-cancel-uuid"
			if _, err := testWrite(s, other, 3, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), other); err != nil {
				t.Fatal(err)
			}
			if r := testReceipt(t, s, other); r.State != uploadquota.Aborted || !r.StagingReleased {
				t.Fatal("confirmed cancel not durably refunded")
			}
			if _, err := testWrite(s, other, 3, 0, []byte("abc")); err == nil {
				t.Fatal("canceled UUID resurrected")
			}
		})
	}
}

type countReader struct{ reads int }

func (r *countReader) Read(p []byte) (int, error) { r.reads++; return 0, io.EOF }

func TestSEC030ChunkSessionAdmissionAndExactLength(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			limits := testLimits()
			limits.StagingBytes = 1 << 20
			input := &countReader{}
			_, err := s.Write(context.Background(), scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 6}, input, 3, 100, limits)
			if !errors.Is(err, uploadquota.ErrStaging) || !errors.Is(err, uploadquota.ErrQuota) || input.reads != 0 {
				t.Fatal("staging rejection read payload or lost quota identity", err, input.reads)
			}
			if _, err := os.Stat(s.payloadPath(ID(scope))); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("admission allocated payload", err)
			}
			if _, err := testWrite(s, scope, 6, 3, []byte("def")); err != nil {
				t.Fatal(err)
			}
			// Extra bytes must not be copied into the adjacent confirmed tail.
			if _, err := s.Write(context.Background(), scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 6, Offset: 0}, bytes.NewBufferString("abcX"), 3, 100, testLimits()); err == nil {
				t.Fatal("oversized actual input accepted")
			}
			if _, err := s.Write(context.Background(), scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 6, Offset: 0}, bytes.NewBufferString("ab"), 3, 100, testLimits()); err == nil {
				t.Fatal("short actual input accepted")
			}
			if _, err := testWrite(s, scope, 6, 0, []byte("abc")); err != nil {
				t.Fatal("unconfirmed range could not be retried", err)
			}
			l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 6}, "exact-file", testLimits())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = l.CloseInput(); l.Release() }()
			got, err := io.ReadAll(l.Input)
			if err != nil || string(got) != "abcdef" {
				t.Fatal("length error modified confirmed range", string(got), err)
			}
		})
	}
}

func TestSEC030ChunkSessionCancellationAndRestartClaim(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "claimed-file", testLimits())
			if err != nil {
				t.Fatal(err)
			}
			input := &countReader{}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := s.Write(ctx, scope, chunking.ChunkInfo{TotalFilesizeBytes: 3}, input, 3, 100, testLimits()); !errors.Is(err, context.Canceled) || input.reads != 0 {
				t.Fatal("canceled caller read/wrote", err)
			}
			if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("live claimed lease did not reject cancellation immediately", err)
			}
			if err := l.CloseInput(); err != nil {
				t.Fatal(err)
			}
			l.Release()
			// This fresh Store models a new process only after the old descriptor
			// has closed. Missing payload must never clear an uncertain Claim.
			restarted := New(s.root)
			restarted.apply, restarted.read = s.apply, s.read
			if _, err := restarted.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "another-file", testLimits()); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("restart regenerated claimed file", err)
			}
			if err := restarted.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("restart refunded uncertain claim", err)
			}
			r := testReceipt(t, s, scope)
			if !r.Completing || r.StagingReleased || r.FileID != "claimed-file" {
				t.Fatal("claimed capacity/identity lost")
			}
		})
	}
}

func TestSEC030ChunkSessionConfirmedRetryIsImmutable(t *testing.T) {
	s := testStore(t, "sqlite")
	scope := testScopes()[0]
	if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.manifestPath(ID(scope)))
	if err != nil {
		t.Fatal(err)
	}
	dataBefore, err := os.ReadFile(s.payloadPath(ID(scope)))
	if err != nil {
		t.Fatal(err)
	}
	rBefore := testReceipt(t, s, scope)
	if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if _, err := testWrite(s, scope, 3, 0, []byte("bad")); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatal(err)
	}
	after, err := os.ReadFile(s.manifestPath(ID(scope)))
	if err != nil {
		t.Fatal(err)
	}
	dataAfter, err := os.ReadFile(s.payloadPath(ID(scope)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(dataBefore, dataAfter) || !reflect.DeepEqual(rBefore, testReceipt(t, s, scope)) {
		t.Fatal("retry modified confirmed bytes, progress or accounting")
	}
}
