//go:build test && security_whole

package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/filesystem"
	"github.com/Activer007/SupaShare/internal/storage/filesystem/s3filesystem/aws"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

const wholeTestOwner = 77

func wholeTestLimits() uploadquota.Limits {
	return uploadquota.Limits{
		User:       uploadquota.Usage{Bytes: 64 << 20, Files: 8},
		Request:    uploadquota.Usage{Bytes: 64 << 20, Files: 8},
		Instance:   uploadquota.Usage{Bytes: 128 << 20, Files: 32},
		UserActive: 8, RequestActive: 8, InstanceActive: 16, StagingBytes: 512 << 20,
	}
}

// These cases run serially: the existing storage package owns one global DB and
// configuration. Both the temporary provider and DataDir are replaced/restored
// together, and no production or existing fixture data is used for uploads.
func wholeTestStore(t *testing.T, backend string) string {
	t.Helper()
	saved := *configuration.Get()
	previous, err := database.ParseUrl(saved.DatabaseUrl, true)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	connection := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "whole.sqlite")}
	if backend == "redis" {
		connection = models.DbConnection{Type: 1, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "whole_"}
	}
	database.Close()
	database.Connect(connection)
	t.Cleanup(func() {
		database.Close()
		*configuration.Get() = saved
		database.Connect(previous)
		filesystem.Init(saved.DataDir)
	})
	cfg := configuration.Get()
	cfg.DataDir, cfg.MaxFileSizeMB, cfg.MaxMemory = dir, 32, 1
	cfg.DatabaseUrl = "sqlite://" + connection.HostUrl
	if backend == "redis" {
		cfg.DatabaseUrl = "redis://" + connection.HostUrl + "?prefix=" + connection.RedisPrefix
	}
	cfg.Encryption.Level = encryption.NoEncryption
	cfg.Authentication.SaltFiles = "synthetic-whole-quota-file-salt"
	filesystem.Init(dir)
	if aws.IsAvailable() {
		t.Fatal("whole-file local contract requires an isolated local storage configuration")
	}
	database.SaveUser(models.User{Id: wholeTestOwner, Name: "Synthetic quota owner", UserLevel: models.UserLevelUser}, false)
	return dir
}

func wholeTestInput(t *testing.T, payload []byte, filename, contentType string) (*os.File, *multipart.FileHeader, func() error, *int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "received-spool")
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close(); _ = os.Remove(path) })
	h := make(textproto.MIMEHeader)
	h.Set("Content-Type", contentType)
	header := &multipart.FileHeader{Filename: filename, Header: h, Size: int64(len(payload))}
	calls := new(int)
	closeInput := func() error {
		*calls = *calls + 1
		if err := file.Close(); err != nil {
			return err
		}
		return os.Remove(path)
	}
	return file, header, closeInput, calls
}

func wholeTestParams() models.UploadParameters {
	return models.UploadParameters{UnlimitedTime: true, UnlimitedDownload: true}
}

