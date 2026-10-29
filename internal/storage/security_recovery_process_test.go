//go:build test && security_recovery

package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/forceu/gokapi/internal/configuration"
	"github.com/forceu/gokapi/internal/configuration/database"
	"github.com/forceu/gokapi/internal/encryption"
	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/storage/chunking"
	"github.com/forceu/gokapi/internal/storage/chunking/chunksession"
	"github.com/forceu/gokapi/internal/storage/filesystem"
	"github.com/forceu/gokapi/internal/storage/filesystem/s3filesystem/aws"
	"github.com/forceu/gokapi/internal/storage/uploadreceive"
	"github.com/forceu/gokapi/internal/uploadquota"
)

const recoveryProcessEnv = "GOKAPI_SEC030_RECOVERY_PROCESS"
const recoveryRootEnv = "GOKAPI_SEC030_RECOVERY_ROOT"
const recoveryOwner = 9030
const recoveryCrashExit = 97

// This is a new startup-recovery contract. The initial coordinator stub is
// expected to be RED; it is not evidence of a historical recovery vulnerability.
// Every crash and restart executes this test binary in a fresh working directory.
func TestSEC030RecoveryCrash(t *testing.T) {
	if phase := os.Getenv(recoveryProcessEnv); phase != "" {
		sec030RecoveryWorker(t, os.Getenv(recoveryRootEnv), phase)
		return
	}
	for _, window := range []string{"A", "B", "C"} {
		t.Run(window, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
				t.Fatal("RECOVERY_SETUP cannot create isolated persistent data directory")
			}
			sec030RecoveryRun(t, root, window+"/crash", true)
			if window == "A" {
				sec030RecoveryExpireFixture(t, root)
				sec030RecoveryRun(t, root, "A/recover", false)
				return
			}
			sec030RecoveryRun(t, root, window+"/manual", false)
			if window == "C" {
				sec030RecoveryReviewWork(t, root)
				sec030RecoveryRun(t, root, "C/recover", false)
				sec030RecoveryRun(t, root, "C/repeat", false)
			}
		})
	}
}

// These two negative fixtures reuse the crash boundaries above. They do not add
// a provider or crash-window matrix: each must preserve all evidence and quota.
func TestSEC030RecoveryNegative(t *testing.T) {
	for _, kind := range []string{"canonical", "whole"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
				t.Fatal("RECOVERY_SETUP cannot create isolated negative data directory")
			}
			if kind == "canonical" {
				sec030RecoveryRun(t, root, "C/crash", true)
				sec030RecoveryReviewWork(t, root)
				sec030RecoveryCorruptCanonical(t, root)
				sec030RecoveryRun(t, root, "canonical/manual", false)
				return
			}
			sec030RecoveryRun(t, root, "A/crash", true)
			sec030RecoveryExpireFixture(t, root)
			sec030RecoveryImpossibleWhole(t, root)
			sec030RecoveryRun(t, root, "whole/manual", false)
		})
	}
}

func sec030RecoveryRun(t *testing.T, root, phase string, crash bool) {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot locate isolated executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestSEC030RecoveryCrash$", "-test.v", "-test.timeout=40s")
	command.Dir = t.TempDir()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, recoveryProcessEnv) && !strings.EqualFold(name, recoveryRootEnv) {
			command.Env = append(command.Env, entry)
		}
	}
	command.Env = append(command.Env, recoveryProcessEnv+"="+phase, recoveryRootEnv+"="+root)
	output, runErr := command.CombinedOutput()
	// Startup and transport diagnostics can contain paths. Forward only our
	// fixed test messages; never print raw IDs, tokens, reports or filenames.
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, "RECOVERY_") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if crash {
		var exitError *exec.ExitError
		if !errors.As(runErr, &exitError) || exitError.ExitCode() != recoveryCrashExit {
			t.Fatal("RECOVERY_CRASH production boundary did not exit with the required code 97")
		}
	} else if runErr != nil {
		t.Fatal("RECOVERY_CHILD fresh-process recovery contract failed")
	}
}

