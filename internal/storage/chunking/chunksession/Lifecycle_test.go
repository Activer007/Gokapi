//go:build test && security_chunk

package chunksession

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/storage/chunking"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

type heldReader struct {
	reader  *bytes.Reader
	started chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (r *heldReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.resume })
	return r.reader.Read(p)
}

func TestSEC030ChunkSessionCancelWaitsForWriter(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			input := &heldReader{reader: bytes.NewReader([]byte("abc")), started: make(chan struct{}), resume: make(chan struct{})}
			var resume sync.Once
			defer resume.Do(func() { close(input.resume) })
			done := make(chan error, 1)
			go func() {
				_, err := s.Write(context.Background(), scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: 3}, input, 3, 100, testLimits())
				done <- err
			}()
			select {
			case <-input.started:
			case <-time.After(5 * time.Second):
				t.Fatal("writer never reached real payload read")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := s.Cancel(ctx, scope); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("cancel did not wait for real I/O to stop", err)
			}
			if _, err := os.Stat(s.payloadPath(ID(scope))); err != nil {
				t.Fatal("active writer payload was removed", err)
			}
			if r := testReceipt(t, s, scope); r.StagingReleased {
				t.Fatal("active writer was refunded")
			}
			resume.Do(func() { close(input.resume) })
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), scope); err != nil {
				t.Fatal(err)
			}
			if r := testReceipt(t, s, scope); r.State != uploadquota.Aborted || !r.StagingReleased {
				t.Fatal("stopped writer not cleaned/canceled")
			}
		})
	}
}

func TestSEC030ChunkSessionUncertainCloseRetainsResources(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			calls := 0
			s.closeFile = func(f *os.File) error {
				err := f.Close()
				if strings.HasPrefix(filepath.Base(f.Name()), "payload-") {
					calls++
					return errors.Join(err, syscall.EIO)
				}
				return err
			}
			if _, err := testWrite(s, scope, 3, 0, []byte("abc")); !errors.Is(err, syscall.EIO) {
				t.Fatal("uncertain close was hidden", err)
			}
			if _, err := testWrite(s, scope, 3, 0, []byte("abc")); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("uncertain descriptor was reused", err)
			}
			if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("uncertain descriptor was cleaned in same process", err)
			}
			if calls != 1 || testReceipt(t, s, scope).StagingReleased {
				t.Fatal("uncertain charge/close guard changed")
			}
			if _, err := os.Lstat(s.payloadPath(ID(scope))); err != nil {
				t.Fatal("uncertain owned payload was removed", err)
			}
			// The injected close really closed the OS handle before returning EIO.
			// This fresh Store models process restart, not a live-FD recovery trick.
			restarted := New(s.root)
			restarted.apply, restarted.read = s.apply, s.read
			if err := restarted.Cancel(context.Background(), scope); err != nil {
				t.Fatal("confirmed restart cleanup failed", err)
			}
			if !testReceipt(t, s, scope).StagingReleased {
				t.Fatal("restart cleanup did not refund confirmed resources")
			}
		})
	}
}

func TestSEC030ChunkSessionLeaseCloseErrorIsSticky(t *testing.T) {
	s := testStore(t, "sqlite")
	scope := testScopes()[0]
	if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "sticky-file", testLimits())
	if err != nil {
		t.Fatal(err)
	}
	s.closeFile = func(f *os.File) error { return errors.Join(f.Close(), syscall.EIO) }
	if err := l.CloseInput(); !errors.Is(err, syscall.EIO) {
		t.Fatal(err)
	}
	if err := l.CloseInput(); !errors.Is(err, syscall.EIO) {
		t.Fatal("repeat close erased uncertainty", err)
	}
	l.Release()
	if _, err := os.Stat(s.payloadPath(ID(scope))); err != nil {
		t.Fatal("uncertain close removed payload", err)
	}
	if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
		t.Fatal(err)
	}
	if testReceipt(t, s, scope).StagingReleased {
		t.Fatal("uncertain lease was refunded")
	}
}