func TestSEC030WholeReservedHappy(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, mode := range []string{"plain", "zero", "server-encrypted", "e2e", "e2e-without-server-policy", "hotlink"} {
			t.Run(backend+"/"+mode, func(t *testing.T) {
				dir := wholeTestStore(t, backend)
				payload := []byte("synthetic whole-file payload")
				params := wholeTestParams()
				filename, contentType := "payload.bin", "application/octet-stream"
				switch mode {
				case "zero":
					payload = nil
				case "server-encrypted":
					payload = bytes.Repeat([]byte{0x41}, 2<<20)
					configuration.Get().Encryption = models.Encryption{Level: encryption.LocalEncryptionStored, Cipher: bytes.Repeat([]byte{0x47}, 32)}
					encryption.Init(*configuration.Get())
				case "e2e":
					configuration.Get().Encryption.Level = encryption.EndToEndEncryption
					params.IsEndToEndEncrypted, params.RealSize = true, math.MaxInt64
				case "e2e-without-server-policy":
					params.IsEndToEndEncrypted, params.RealSize = true, math.MaxInt64
				case "hotlink":
					filename, contentType = "synthetic.png", "image/png"
				}
				input, header, closeInput, closeCalls := wholeTestInput(t, payload, filename, contentType)
				file, err := NewFileReserved(context.Background(), input, header, wholeTestOwner, params, wholeTestLimits(), closeInput)
				if err != nil {
					t.Fatalf("reserved upload failed: %v", err)
				}
				if file.Id == "" || file.UserId != wholeTestOwner || file.UploadRequestId != "" || file.SizeBytes != int64(len(payload)) {
					t.Fatal("committed metadata lost the fixed owner/request/actual size")
				}
				stored, ok := database.GetMetaDataById(file.Id)
				if !ok || !reflect.DeepEqual(stored, file) || len(database.GetAllMetadata()) != 1 {
					t.Fatal("completed metadata was not committed exactly once")
				}
				ledger, err := database.ReadUploadQuota(context.Background())
				if err != nil || len(ledger.Records) != 1 {
					t.Fatalf("completion receipt missing: %v", err)
				}
				for _, record := range ledger.Records {
					if record.State != uploadquota.Committed || !record.StagingReleased || record.Completing || record.FileID != file.Id || record.UserID != wholeTestOwner || record.RequestID != "" || record.Bytes != int64(len(payload)) {
						t.Fatal("completed upload did not release confirmed staging or preserve identity")
					}
				}
				if *closeCalls != 1 {
					t.Fatal("input spool cleanup must be confirmed exactly once")
				}
				blob, err := os.Open(filepath.Join(dir, file.SHA1))
				if err != nil {
					t.Fatal(err)
				}
				defer blob.Close()
				var content bytes.Buffer
				if mode == "server-encrypted" {
					if !file.Encryption.IsEncrypted || file.Encryption.IsEndToEndEncrypted {
						t.Fatal("server encryption flags changed")
					}
					err = encryption.DecryptReader(file.Encryption, blob, &content)
				} else {
					_, err = io.Copy(&content, blob)
				}
				if err != nil || !bytes.Equal(content.Bytes(), payload) {
					t.Fatal("committed file bytes cannot be read back", err)
				}
				if strings.HasPrefix(mode, "e2e") && (!file.Encryption.IsEndToEndEncrypted || !file.Encryption.IsEncrypted) {
					t.Fatal("E2E flags were overwritten or claimed plaintext was charged")
				}
				if mode == "hotlink" {
					if linked, ok := database.GetHotlink(file.HotlinkId); file.HotlinkId == "" || !ok || linked != file.Id {
						t.Fatal("hotlink was not published with the committed metadata")
					}
				}
			})
		}
	}
}