func sec030RecoveryWorker(t *testing.T, root, phase string) {
	t.Helper()
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("RECOVERY_SETUP missing isolated persistent directory")
	}
	database.Close()
	cfg := configuration.Get()
	cfg.DataDir, cfg.DatabaseUrl = filepath.Join(root, "data"), "sqlite://"+filepath.Join(root, "state.sqlite")
	cfg.MaxFileSizeMB, cfg.MaxMemory = 4, 1
	cfg.Encryption.Level = encryption.NoEncryption
	cfg.Authentication.SaltFiles = "synthetic-recovery-file-salt"
	filesystem.Init(cfg.DataDir)
	if aws.IsAvailable() {
		t.Fatal("RECOVERY_SETUP process contract requires local storage")
	}
	database.Connect(models.DbConnection{HostUrl: filepath.Join(root, "state.sqlite")})
	defer database.Close()
	if strings.HasSuffix(phase, "/crash") {
		sec030RecoveryCrashWorker(t, strings.TrimSuffix(phase, "/crash"))
		t.Fatal("RECOVERY_CRASH worker returned without reaching its production boundary")
	}
	before := sec030RecoverySnapshot(t)
	var old uploadquota.Reservation
	if phase == "whole/manual" {
		for _, record := range before.ledger.Records {
			if strings.HasPrefix(record.ID, "whole-") {
				old = record
			}
		}
		if len(before.ledger.Records) != 2 || old.State != uploadquota.Uploading || !old.BytesKnown || !old.InitiallyUnbound || old.Completing || old.FileID != "" {
			t.Fatal("RECOVERY_SETUP negative whole state did not reach the intended legal-ledger boundary")
		}
	} else {
		old = sec030RecoveryRecord(t, before.ledger)
	}
	prepareCalls := 0
	prepare := func(ctx context.Context, dataDir string) error {
		prepareCalls++
		pool, err := uploadreceive.New(1, 8<<20)
		if err != nil {
			return err
		}
		preparer, ok := any(pool).(interface {
			Prepare(context.Context, string) error
		})
		if !ok {
			return errors.New("receiver preparation contract is unavailable")
		}
		return preparer.Prepare(ctx, dataDir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	report, err := RecoverUploads(ctx, prepare)
	if strings.HasSuffix(phase, "/manual") {
		if !errors.Is(err, ErrRecoveryManual) || report.Version != 1 || report.Manual < 1 {
			t.Fatal("RECOVERY_MANUAL uncertain publication must require manual review")
		}
		if !reflect.DeepEqual(before, sec030RecoverySnapshot(t)) {
			t.Fatal("RECOVERY_MANUAL uncertain publication changed quota, metadata or any retained file")
		}
		if old.StagingReleased || (phase == "B/manual" && (!old.Completing || old.State != uploadquota.Uploading || len(before.metadata) != 1)) ||
			(phase == "C/manual" && (old.State != uploadquota.Committed || old.Completing || len(before.metadata) != 2)) {
			t.Fatal("RECOVERY_MANUAL crash did not retain its exact unrefunded publication boundary")
		}
		return
	}
	if phase != "A/recover" && phase != "C/recover" && phase != "C/repeat" {
		t.Fatal("RECOVERY_SETUP invalid fresh-process recovery phase")
	}
	if err != nil || report.Version != 1 || !report.ScanComplete || report.Truncated || report.Manual != 0 || report.Held != 0 || prepareCalls != 1 {
		t.Fatal("RECOVERY_COMPLETE safe startup did not finish scanning and prepare reception")
	}
	after := sec030RecoverySnapshot(t)
	record := sec030RecoveryRecord(t, after.ledger)
	if !reflect.DeepEqual(before.metadata, after.metadata) || !record.StagingReleased {
		t.Fatal("RECOVERY_COMPLETE startup changed committed metadata or failed to release confirmed staging")
	}
	sec030RecoveryCheckControl(t, after.metadata)
	sec030RecoveryNoInputs(t)
	if phase == "A/recover" {
		if old.Completing || old.State != uploadquota.Uploading || record.State != uploadquota.Expired || record.Completing || record.FileID != "" || len(after.metadata) != 1 || report.Cleaned < 1 {
			t.Fatal("RECOVERY_UNCLAIMED expired pre-Claim input was not cleaned and expired without publication")
		}
		if record.ID != old.ID || record.UserID != old.UserID || record.RequestID != old.RequestID || record.Bytes != old.Bytes || record.StagingBytes != old.StagingBytes || record.CreatedAt != old.CreatedAt || record.ExpiresAt != old.ExpiresAt || record.FinishedAt < record.ExpiresAt {
			t.Fatal("RECOVERY_UNCLAIMED cleanup changed the declaration instead of confirming its expiration")
		}
		return
	}
	if old.State != uploadquota.Committed || old.Completing || len(after.metadata) != 2 {
		t.Fatal("RECOVERY_COMMITTED restart lost the unique committed upload")
	}
	want := old
	want.StagingReleased = true
	if !reflect.DeepEqual(want, record) {
		t.Fatal("RECOVERY_COMMITTED cleanup changed the committed receipt beyond its staging confirmation")
	}
	sec030RecoveryCheckUpload(t, record, after.metadata)
	if phase == "C/repeat" {
		if !old.StagingReleased || report.Cleaned != 0 || !reflect.DeepEqual(before, after) {
			t.Fatal("RECOVERY_IDEMPOTENT second fresh startup repeated cleanup or changed durable state")
		}
	} else if old.StagingReleased || report.Cleaned < 1 {
		t.Fatal("RECOVERY_COMMITTED known chunk input was not cleaned after reviewed work removal")
	}
}

func sec030RecoveryCrashWorker(t *testing.T, window string) {
	t.Helper()
	if window != "A" && window != "B" && window != "C" {
		t.Fatal("RECOVERY_SETUP invalid crash window")
	}
	database.SaveUser(models.User{Id: recoveryOwner, Name: "synthetic-recovery-owner", UserLevel: models.UserLevelUser}, false)
	control := sec030RecoveryControl()
	if err := os.WriteFile(filepath.Join(configuration.Get().DataDir, control.SHA1), sec030RecoveryControlBytes(), 0600); err != nil {
		t.Fatal("RECOVERY_SETUP cannot write canonical control")
	}
	database.SaveMetaData(control)
	payload := sec030RecoveryBytes()
	scope := chunksession.Scope{ClientID: "synthetic-recovery-client", UserID: recoveryOwner}
	ctx := context.Background()
	limits := sec030RecoveryLimits()
	if _, err := ChunkSessions().Write(ctx, scope, chunking.ChunkInfo{UUID: scope.ClientID, TotalFilesizeBytes: int64(len(payload))}, bytes.NewReader(payload), int64(len(payload)), 4<<20, limits); err != nil {
		t.Fatal("RECOVERY_SETUP production chunk write failed")
	}
	if window == "A" {
		record := sec030RecoveryRecord(t, sec030RecoverySnapshot(t).ledger)
		if record.State != uploadquota.Uploading || record.Completing || record.FileID != "" || record.StagingReleased {
			t.Fatal("RECOVERY_SETUP pre-Claim production state is invalid")
		}
		os.Exit(recoveryCrashExit)
	}
	lease, err := ChunkSessions().Prepare(ctx, scope, chunking.FileHeader{Filename: "synthetic-recovery.bin", ContentType: "application/octet-stream", Size: int64(len(payload))}, "synthetic-recovery-file", limits)
	if err != nil || lease.Input == nil || !lease.Upload.Completing {
		t.Fatal("RECOVERY_SETUP production chunk Claim failed")
	}
	header := &multipart.FileHeader{Filename: "synthetic-recovery.bin", Size: int64(len(payload)), Header: make(textproto.MIMEHeader)}
	header.Header.Set("Content-Type", "application/octet-stream")
	apply := func(ctx context.Context, change uploadquota.Change) (uploadquota.Reservation, error) {
		if change.Action != uploadquota.Commit {
			return database.ApplyUploadQuota(ctx, change)
		}
		if change.File == nil || change.File.UserId != recoveryOwner {
			t.Fatal("RECOVERY_SETUP completion omitted its authenticated metadata")
		}
		sec030RecoveryPublished(t, *change.File)
		if window == "B" {
			if len(database.GetAllMetadata()) != 1 {
				t.Fatal("RECOVERY_SETUP metadata appeared before real Commit")
			}
			os.Exit(recoveryCrashExit)
		}
		record, err := database.ApplyUploadQuota(ctx, change)
		if err != nil || record.State != uploadquota.Committed || record.StagingReleased || len(database.GetAllMetadata()) != 2 {
			t.Fatal("RECOVERY_SETUP real Commit did not precede the crash")
		}
		os.Exit(recoveryCrashExit)
		return record, err
	}
	_, _ = finishReservedFile(ctx, lease.Input, header, recoveryOwner, models.UploadParameters{UnlimitedTime: true, UnlimitedDownload: true}, limits, lease.Upload, lease.CloseInput, apply)
}

func sec030RecoveryLimits() uploadquota.Limits {
	return uploadquota.Limits{User: uploadquota.Usage{Bytes: 16 << 20, Files: 16}, Request: uploadquota.Usage{Bytes: 16 << 20, Files: 16},
		Instance: uploadquota.Usage{Bytes: 32 << 20, Files: 32}, UserActive: 8, RequestActive: 8, InstanceActive: 8, StagingBytes: 32 << 20}
}

func sec030RecoveryBytes() []byte        { return bytes.Repeat([]byte{0x72}, 64<<10) }
func sec030RecoveryControlBytes() []byte { return []byte("synthetic recovery canonical control") }

func sec030RecoveryControl() models.File {
	content := sec030RecoveryControlBytes()
	hash := sha1.Sum(content)
	return models.File{Id: "synthetic-recovery-control", Name: "synthetic-control.bin", SHA1: hex.EncodeToString(hash[:]), SizeBytes: int64(len(content)), UserId: recoveryOwner, UnlimitedTime: true, UnlimitedDownloads: true}
}

type sec030RecoveryState struct {
	ledger   uploadquota.Ledger
	metadata map[string]models.File
	files    map[string][]byte
}

func sec030RecoverySnapshot(t *testing.T) sec030RecoveryState {
	t.Helper()
	ledger, err := database.ReadUploadQuota(context.Background())
	if err != nil {
		t.Fatal("RECOVERY_STATE cannot read durable declarations")
	}
	return sec030RecoveryState{ledger, database.GetAllMetadata(), sec030RecoveryFiles(t, configuration.Get().DataDir)}
}

func sec030RecoveryRecord(t *testing.T, ledger uploadquota.Ledger) uploadquota.Reservation {
	t.Helper()
	if len(ledger.Records) != 1 {
		t.Fatal("RECOVERY_STATE fixture must retain exactly one upload declaration")
	}
	for _, record := range ledger.Records {
		if record.UserID != recoveryOwner || record.RequestID != "" || !record.BytesKnown || record.Bytes != int64(len(sec030RecoveryBytes())) {
			t.Fatal("RECOVERY_STATE durable declaration lost fixed scope or size")
		}
		return record
	}
	panic("unreachable recovery fixture")
}

func sec030RecoveryFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := make(map[string][]byte)
	entries, total := 0, 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		entries++
		if walkErr != nil || entries > 64 || entry.Type()&os.ModeSymlink != 0 {
			return errors.New("invalid recovery snapshot entry")
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 1<<20 {
			return errors.New("invalid recovery snapshot file")
		}
		data, err := os.ReadFile(path)
		if err != nil || total > (4<<20)-len(data) {
			return errors.New("recovery snapshot exceeds fixture bound")
		}
		total += len(data)
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = data
		return nil
	})
	if err != nil {
		t.Fatal("RECOVERY_FILES isolated file snapshot failed its bounded validation")
	}
	return result
}

