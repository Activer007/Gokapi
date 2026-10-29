package storage

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/storage/uploadreceive"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

var ErrRecoveryManual = errors.New("upload recovery requires manual review")

type RecoveryItem struct {
	Ref    string `json:"ref"`
	Kind   string `json:"kind"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type RecoveryReport struct {
	Version      int            `json:"version"`
	ScanComplete bool           `json:"scan_complete"`
	Scanned      int            `json:"scanned"`
	Cleaned      int            `json:"cleaned"`
	Held         int            `json:"held"`
	Manual       int            `json:"manual"`
	Truncated    bool           `json:"truncated"`
	Items        []RecoveryItem `json:"items,omitempty"`
}

const recoveryMaxRootEntries = 16384
const recoveryMaxItems = 128

// RecoverUploads runs once before normal GC or admission in a fresh process.
// The caller must exclude other processes and all upload/metadata workers.
// Publication uncertainty is never resolved by this function.
func RecoverUploads(ctx context.Context, prepareReceiver func(context.Context, string) error) (RecoveryReport, error) {
	report := RecoveryReport{Version: 1}
	stop := func(reason string) (RecoveryReport, error) {
		report.manual("", "startup", "unknown", reason)
		return report, ErrRecoveryManual
	}
	if ctx == nil || prepareReceiver == nil {
		return stop("invalid_startup")
	}
	if ctx.Err() != nil {
		return stop("interrupted")
	}
	ledger, err := database.ReadUploadQuota(ctx)
	if err != nil {
		return stop("ledger_unavailable")
	}
	var pending []uploadquota.Reservation
	after := ""
	for {
		if ctx.Err() != nil {
			return stop("interrupted")
		}
		batch, next, err := ledger.RecoveryBatch(after, 128)
		if err != nil {
			return stop("invalid_ledger")
		}
		pending = append(pending, batch...)
		if next == "" {
			break
		}
		after = next
	}
	report.Scanned = len(pending)
	report.Held = len(pending)
	dataDir := configuration.Get().DataDir
	if reason := report.inspectWork(ctx, dataDir); reason != "" {
		return stop(reason)
	}
	now := time.Now().Unix()
	for _, r := range pending {
		reason := recoveryManualReason(r)
		if reason != "" {
			report.manual(r.ID, recoveryKind(r.ID), string(r.State), reason)
		}
	}
	if report.Manual != 0 {
		return report, ErrRecoveryManual
	}
	for _, r := range pending {
		if r.State == uploadquota.Committed && !recoveryCommittedBlob(ctx, dataDir, r) {
			report.manual(r.ID, recoveryKind(r.ID), string(r.State), "commit_unconfirmed")
		}
	}
	if report.Manual != 0 {
		return report, ErrRecoveryManual
	}
	// Both namespaces are fully verified before any deletion. The temporary
	// pool only reads; the callback prepares the actual, reused HTTP pool.
	inspector, err := uploadreceive.New(1, 1)
	if err != nil || inspector.VerifyRecovery(ctx, dataDir) != nil {
		return stop("receiver_namespace")
	}
	store := ChunkSessions()
	if err := store.VerifyRecovery(ctx); err != nil {
		return stop("chunk_namespace")
	}
	report.ScanComplete = true
	if err := prepareReceiver(ctx, dataDir); err != nil {
		return stop("receiver_cleanup")
	}
	if err := store.Sweep(ctx); err != nil {
		return stop("chunk_cleanup")
	}
	for _, r := range pending {
		if ctx.Err() != nil {
			return stop("interrupted")
		}
		switch {
		case r.State == uploadquota.Committed:
			if err := store.CleanupCommittedInput(ctx, r); err != nil {
				return stop("committed_input_cleanup")
			}
			if _, err := database.ApplyUploadQuota(ctx, uploadquota.Change{Action: uploadquota.ReleaseStaging, Upload: r, Now: time.Now().Unix(), CleanupConfirmed: true}); err != nil {
				return stop("release_unconfirmed")
			}
		case strings.HasPrefix(r.ID, "whole-") && r.ExpiresAt <= now:
			// The unclaimed whole reservation precedes work-file creation.
			// Receiver cleanup above confirmed its only possible private input.
			if _, err := database.ApplyUploadQuota(ctx, uploadquota.Change{Action: uploadquota.Abort, Upload: r, Now: time.Now().Unix(), Terminal: uploadquota.Expired, CleanupConfirmed: true}); err != nil {
				return stop("expiration_unconfirmed")
			}
		}
		fresh, err := database.ReadUploadQuota(ctx)
		if err != nil {
			return stop("result_unconfirmed")
		}
		current, ok := fresh.Records[r.ID]
		if !ok {
			return stop("result_unconfirmed")
		}
		if current.StagingReleased {
			report.Cleaned++
			report.Held--
			report.item(r.ID, recoveryKind(r.ID), string(current.State), "cleanup_confirmed")
		} else {
			report.item(r.ID, recoveryKind(r.ID), string(current.State), "active_unclaimed")
		}
	}
	return report, nil
}

func recoveryKind(id string) string {
	if strings.HasPrefix(id, "chunk.") {
		return "chunk"
	}
	if strings.HasPrefix(id, "whole-") {
		return "whole"
	}
	return "unknown"
}

func recoveryManualReason(r uploadquota.Reservation) string {
	if recoveryKind(r.ID) == "unknown" {
		return "unknown_record"
	}
	if r.Completing {
		return "claimed"
	}
	if r.State == uploadquota.Committed {
		if recoveryKind(r.ID) != "chunk" {
			return "whole_commit"
		}
		return ""
	}
	if r.State != uploadquota.Reserved && r.State != uploadquota.Uploading {
		return "unknown_state"
	}
	if recoveryKind(r.ID) == "whole" && (r.State != uploadquota.Reserved || !r.BytesKnown || r.InitiallyUnbound || r.FileID != "" || r.RequestID != "") {
		return "whole_state_unconfirmed"
	}
	return ""
}

func recoveryCommittedBlob(ctx context.Context, dataDir string, r uploadquota.Reservation) bool {
	if ctx.Err() != nil {
		return false
	}
	file, ok, readErr := database.ReadRecoveryFile(ctx, r.FileID)
	if readErr != nil || !ok || !r.MatchesCommittedFile(file) || !file.IsLocalStorage() || len(file.SHA1) != 40 || (file.Encryption.IsEncrypted && !file.Encryption.IsEndToEndEncrypted) {
		return false
	}
	if _, err := hex.DecodeString(file.SHA1); err != nil {
		return false
	}
	info, err := os.Lstat(filepath.Join(dataDir, file.SHA1))
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if info.Size() != r.Bytes {
		return false
	}
	input, err := os.Open(filepath.Join(dataDir, file.SHA1))
	if err != nil {
		return false
	}
	opened, statErr := input.Stat()
	if statErr != nil || !os.SameFile(info, opened) {
		_ = input.Close()
		return false
	}
	// The existing plaintext/E2E locator is a digest of the stored bytes.
	// Server encryption uses a different derivation and remains manual here.
	hash := sha1.New()
	n, readErr := io.Copy(hash, io.LimitReader(wholeContextReader{ctx, input}, r.Bytes+1))
	closeErr := input.Close()
	return readErr == nil && closeErr == nil && ctx.Err() == nil && n == r.Bytes && hex.EncodeToString(hash.Sum(nil)) == file.SHA1
}

// inspectWork never follows or removes any root entry. Even a recognized
// prefix has no persistent mapping to a reservation and must remain manual.
func (report *RecoveryReport) inspectWork(ctx context.Context, dataDir string) string {
	info, err := os.Lstat(dataDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "data_directory"
	}
	directory, err := os.Open(dataDir)
	if err != nil {
		return "data_directory"
	}
	defer directory.Close()
	count := 0
	for {
		if ctx.Err() != nil {
			return "interrupted"
		}
		entries, err := directory.ReadDir(128)
		count += len(entries)
		if count > recoveryMaxRootEntries {
			report.Truncated = true
			return "scan_limit"
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".gokapi-whole-") {
				report.Scanned++
				report.manual(entry.Name(), "whole_work", "unknown", "unmapped_work")
			}
		}
		if errors.Is(err, io.EOF) {
			return ""
		}
		if err != nil {
			return "data_directory"
		}
	}
}

func (report *RecoveryReport) manual(id, kind, state, reason string) {
	report.Manual++
	report.item(id, kind, state, reason)
}

func (report *RecoveryReport) item(id, kind, state, reason string) {
	if len(report.Items) >= recoveryMaxItems {
		report.Truncated = true
		return
	}
	ref := ""
	if id != "" {
		hash := sha256.Sum256([]byte(id))
		ref = hex.EncodeToString(hash[:8])
	}
	report.Items = append(report.Items, RecoveryItem{Ref: ref, Kind: kind, State: state, Reason: reason})
}