func TestSEC030WholeReservedCommitFailureConservesSharedBlob(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, after := range []bool{false, true} {
			name := "before-commit"
			if after {
				name = "reply-lost-after-commit"
			}
			t.Run(backend+"/"+name, func(t *testing.T) {
				dir := wholeTestStore(t, backend)
				payload := []byte("existing shared canonical bytes")
				hash := sha1.Sum(payload)
				locator := hex.EncodeToString(hash[:])
				path := filepath.Join(dir, locator)
				if err := os.WriteFile(path, payload, 0600); err != nil {
					t.Fatal(err)
				}
				database.SaveMetaData(models.File{Id: "existing-file", UserId: wholeTestOwner, SHA1: locator, SizeBytes: int64(len(payload)), Name: "existing.bin", UnlimitedTime: true})
				input, header, closeInput, _ := wholeTestInput(t, payload, "candidate.png", "image/png")
				injected := errors.New("synthetic quota commit transport failure")
				commitSeen := false
				var claimedID string
				apply := func(ctx context.Context, change uploadquota.Change) (uploadquota.Reservation, error) {
					if change.Action == uploadquota.Claim {
						claimedID = change.FileID
					}
					if change.Action != uploadquota.Commit {
						return database.ApplyUploadQuota(ctx, change)
					}
					commitSeen = true
					if len(database.GetAllMetadata()) != 1 {
						t.Error("raw metadata was saved before atomic Commit")
					}
					if change.File == nil {
						t.Fatal("Commit omitted metadata")
					}
					if change.File.Id != claimedID || change.File.UserId != wholeTestOwner || change.File.UploadRequestId != "" || change.File.SizeBytes != int64(len(payload)) {
						t.Error("Commit did not use its claimed fixed identity and observed size")
					}
					if change.File.HotlinkId == "" {
						t.Error("hotlink candidate was not part of the committed digest")
					}
					if _, ok := database.GetHotlink(change.File.HotlinkId); ok {
						t.Error("hotlink was published before Commit")
					}
					if !after {
						return uploadquota.Reservation{}, injected
					}
					record, err := database.ApplyUploadQuota(ctx, change)
					if err != nil {
						return record, err
					}
					if linked, ok := database.GetHotlink(change.File.HotlinkId); !ok || linked != change.File.Id {
						t.Error("metadata Commit did not atomically publish its hotlink")
					}
					return record, injected
				}
				file, err := newFileReserved(context.Background(), input, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput, apply)
				if !errors.Is(err, injected) || file.Id != "" || !commitSeen {
					t.Error("uncertain Commit was reported as success or bypassed the atomic callback", err)
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatal("failed upload removed or changed another metadata's shared blob", err)
				}
				wantMetadata := 1
				if after {
					wantMetadata = 2
				}
				if len(database.GetAllMetadata()) != wantMetadata {
					t.Fatal("failed completion performed an extra raw metadata save")
				}
				ledger, err := database.ReadUploadQuota(context.Background())
				if err != nil || len(ledger.Records) != 1 {
					t.Fatal("failed completion lost its reservation", err)
				}
				for _, record := range ledger.Records {
					wantState := uploadquota.Uploading
					if after {
						wantState = uploadquota.Committed
					}
					if record.State != wantState || record.StagingReleased || record.FileID != claimedID {
						t.Fatal("failed/uncertain completion refunded unconfirmed staging")
					}
				}
			})
		}
	}
}

func TestSEC030WholeReservedRejectsObservedSizeMismatch(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			wholeTestStore(t, backend)
			input, header, closeInput, _ := wholeTestInput(t, []byte("actual bytes"), "size.bin", "application/octet-stream")
			header.Size++
			file, err := NewFileReserved(context.Background(), input, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput)
			if err == nil || file.Id != "" || len(database.GetAllMetadata()) != 0 {
				t.Fatal("declared size replaced actual input size")
			}
		})
	}
}

func TestSEC030WholeReservedRejectsMixedEncryption(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := wholeTestStore(t, backend)
			configuration.Get().Encryption = models.Encryption{Level: encryption.FullEncryptionStored, Cipher: bytes.Repeat([]byte{0x47}, 32)}
			encryption.Init(*configuration.Get())
			input, header, closeInput, calls := wholeTestInput(t, []byte("synthetic cipher bytes"), "mixed.bin", "application/octet-stream")
			params := wholeTestParams()
			params.IsEndToEndEncrypted = true
			file, err := NewFileReserved(context.Background(), input, header, wholeTestOwner, params, wholeTestLimits(), closeInput)
			if !errors.Is(err, uploadquota.ErrInvalid) || file.Id != "" || *calls != 1 {
				t.Fatal("client E2E flag bypassed server encryption or input cleanup", err)
			}
			if len(database.GetAllMetadata()) != 0 {
				t.Fatal("mixed encryption published metadata")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("mixed encryption published a blob/workfile", err)
			}
			ledger, err := database.ReadUploadQuota(context.Background())
			if err != nil || len(ledger.Records) != 0 {
				t.Fatal("invalid encryption allocated durable state", err)
			}
		})
	}
}