func sec030RecoveryCheckControl(t *testing.T, metadata map[string]models.File) {
	t.Helper()
	control := sec030RecoveryControl()
	if !reflect.DeepEqual(metadata[control.Id], control) {
		t.Fatal("RECOVERY_CONTROL unrelated canonical metadata changed")
	}
	data, err := os.ReadFile(filepath.Join(configuration.Get().DataDir, control.SHA1))
	if err != nil || !bytes.Equal(data, sec030RecoveryControlBytes()) {
		t.Fatal("RECOVERY_CONTROL unrelated canonical bytes changed")
	}
}

func sec030RecoveryCheckUpload(t *testing.T, record uploadquota.Reservation, metadata map[string]models.File) {
	t.Helper()
	file, found := metadata[record.FileID]
	if !found || file.UserId != recoveryOwner || file.SizeBytes != record.Bytes || file.UploadRequestId != "" || !file.UnlimitedTime || !file.UnlimitedDownloads {
		t.Fatal("RECOVERY_COMMITTED unique uploaded metadata differs from the committed declaration")
	}
	sec030RecoveryPublishedHash(t, filepath.Join(configuration.Get().DataDir, file.SHA1))
}

func sec030RecoveryPublishedHash(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(data) != sha256.Sum256(sec030RecoveryBytes()) {
		t.Fatal("RECOVERY_CONTENT published upload failed SHA256 read-back")
	}
}