func TestSEC030ChunkSessionSweepPreservesUncertainPublication(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			ordinary := testScopes()[0]
			claimed := ordinary
			claimed.ClientID = "synthetic-claimed-expiry"
			committed := ordinary
			committed.ClientID = "synthetic-committed-expiry"
			for _, scope := range []Scope{ordinary, claimed, committed} {
				if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
					t.Fatal(err)
				}
			}
			for _, scope := range []Scope{claimed, committed} {
				l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "file-"+scope.ClientID, testLimits())
				if err != nil {
					t.Fatal(err)
				}
				if scope == committed {
					file := models.File{Id: l.Upload.FileID, Name: "synthetic.bin", UserId: 77, SizeBytes: 3, SHA1: "synthetic"}
					if _, err := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Commit, Upload: l.Upload, File: &file, Now: now, Limits: testLimits()}); err != nil {
						t.Fatal(err)
					}
				}
				if err := l.CloseInput(); err != nil {
					t.Fatal(err)
				}
				l.Release()
			}
			now += uploadquota.MaxLifetime
			if err := s.Sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			if r := testReceipt(t, s, ordinary); r.State != uploadquota.Expired || !r.StagingReleased {
				t.Fatal("expired unclaimed session not cleaned")
			}
			for _, scope := range []Scope{claimed, committed} {
				if r := testReceipt(t, s, scope); r.StagingReleased {
					t.Fatal("missing input was mistaken for publication/work cleanup")
				}
			}
		})
	}
}

func TestSEC030ChunkSessionAbsoluteExpiryAndScope(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[1]
			now := time.Now().Unix()
			s.now = func() int64 { return now }
			initial, err := s.Reserve(context.Background(), scope, testLimits())
			if err != nil {
				t.Fatal(err)
			}
			if initial.ExpiresAt-now != uploadquota.MaxLifetime {
				t.Fatal("new session did not receive absolute 24h expiry")
			}
			now += 5 * 60 // A legitimate file-first reception may exceed the old 4m TTL.
			if _, err := testWrite(s, scope, 6, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			other := scope
			other.RequestID = "another-request"
			if _, err := testWrite(s, other, 6, 3, []byte("bad")); !errors.Is(err, uploadquota.ErrConflict) {
				t.Fatal("same UUID crossed request scope", err)
			}
			now += 60 * 60
			if r, err := testWrite(s, scope, 6, 3, []byte("def")); err != nil || r.ExpiresAt != initial.ExpiresAt {
				t.Fatal("write slid expiry or rejected legitimate completion", err)
			}
			now = initial.ExpiresAt
			if _, err := testWrite(s, scope, 6, 0, []byte("abc")); !errors.Is(err, uploadquota.ErrExpired) {
				t.Fatal("expired retry renewed admission", err)
			}
			if testReceipt(t, s, scope).StagingReleased {
				t.Fatal("expiry alone refunded staging")
			}
		})
	}
}