func TestSEC030WholeReservedInputCloseFailureKeepsStaging(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			wholeTestStore(t, backend)
			input, header, cleanup, calls := wholeTestInput(t, []byte("committed before uncertain close"), "close.bin", "application/octet-stream")
			closeInput := func() error { return errors.Join(cleanup(), syscall.EIO) }
			file, err := NewFileReserved(context.Background(), input, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput)
			if !errors.Is(err, syscall.EIO) || file.Id != "" || *calls != 1 {
				t.Fatal("uncertain input close reported success", err)
			}
			ledger, err := database.ReadUploadQuota(context.Background())
			if err != nil || len(ledger.Records) != 1 {
				t.Fatal("uncertain close lost reservation", err)
			}
			for _, record := range ledger.Records {
				if record.State != uploadquota.Committed || record.StagingReleased {
					t.Fatal("uncertain close released staging")
				}
				if _, ok := database.GetMetaDataById(record.FileID); !ok {
					t.Fatal("uncertain cleanup rolled back committed metadata")
				}
			}
		})
	}
}

type wholeFailReader struct{ err error }

func (reader wholeFailReader) Read([]byte) (int, error) { return 0, reader.err }

func TestSEC030WholeReservedReadFailureCleansBeforeAbort(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := wholeTestStore(t, backend)
			input, header, closeInput, calls := wholeTestInput(t, []byte("expected bytes"), "read.bin", "application/octet-stream")
			injected := errors.New("synthetic input read failure")
			reader := io.MultiReader(io.LimitReader(input, 3), wholeFailReader{injected})
			file, err := NewFileReserved(context.Background(), reader, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput)
			if !errors.Is(err, injected) || file.Id != "" || *calls != 1 {
				t.Fatal("read failure did not propagate and close input", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatal("read failure retained an untracked workfile", err)
			}
			ledger, err := database.ReadUploadQuota(context.Background())
			if err != nil || len(ledger.Records) != 1 {
				t.Fatal("read failure lost its terminal receipt", err)
			}
			for _, record := range ledger.Records {
				if record.State != uploadquota.Aborted || !record.StagingReleased {
					t.Fatal("prepublication cleanup did not confirm abort")
				}
			}
			if len(database.GetAllMetadata()) != 0 {
				t.Fatal("read failure committed metadata")
			}
		})
	}
}

func TestSEC030WholeReservedPublicationLockCancellation(t *testing.T) {
	locator := models.File{SHA1: "synthetic-lock-cancellation"}
	unlock, err := lockBlobPublication(context.Background(), locator)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		release, err := lockBlobPublication(ctx, locator)
		if release != nil {
			release()
		}
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled publication wait acquired the held target", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publication lock ignored request cancellation")
	}
}