func sec030RecoveryPublished(t *testing.T, file models.File) {
	t.Helper()
	canonical := filepath.Join(configuration.Get().DataDir, file.SHA1)
	sec030RecoveryPublishedHash(t, canonical)
	info, err := os.Stat(canonical)
	work, globErr := filepath.Glob(filepath.Join(configuration.Get().DataDir, ".gokapi-whole-*"))
	if err != nil || globErr != nil || len(work) != 1 {
		t.Fatal("RECOVERY_SETUP real publication did not leave its private work link")
	}
	workInfo, err := os.Stat(work[0])
	if err != nil || !os.SameFile(info, workInfo) {
		t.Fatal("RECOVERY_SETUP canonical publication was not the production hard link")
	}
}

func sec030RecoveryNoInputs(t *testing.T) {
	t.Helper()
	work, err := filepath.Glob(filepath.Join(configuration.Get().DataDir, ".gokapi-whole-*"))
	if err != nil || len(work) != 0 {
		t.Fatal("RECOVERY_INPUT confirmed recovery retained a private whole work file")
	}
	for _, namespace := range []string{".gokapi-chunks-v1", ".gokapi-receive-v1"} {
		entries, err := os.ReadDir(filepath.Join(configuration.Get().DataDir, namespace))
		if err != nil || len(entries) != 1 || entries[0].Name() != ".owner" {
			t.Fatal("RECOVERY_INPUT confirmed recovery retained owned chunk or receive input")
		}
	}
}