func TestSEC030ChunkSessionManifestBounds(t *testing.T) {
	hash := sha256.Sum256([]byte("synthetic"))
	digest := hex.EncodeToString(hash[:])
	scope := testScopes()[0]
	// This tests only the bounded progress representation for a 2GiB file;
	// it does not allocate a large sparse file or claim a large-I/O benchmark.
	m := manifest{Version: 1, ID: ID(scope), UserID: 77, Bytes: 2 << 30, Ranges: make([]partReceipt, 2048)}
	for i := range m.Ranges {
		m.Ranges[i] = partReceipt{Offset: int64(i) * (1 << 20), Length: 1 << 20, SHA256: digest}
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeManifest(data)
	if err != nil || !decoded.complete() || len(data) > maxManifestBytes {
		t.Fatal("2GiB/min-chunk progress cannot be represented", err)
	}
	for _, data := range [][]byte{append(data, []byte("{}")...), []byte(`{"Version":1,"Version":1}`), bytes.Repeat([]byte(" "), maxManifestBytes+1)} {
		if _, err := decodeManifest(data); err == nil {
			t.Fatal("malformed/oversized progress accepted")
		}
	}
	m.Ranges = make([]partReceipt, maxRanges+1)
	data, _ = json.Marshal(m)
	if _, err := decodeManifest(data); err == nil {
		t.Fatal("unbounded receipt count accepted")
	}
}

func TestSEC030ChunkSessionCorruptProgressCannotClaim(t *testing.T) {
	s := testStore(t, "sqlite")
	scope := testScopes()[0]
	if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.manifestPath(ID(scope)), []byte(`{"Version":1,"Version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "corrupt-file", testLimits()); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("corrupt progress accepted", err)
	}
	if testReceipt(t, s, scope).Completing {
		t.Fatal("corrupt progress was claimed")
	}
}

func TestSEC030ChunkSessionUnownedRootIsNeverDeleted(t *testing.T) {
	for _, mode := range []string{"missing-marker", "wrong-marker", "unknown-entry"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t, "sqlite")
			if err := os.Mkdir(s.path(""), 0700); err != nil {
				t.Fatal(err)
			}
			marker := markerContents
			if mode == "wrong-marker" {
				marker = "foreign owner\n"
			}
			if mode != "missing-marker" {
				if err := os.WriteFile(s.path(markerName), []byte(marker), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(s.path("foreign-keep"), []byte("must remain"), 0600); err != nil {
				t.Fatal(err)
			}
			input := &countReader{}
			if _, err := s.Write(context.Background(), testScopes()[0], chunking.ChunkInfo{TotalFilesizeBytes: 1}, input, 1, 100, testLimits()); !errors.Is(err, uploadquota.ErrInvalid) {
				t.Fatal("unowned directory was adopted", err)
			}
			if input.reads != 0 {
				t.Fatal("rejected root read input")
			}
			if got, err := os.ReadFile(s.path("foreign-keep")); err != nil || string(got) != "must remain" {
				t.Fatal("unowned entry was deleted", err)
			}
		})
	}
}

func TestSEC030ChunkSessionSpaceCheckpoint(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, mode := range []string{"too-small", "overflow", "probe-error"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				s := testStore(t, backend)
				scope := testScopes()[0]
				probes := 0
				s.freeFloor = func() (int64, error) { return 100, nil }
				s.freeSpace = func(string) (uint64, error) { probes++; return 102, nil }
				want := ErrSpace
				if mode == "overflow" {
					s.freeFloor = func() (int64, error) { return math.MaxInt64, nil }
					want = uploadquota.ErrInvalid
				}
				if mode == "probe-error" {
					s.freeSpace = func(string) (uint64, error) { probes++; return 0, syscall.EIO }
					want = syscall.EIO
				}
				input := &countReader{}
				_, err := s.Write(context.Background(), scope, chunking.ChunkInfo{TotalFilesizeBytes: 3}, input, 3, 100, testLimits())
				if !errors.Is(err, want) || input.reads != 0 {
					t.Fatal("space checkpoint read input or hid failure", err, input.reads)
				}
				if mode == "overflow" && probes != 0 {
					t.Fatal("overflow reached OS probe")
				}
				if mode == "too-small" && !errors.Is(err, uploadquota.ErrStaging) {
					t.Fatal("space failure lost capacity identity")
				}
				if _, err := os.Lstat(s.payloadPath(ID(scope))); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed checkpoint allocated payload", err)
				}
				if r := testReceipt(t, s, scope); !r.BytesKnown || r.Bytes != 3 || r.StagingReleased {
					t.Fatal("probe failure silently refunded admission")
				}
				if err := s.Cancel(context.Background(), scope); err != nil {
					t.Fatal("confirmed empty-resource cancel blocked", err)
				}
			})
		}
	}
}

var _ io.Reader = (*heldReader)(nil)

func TestSEC030ChunkSessionCleanupIdentityIsFixed(t *testing.T) {
	s := testStore(t, "sqlite")
	scope := testScopes()[0]
	other := scope
	other.ClientID = "synthetic-other-upload"
	for _, candidate := range []Scope{scope, other} {
		if _, err := testWrite(s, candidate, 3, 0, []byte("abc")); err != nil {
			t.Fatal(err)
		}
	}
	l, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "fixed-cleanup-file", testLimits())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	foreign, err := os.OpenFile(filepath.Join(t.TempDir(), "unrelated"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	original := l.Input
	closedOriginal := false
	s.closeFile = func(f *os.File) error {
		if f == original {
			closedOriginal = true
		}
		return f.Close()
	}
	// Exported result fields are caller-visible snapshots, not authority to
	// choose the descriptor or path which the lease is allowed to clean.
	l.Upload.ID = ID(other)
	l.Input = foreign
	l.AlreadyCommitted = true
	if err := l.CloseInput(); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Stat(); err == nil || !closedOriginal {
		t.Fatal("fixed original descriptor was not closed", err)
	}
	if _, err := foreign.WriteString("still open"); err != nil {
		t.Fatal("caller-mutated Input closed an unrelated descriptor", err)
	}
	if got, err := os.ReadFile(s.payloadPath(ID(other))); err != nil || string(got) != "abc" {
		t.Fatal("caller-mutated record deleted another session", err)
	}
	if _, err := os.Stat(s.payloadPath(ID(scope))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("fixed owned payload was not cleaned", err)
	}
	if testReceipt(t, s, other).StagingReleased {
		t.Fatal("another session was refunded")
	}
	if err := s.Cancel(context.Background(), other); err != nil {
		t.Fatal(err)
	}
}

func TestSEC030ChunkSessionCancelCompatibility(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[1]
			unknown := scope
			unknown.ClientID = "synthetic-unknown-public-id"
			before, err := s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), unknown); err != nil {
				t.Fatal("unknown public ID was not a no-op", err)
			}
			after, err := s.read(context.Background())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("unknown cancel changed ledger", err)
			}
			if _, err := os.Lstat(s.path("")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("unknown cancel created or touched namespace", err)
			}
			if _, err := s.Reserve(context.Background(), scope, testLimits()); err != nil {
				t.Fatal(err)
			}
			if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			foreign := scope
			foreign.RequestID = "another-request"
			foreign.UserID = 78
			assertUnchanged := func(before uploadquota.Ledger) {
				t.Helper()
				after, err := s.read(context.Background())
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("no-op cancel changed another session's ledger", err)
				}
				data, err := os.ReadFile(s.payloadPath(ID(scope)))
				if err != nil || string(data) != "abc" {
					t.Fatal("no-op cancel removed or changed owned bytes", err)
				}
			}
			before, err = s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), foreign); err != nil {
				t.Fatal("foreign scope was not unknown/no-op", err)
			}
			assertUnchanged(before)
			lease, err := s.Prepare(context.Background(), scope, chunking.FileHeader{Size: 3}, "cancel-compat-file", testLimits())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lease.CloseInput(); lease.Release() }()
			before, err = s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), foreign); err != nil {
				t.Fatal("foreign scope leaked claimed gate state", err)
			}
			assertUnchanged(before)
			if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInProgress) {
				t.Fatal("active completion was falsely canceled", err)
			}
			assertUnchanged(before)
			file := models.File{Id: lease.Upload.FileID, UserId: scope.UserID, UploadRequestId: scope.RequestID, SizeBytes: 3, Name: "synthetic.bin", SHA1: "synthetic-compat-hash"}
			if _, err := s.apply(context.Background(), uploadquota.Change{Action: uploadquota.Commit, Upload: lease.Upload, File: &file, Limits: testLimits(), Now: s.now(), CheckRequestPolicy: true}); err != nil {
				t.Fatal(err)
			}
			before, err = s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), scope); err != nil {
				t.Fatal("committed cancel was not a no-op", err)
			}
			assertUnchanged(before)
			if r := testReceipt(t, s, scope); r.State != uploadquota.Committed || r.StagingReleased {
				t.Fatal("committed-held cancel refunded unknown work")
			}
		})
	}
}

func TestSEC030ChunkSessionCancelDoesNotHideCleanupFailure(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			s := testStore(t, backend)
			scope := testScopes()[0]
			if _, err := testWrite(s, scope, 3, 0, []byte("abc")); err != nil {
				t.Fatal(err)
			}
			before, err := s.read(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.path(markerName), []byte("foreign marker\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.Cancel(context.Background(), scope); !errors.Is(err, uploadquota.ErrInvalid) {
				t.Fatal("known cleanup failure became success", err)
			}
			after, err := s.read(context.Background())
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed cleanup refunded or changed ledger", err)
			}
			if data, err := os.ReadFile(s.payloadPath(ID(scope))); err != nil || string(data) != "abc" {
				t.Fatal("failed validation removed owned payload", err)
			}
			if err := os.WriteFile(s.path(markerName), []byte(markerContents), 0600); err != nil {
				t.Fatal(err)
			}
			read := s.read
			s.read = func(context.Context) (uploadquota.Ledger, error) { return uploadquota.Ledger{}, uploadquota.ErrSchema }
			unknown := scope
			unknown.ClientID = "synthetic-unknown-during-db-failure"
			err = s.Cancel(context.Background(), unknown)
			s.read = read
			if !errors.Is(err, uploadquota.ErrSchema) {
				t.Fatal("read failure was assumed to mean unknown", err)
			}
			if err := s.Cancel(context.Background(), scope); err != nil {
				t.Fatal("confirmed cleanup could not retry", err)
			}
		})
	}
}