func TestSEC030WholeReservedGCRechecksCommittedReference(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := wholeTestStore(t, backend)
			payload := []byte("shared blob with an expired previous owner")
			hash := sha1.Sum(payload)
			locator := hex.EncodeToString(hash[:])
			path := filepath.Join(dir, locator)
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			expired := models.File{Id: "expired-reference", UserId: wholeTestOwner, SHA1: locator, SizeBytes: int64(len(payload)), Name: "expired.bin"}
			database.SaveMetaData(expired)
			input, header, closeInput, _ := wholeTestInput(t, payload, "replacement.bin", "application/octet-stream")
			entered, release := make(chan struct{}), make(chan struct{})
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()
			apply := func(ctx context.Context, change uploadquota.Change) (uploadquota.Reservation, error) {
				if change.Action == uploadquota.Commit {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return uploadquota.Reservation{}, ctx.Err()
					}
				}
				return database.ApplyUploadQuota(ctx, change)
			}
			type outcome struct {
				file models.File
				err  error
			}
			uploadDone := make(chan outcome, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var workers sync.WaitGroup
			defer func() {
				cancel()
				select {
				case <-release:
				default:
					close(release)
				}
				workers.Wait()
			}()
			workers.Add(1)
			go func() {
				defer workers.Done()
				file, err := newFileReserved(ctx, input, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput, apply)
				uploadDone <- outcome{file, err}
			}()
			select {
			case <-entered:
			case early := <-uploadDone:
				t.Fatal("upload did not reach its real Commit", early.err)
			case <-ctx.Done():
				t.Fatal("upload did not reach Commit before deadline")
			}
			gcStarted, gcDone := make(chan struct{}), make(chan bool, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				close(gcStarted)
				gcDone <- cleanUpBlobReference(expired.Id, expired, time.Now().Unix())
			}()
			<-gcStarted
			var earlyGC *bool
			select {
			case deleted := <-gcDone:
				earlyGC = &deleted
				t.Error("GC passed the last-reference check while publication held the locator")
			case <-time.After(25 * time.Millisecond):
				// This only checks blocking, not a CPU/throughput threshold.
			}
			close(release)
			var completed outcome
			select {
			case completed = <-uploadDone:
			case <-ctx.Done():
				t.Fatal("upload did not finish after Commit release")
			}
			if completed.err != nil {
				t.Fatal(completed.err)
			}
			if earlyGC == nil {
				select {
				case deleted := <-gcDone:
					if !deleted {
						t.Error("GC did not remove the expired metadata reference")
					}
				case <-ctx.Done():
					t.Fatal("GC stayed blocked after publication")
				}
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("GC deleted the newly committed shared blob", err)
			}
			if _, ok := database.GetMetaDataById(expired.Id); ok {
				t.Fatal("expired metadata was not removed")
			}
			if stored, ok := database.GetMetaDataById(completed.file.Id); !ok || stored.SHA1 != locator {
				t.Fatal("GC removed the newly committed metadata")
			}
		})
	}
}

func TestSEC030WholeReservedLegacyDedupChecksExpectedFlags(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, tc := range []struct {
			name         string
			level        int
			e2e          bool
			previous     models.EncryptionInfo
			wantConflict bool
		}{
			{"server-compatible-zero-candidate", encryption.LocalEncryptionStored, false, models.EncryptionInfo{IsEncrypted: true}, false},
			{"server-found-plain", encryption.LocalEncryptionStored, false, models.EncryptionInfo{}, true},
			{"plain-found-server", encryption.NoEncryption, false, models.EncryptionInfo{IsEncrypted: true}, true},
			{"e2e-compatible", encryption.EndToEndEncryption, true, models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}, false},
			{"e2e-found-plain", encryption.EndToEndEncryption, true, models.EncryptionInfo{}, true},
			{"plain-found-e2e", encryption.NoEncryption, false, models.EncryptionInfo{IsEncrypted: true, IsEndToEndEncrypted: true}, true},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				dir := wholeTestStore(t, backend)
				configuration.Get().Encryption.Level = tc.level
				previous := models.File{Id: "existing-encryption", UserId: wholeTestOwner, SHA1: "synthetic-flags-blob", Encryption: tc.previous, UnlimitedTime: true}
				database.SaveMetaData(previous)
				bytesBefore := []byte("canonical bytes must never be repaired by deletion")
				path := filepath.Join(dir, previous.SHA1)
				if err := os.WriteFile(path, bytesBefore, 0600); err != nil {
					t.Fatal(err)
				}
				// Legacy chunk metadata is constructed before encryption has run:
				// IsEncrypted may still be false, while E2E comes from parameters.
				candidate := models.File{SHA1: previous.SHA1, Encryption: models.EncryptionInfo{IsEndToEndEncrypted: tc.e2e}}
				ok, err := copyEncryptionInfo(&candidate)
				if tc.wantConflict {
					if ok || !errors.Is(err, uploadquota.ErrConflict) {
						t.Fatal("incompatible canonical flags were silently copied", err)
					}
				} else if !ok || err != nil || !reflect.DeepEqual(candidate.Encryption, tc.previous) {
					t.Fatal("compatible canonical flags were rejected or not preserved", err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, bytesBefore) {
					t.Fatal("dedup flag check changed canonical bytes", err)
				}
			})
		}
	}
}