// Only the already crashed fixture is edited, through SQLite's raw State JSON.
// No upload transition is used to pretend that a live worker had expired.
func sec030RecoveryExpireFixture(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "state.sqlite"))
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot open offline fixture database")
	}
	defer db.Close()
	var state []byte
	if err := db.QueryRow("SELECT State FROM UploadQuota WHERE Id = 1").Scan(&state); err != nil {
		t.Fatal("RECOVERY_SETUP cannot read offline quota JSON")
	}
	ledger, err := uploadquota.Decode(state)
	if err != nil {
		t.Fatal("RECOVERY_SETUP offline quota JSON is invalid")
	}
	record := sec030RecoveryRecord(t, ledger)
	if record.Completing || record.State != uploadquota.Uploading {
		t.Fatal("RECOVERY_SETUP only the pre-Claim fixture may be expired offline")
	}
	now := time.Now().Unix()
	record.CreatedAt, record.ExpiresAt = now-7200, now-3600
	ledger.Records[record.ID] = record
	state, err = json.Marshal(ledger)
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot encode offline fixture JSON")
	}
	if _, err := uploadquota.Decode(state); err != nil {
		t.Fatal("RECOVERY_SETUP offline expiration violates durable timestamp constraints")
	}
	result, err := db.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", state)
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot persist offline fixture expiration")
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		t.Fatal("RECOVERY_SETUP offline expiration did not update exactly one ledger")
	}
}