type wholeCountedReader struct {
	reader io.Reader
	reads  int
}

func (reader *wholeCountedReader) Read(buffer []byte) (int, error) {
	reader.reads++
	return reader.reader.Read(buffer)
}

func TestSEC030WholeReservedActiveAndStagingAdmission(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		for _, capacity := range []string{"active", "staging"} {
			t.Run(backend+"/"+capacity, func(t *testing.T) {
				dir := wholeTestStore(t, backend)
				now := time.Now().Unix()
				seed := uploadquota.Reservation{ID: "already-reserved", UserID: wholeTestOwner, BytesKnown: true, Bytes: 1, StagingBytes: 1, ExpiresAt: now + 3600}
				limits := wholeTestLimits()
				if _, err := database.ApplyUploadQuota(context.Background(), uploadquota.Change{Action: uploadquota.Reserve, Upload: seed, Limits: limits, Now: now}); err != nil {
					t.Fatal(err)
				}
				before, err := database.ReadUploadQuota(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				payload := []byte("four")
				input, header, closeInput, closed := wholeTestInput(t, payload, "admission.bin", "application/octet-stream")
				counted := &wholeCountedReader{reader: input}
				want := uploadquota.ErrActive
				if capacity == "active" {
					limits.UserActive, limits.InstanceActive = 1, 1
				} else {
					want = uploadquota.ErrStaging
					// One byte less than the seed plus this candidate's concrete
					// 4*n+64KiB staging declaration. Logical capacities stay ample.
					limits.StagingBytes = seed.StagingBytes + 4*int64(len(payload)) + (64 << 10) - 1
				}
				file, err := NewFileReserved(context.Background(), counted, header, wholeTestOwner, wholeTestParams(), limits, closeInput)
				if file.Id != "" || !errors.Is(err, want) || !errors.Is(err, uploadquota.ErrQuota) {
					t.Fatal("admission returned success or the wrong quota reason", err)
				}
				if counted.reads != 0 || *closed != 1 {
					t.Fatal("rejected reservation consumed content or skipped input cleanup")
				}
				after, err := database.ReadUploadQuota(context.Background())
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("rejected admission changed the existing ledger", err)
				}
				if len(database.GetAllMetadata()) != 0 {
					t.Fatal("rejected admission published raw metadata")
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("rejected admission allocated a workfile/blob", err)
				}
			})
		}
	}
}

func TestSEC030WholeProcessingSpaceCheckpoint(t *testing.T) {
	// A large configured reserve simulates insufficient free space without
	// allocating a large file or modifying the host filesystem capacity.
	t.Setenv("GOKAPI_MIN_FREE_SPACE", "2147483647")
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			dir := wholeTestStore(t, backend)
			input, header, closeInput, closed := wholeTestInput(t, []byte("space checkpoint"), "space.bin", "application/octet-stream")
			counted := &wholeCountedReader{reader: input}
			_, err := NewFileReserved(context.Background(), counted, header, wholeTestOwner, wholeTestParams(), wholeTestLimits(), closeInput)
			if !errors.Is(err, uploadquota.ErrStaging) || counted.reads != 0 || *closed != 1 {
				t.Fatal("space rejection consumed input or failed to close it", err)
			}
			ledger, err := database.ReadUploadQuota(context.Background())
			if err != nil || len(ledger.Records) != 1 {
				t.Fatal("missing durable cleanup receipt", err)
			}
			for _, record := range ledger.Records {
				if record.State != uploadquota.Aborted || !record.StagingReleased {
					t.Fatal("confirmed space failure retained an active reservation")
				}
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 || len(database.GetAllMetadata()) != 0 {
				t.Fatal("space rejection allocated work or published metadata", err)
			}
		})
	}
}