// Simulate the operator's backup and offline review. Only a work link proven to
// share the committed canonical inode and SHA256 is removed; inputs stay put for
// the following fresh-process coordinator to clean and confirm staging release.
func sec030RecoveryReviewWork(t *testing.T, root string) {
	t.Helper()
	dataDir, backup := filepath.Join(root, "data"), filepath.Join(root, "reviewed-backup")
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal("RECOVERY_REVIEW cannot create fixture backup")
	}
	for relative, content := range sec030RecoveryFiles(t, dataDir) {
		path := filepath.Join(backup, "data", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal("RECOVERY_REVIEW cannot create fixture backup directory")
		}
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal("RECOVERY_REVIEW cannot back up fixture data")
		}
	}
	for _, name := range []string{"state.sqlite", "state.sqlite-wal", "state.sqlite-shm"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) && name != "state.sqlite" {
			continue
		}
		if err != nil || len(content) > 4<<20 || os.WriteFile(filepath.Join(backup, name), content, 0600) != nil {
			t.Fatal("RECOVERY_REVIEW cannot back up offline fixture database")
		}
	}
	db, err := sql.Open("sqlite", filepath.Join(root, "state.sqlite"))
	if err != nil {
		t.Fatal("RECOVERY_REVIEW cannot inspect offline fixture database")
	}
	defer db.Close()
	var state []byte
	if err := db.QueryRow("SELECT State FROM UploadQuota WHERE Id = 1").Scan(&state); err != nil {
		t.Fatal("RECOVERY_REVIEW cannot inspect committed receipt")
	}
	ledger, err := uploadquota.Decode(state)
	if err != nil {
		t.Fatal("RECOVERY_REVIEW committed receipt is invalid")
	}
	record := sec030RecoveryRecord(t, ledger)
	if record.State != uploadquota.Committed || record.StagingReleased {
		t.Fatal("RECOVERY_REVIEW offline review requires a held committed receipt")
	}
	var locator string
	if err := db.QueryRow("SELECT SHA1 FROM FileMetaData WHERE Id = ?", record.FileID).Scan(&locator); err != nil {
		t.Fatal("RECOVERY_REVIEW cannot resolve unique committed canonical metadata")
	}
	canonical := filepath.Join(dataDir, locator)
	sec030RecoveryPublishedHash(t, canonical)
	info, err := os.Lstat(canonical)
	work, globErr := filepath.Glob(filepath.Join(dataDir, ".gokapi-whole-*"))
	if err != nil || globErr != nil || len(work) != 1 || !info.Mode().IsRegular() {
		t.Fatal("RECOVERY_REVIEW offline publication has an unexpected work set")
	}
	workInfo, err := os.Lstat(work[0])
	if err != nil || !workInfo.Mode().IsRegular() || !os.SameFile(info, workInfo) {
		t.Fatal("RECOVERY_REVIEW work link is not the confirmed canonical inode")
	}
	sec030RecoveryPublishedHash(t, work[0])
	if err := os.Remove(work[0]); err != nil {
		t.Fatal("RECOVERY_REVIEW cannot unlink the confirmed fixture work link")
	}
	sec030RecoveryPublishedHash(t, canonical)
}

func sec030RecoveryCorruptCanonical(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "state.sqlite"))
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot inspect negative canonical metadata")
	}
	defer db.Close()
	var locator string
	if err := db.QueryRow("SELECT SHA1 FROM FileMetaData WHERE Id = ?", "synthetic-recovery-file").Scan(&locator); err != nil {
		t.Fatal("RECOVERY_SETUP negative committed metadata is missing")
	}
	path := filepath.Join(root, "data", locator)
	sec030RecoveryPublishedHash(t, path)
	content, err := os.ReadFile(path)
	if err != nil || len(content) != len(sec030RecoveryBytes()) {
		t.Fatal("RECOVERY_SETUP cannot read fixed-size negative canonical content")
	}
	content[0] ^= 0xff
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal("RECOVERY_SETUP cannot persist same-length canonical corruption")
	}
}

func sec030RecoveryImpossibleWhole(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "state.sqlite"))
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot open negative whole fixture")
	}
	defer db.Close()
	var state []byte
	if err := db.QueryRow("SELECT State FROM UploadQuota WHERE Id = 1").Scan(&state); err != nil {
		t.Fatal("RECOVERY_SETUP cannot read negative whole fixture")
	}
	ledger, err := uploadquota.Decode(state)
	if err != nil {
		t.Fatal("RECOVERY_SETUP negative whole source ledger is invalid")
	}
	record := sec030RecoveryRecord(t, ledger)
	// This is a valid Bind result for an initially unbound upload, but the
	// whole-file production path never binds: before Claim it is known/Reserved.
	// Keep the original expired chunk record and its matching input intact, so
	// namespace validation cannot mask a missing whole-state proof.
	record.ID, record.InitiallyUnbound = "whole-synthetic-impossible", true
	ledger.Records[record.ID] = record
	state, err = json.Marshal(ledger)
	if err != nil {
		t.Fatal("RECOVERY_SETUP cannot encode negative whole fixture")
	}
	if _, err := uploadquota.Decode(state); err != nil {
		t.Fatal("RECOVERY_SETUP negative whole fixture must remain a valid ledger")
	}
	if _, err := db.Exec("UPDATE UploadQuota SET State = ? WHERE Id = 1", state); err != nil {
		t.Fatal("RECOVERY_SETUP cannot persist negative whole fixture")
	}
}
