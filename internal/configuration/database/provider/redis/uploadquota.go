package redis

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	randv2 "math/rand/v2"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	keyUploadQuota      = "uploadquota:ledger"
	keyMetadataRevision = "uploadquota:metadata-revision"
	quotaAttempts       = 256
)

func (p DatabaseProvider) ApplyUploadQuota(ctx context.Context, change uploadquota.Change) (uploadquota.Reservation, error) {
	if (change.Action == uploadquota.DuplicateMetadata || change.Action == uploadquota.TransferOwner) && change.Source == nil {
		return uploadquota.Reservation{}, uploadquota.ErrInvalid
	}
	if change.Action == uploadquota.ReplaceMetadata {
		if err := uploadquota.ValidateReplacement(change); err != nil {
			return uploadquota.Reservation{}, err
		}
	}
	mutate := func(conn redigo.Conn, ledger uploadquota.Ledger) (uploadquota.Ledger, uploadquota.Reservation, *models.File, error) {
		// Trusted cleanup transitions cannot add usage or metadata. Unrelated
		// corrupt metadata must not prevent confirming already completed cleanup.
		if change.Action == uploadquota.Abort || change.Action == uploadquota.ReleaseStaging {
			return uploadquota.Apply(ledger, nil, change)
		}
		if change.Action == uploadquota.DuplicateMetadata {
			source, err := p.readDuplicateSource(ctx, conn, change.Source.Id)
			if err != nil {
				return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
			}
			if err = uploadquota.CheckDuplicateSource(*change.Source, source, change.Now); err != nil {
				return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
			}
		}
		effective := change
		if change.RequiresRequestPolicy() {
			policy, err := p.readRequestPolicy(ctx, conn, change.Upload.RequestID)
			if err != nil {
				return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
			}
			effective, err = uploadquota.ApplyRequestPolicy(ledger, change, policy)
			if err != nil {
				return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
			}
		}
		files, err := p.quotaMetadata(ctx, conn)
		if err != nil {
			return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, err
		}
		return uploadquota.Apply(ledger, files, effective)
	}
	if change.Action == uploadquota.TransferOwner || change.Action == uploadquota.ReplaceMetadata {
		return p.changeUploadQuotaWithPatch(ctx, mutate, &change)
	}
	return p.changeUploadQuota(ctx, mutate)
}

func (p DatabaseProvider) ReadUploadQuota(ctx context.Context) (uploadquota.Ledger, error) {
	if ctx == nil {
		return uploadquota.Ledger{}, uploadquota.ErrInvalid
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return uploadquota.Ledger{}, err
	}
	defer conn.Close()
	return p.readUploadQuota(ctx, conn)
}

func (p DatabaseProvider) ImportUploadQuota(ctx context.Context, ledger uploadquota.Ledger) error {
	if _, err := uploadquota.Encode(ledger); err != nil {
		return err
	}
	if ledger.HasOutstanding() {
		return uploadquota.ErrConflict
	}
	_, err := p.changeUploadQuota(ctx, func(_ redigo.Conn, current uploadquota.Ledger) (uploadquota.Ledger, uploadquota.Reservation, *models.File, error) {
		if len(current.Records) != 0 {
			return uploadquota.Ledger{}, uploadquota.Reservation{}, nil, uploadquota.ErrConflict
		}
		return ledger, uploadquota.Reservation{}, nil, nil
	})
	return err
}

// CheckEmptyForMigration is an offline preflight, not a lock against writers.
// Bootstrap keys are the only permitted keys in the selected application prefix;
// orphan sessions, statistics, E2E configuration and unknown keys are data too.
func (p DatabaseProvider) CheckEmptyForMigration(ctx context.Context) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	multi := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		command := "UNWATCH"
		if multi {
			command = "DISCARD"
		}
		_, _ = redigo.DoContext(conn, cleanup, command)
	}()
	bootstrap := map[string]bool{
		p.dbPrefix + keyDbVersion:             true,
		p.dbPrefix + keyDeliveriesVersion:     true,
		p.dbPrefix + keyUploadQuota:           true,
		p.dbPrefix + keyMetadataRevision:      true,
		p.dbPrefix + keyBlobCandidatesVersion: true,
		p.dbPrefix + keyBlobCandidates:        true,
	}
	if _, err = redigo.DoContext(conn, ctx, "WATCH", p.dbPrefix+keyDbVersion, p.dbPrefix+keyUploadQuota, p.dbPrefix+keyMetadataRevision, p.dbPrefix+keyBlobCandidatesVersion, p.dbPrefix+keyBlobCandidates, p.dbPrefix+keyDeliveriesVersion); err != nil {
		return err
	}
	if err := p.checkDeliverySchema(ctx, conn); err != nil {
		return err
	}
	ledger, err := p.readUploadQuota(ctx, conn)
	if err != nil {
		return err
	}
	if len(ledger.Records) != 0 {
		return uploadquota.ErrConflict
	}
	candidates, err := p.readBlobCandidatesConn(ctx, conn)
	if err != nil {
		return err
	}
	if len(candidates) != 0 {
		return uploadquota.ErrConflict
	}
	version, err := redigo.Int(redigo.DoContext(conn, ctx, "GET", p.dbPrefix+keyDbVersion))
	if err != nil || version != DatabaseSchemeVersion {
		return uploadquota.ErrSchema
	}
	pattern := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(p.dbPrefix) + "*"
	cursor := "0"
	// Bound both each requested batch and the total scan work. A database too
	// busy/large for this preflight must be inspected offline, not treated empty.
	complete := false
	for batch := 0; batch < 4096; batch++ {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", 100))
		if err != nil {
			return err
		}
		if len(reply) != 2 {
			return uploadquota.ErrInvalid
		}
		cursor, err = redigo.String(reply[0], nil)
		if err != nil {
			return err
		}
		keys, err := redigo.Strings(reply[1], nil)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, p.dbPrefix) || !bootstrap[key] {
				return uploadquota.ErrConflict
			}
		}
		if cursor == "0" {
			complete = true
			break
		}
	}
	if !complete {
		return uploadquota.ErrRetry
	}
	// A read-only EXEC verifies bootstrap/metadata revision stability during the
	// scan. All writers must still be stopped for the migration itself.
	if _, err = redigo.DoContext(conn, ctx, "MULTI"); err != nil {
		return err
	}
	multi = true
	if _, err = redigo.DoContext(conn, ctx, "PING"); err != nil {
		return err
	}
	reply, err := redigo.DoContext(conn, ctx, "EXEC")
	if err != nil {
		return err
	}
	multi = false
	if reply == nil {
		return uploadquota.ErrConflict
	}
	return checkQuotaExec(reply, 1)
}

type quotaMutation func(redigo.Conn, uploadquota.Ledger) (uploadquota.Ledger, uploadquota.Reservation, *models.File, error)

func (p DatabaseProvider) changeUploadQuota(ctx context.Context, mutate quotaMutation) (uploadquota.Reservation, error) {
	return p.changeUploadQuotaWithPatch(ctx, mutate, nil)
}

func (p DatabaseProvider) changeUploadQuotaWithPatch(ctx context.Context, mutate quotaMutation, patch *uploadquota.Change) (uploadquota.Reservation, error) {
	if ctx == nil {
		return uploadquota.Reservation{}, uploadquota.ErrInvalid
	}
	for attempt := 0; attempt < quotaAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return uploadquota.Reservation{}, err
		}
		record, retry, err := p.quotaAttemptWithPatch(ctx, mutate, patch)
		if err != nil || !retry {
			return record, err
		}
		timer := time.NewTimer(time.Duration(1+randv2.IntN(10)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return uploadquota.Reservation{}, ctx.Err()
		case <-timer.C:
		}
	}
	return uploadquota.Reservation{}, uploadquota.ErrRetry
}

func (p DatabaseProvider) quotaAttempt(ctx context.Context, mutate quotaMutation) (uploadquota.Reservation, bool, error) {
	return p.quotaAttemptWithPatch(ctx, mutate, nil)
}

func (p DatabaseProvider) quotaAttemptWithPatch(ctx context.Context, mutate quotaMutation, patch *uploadquota.Change) (uploadquota.Reservation, bool, error) {
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	defer conn.Close()
	multi := false
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		command := "UNWATCH"
		if multi {
			command = "DISCARD"
		}
		_, _ = redigo.DoContext(conn, cleanup, command)
	}()
	if _, err = redigo.DoContext(conn, ctx, "WATCH", p.dbPrefix+keyUploadQuota, p.dbPrefix+keyMetadataRevision); err != nil {
		return uploadquota.Reservation{}, false, err
	}
	ledger, err := p.readUploadQuota(ctx, conn)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	var patchArgs redigo.Args
	var blobPatchArgs redigo.Args
	if patch != nil {
		switch patch.Action {
		case uploadquota.TransferOwner:
			err = p.checkOwnerTransfer(ctx, conn, *patch)
		case uploadquota.ReplaceMetadata:
			patchArgs, err = p.prepareReplacement(ctx, conn, *patch)
			if err == nil {
				blobPatchArgs, err = p.prepareBlobReplacement(ctx, conn, *patch, patchArgs)
			}
		default:
			err = uploadquota.ErrInvalid
		}
		if err != nil {
			return uploadquota.Reservation{}, false, err
		}
	}
	next, record, file, err := mutate(conn, ledger)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	if patch != nil && file != nil {
		return uploadquota.Reservation{}, false, uploadquota.ErrInvalid
	}
	encoded, err := uploadquota.Encode(next)
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	var metadataArgs redigo.Args
	hotlinkKey := ""
	if file != nil {
		// Also watch the target itself: an old or external writer may change its
		// type without participating in our metadata revision protocol.
		if _, err = redigo.DoContext(conn, ctx, "WATCH", p.dbPrefix+prefixMetaData+file.Id); err != nil {
			return uploadquota.Reservation{}, false, err
		}
		// Apply requires a new ID. Check the Redis type as well before MULTI;
		// command failures inside EXEC do not roll back preceding commands.
		kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", p.dbPrefix+prefixMetaData+file.Id))
		if err != nil {
			return uploadquota.Reservation{}, false, err
		}
		if kind != "none" {
			return uploadquota.Reservation{}, false, uploadquota.ErrConflict
		}
		marshalled, err := marshalEncryptionInfo(*file)
		if err != nil {
			return uploadquota.Reservation{}, false, err
		}
		metadataArgs = p.buildArgs(prefixMetaData + file.Id).AddFlat(marshalled)
		if file.HotlinkId != "" {
			hotlinkKey = p.dbPrefix + prefixHotlinks + file.HotlinkId
			if _, err = redigo.DoContext(conn, ctx, "WATCH", hotlinkKey); err != nil {
				return uploadquota.Reservation{}, false, err
			}
			kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", hotlinkKey))
			if err != nil {
				return uploadquota.Reservation{}, false, err
			}
			switch kind {
			case "none":
			case "string":
				// File IDs are bounded by the ledger. Reading one extra byte
				// rejects an overlong target without loading an unbounded value.
				target, err := redigo.String(redigo.DoContext(conn, ctx, "GETRANGE", hotlinkKey, 0, uploadquota.MaxIDLength))
				if err != nil {
					return uploadquota.Reservation{}, false, err
				}
				if target != file.Id {
					return uploadquota.Reservation{}, false, uploadquota.ErrConflict
				}
			default:
				return uploadquota.Reservation{}, false, uploadquota.ErrConflict
			}
		}
	}
	if _, err = redigo.DoContext(conn, ctx, "MULTI"); err != nil {
		return uploadquota.Reservation{}, false, err
	}
	multi = true
	// Revision changes first so any partial runtime failure still invalidates
	// other snapshots. EXEC errors are returned, never reported as a rollback.
	if _, err = redigo.DoContext(conn, ctx, "SET", p.dbPrefix+keyMetadataRevision, rand.Text()); err != nil {
		return uploadquota.Reservation{}, false, err
	}
	if patch != nil && patch.Action == uploadquota.TransferOwner {
		// A transfer changes one field of the current hash. Replacing a stale
		// File would overwrite unrelated concurrent edits or download counters.
		if _, err = redigo.DoContext(conn, ctx, "HSET", p.dbPrefix+prefixMetaData+patch.Source.Id, "UserId", patch.Upload.UserID); err != nil {
			return uploadquota.Reservation{}, false, err
		}
	}
	if patch != nil && patch.Action == uploadquota.ReplaceMetadata {
		// EVAL is one queued operation; do not split registration and patch into
		// independently failing EXEC commands. All expected failures are checked
		// before MULTI and the script checks types/capacity again before writing.
		if _, err = redigo.DoContext(conn, ctx, "EVAL", blobPatchArgs...); err != nil {
			return uploadquota.Reservation{}, false, err
		}
	}
	if file != nil {
		if _, err = redigo.DoContext(conn, ctx, "HMSET", metadataArgs...); err != nil {
			return uploadquota.Reservation{}, false, err
		}
	}
	if hotlinkKey != "" {
		if _, err = redigo.DoContext(conn, ctx, "SET", hotlinkKey, file.Id); err != nil {
			return uploadquota.Reservation{}, false, err
		}
	}
	if _, err = redigo.DoContext(conn, ctx, "SET", p.dbPrefix+keyUploadQuota, encoded); err != nil {
		return uploadquota.Reservation{}, false, err
	}
	reply, err := redigo.DoContext(conn, ctx, "EXEC")
	if err != nil {
		return uploadquota.Reservation{}, false, err
	}
	multi = false
	if reply == nil {
		return uploadquota.Reservation{}, true, nil
	}
	expected := 2
	if patch != nil {
		expected++
	}
	if file != nil {
		expected++
	}
	if hotlinkKey != "" {
		expected++
	}
	if err = checkQuotaExec(reply, expected); err != nil {
		if patch != nil && patch.Action == uploadquota.ReplaceMetadata {
			err = blobCandidateRedisError(ctx, err)
		}
		return uploadquota.Reservation{}, false, err
	}
	return record, false, nil
}

func checkQuotaExec(reply any, expected int) error {
	values, err := redigo.Values(reply, nil)
	if err != nil {
		return err
	}
	if len(values) != expected {
		return uploadquota.ErrInvalid
	}
	for _, value := range values {
		if commandErr, ok := value.(redigo.Error); ok {
			return commandErr
		}
	}
	return nil
}

func (p DatabaseProvider) readUploadQuota(ctx context.Context, conn redigo.Conn) (uploadquota.Ledger, error) {
	kinds := make([]string, 0, 2)
	for _, key := range []string{keyUploadQuota, keyMetadataRevision} {
		kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", p.dbPrefix+key))
		if err != nil {
			return uploadquota.Ledger{}, err
		}
		kinds = append(kinds, kind)
	}
	if kinds[0] == "none" && kinds[1] == "none" {
		return uploadquota.Ledger{}, errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
	}
	if kinds[0] != "string" || kinds[1] != "string" {
		return uploadquota.Ledger{}, uploadquota.ErrSchema
	}
	length, err := redigo.Int(redigo.DoContext(conn, ctx, "STRLEN", p.dbPrefix+keyUploadQuota))
	if err != nil {
		return uploadquota.Ledger{}, err
	}
	if length == 0 {
		exists, err := redigo.Int(redigo.DoContext(conn, ctx, "EXISTS", p.dbPrefix+keyUploadQuota))
		if err != nil {
			return uploadquota.Ledger{}, err
		}
		if exists == 0 {
			return uploadquota.Ledger{}, p.missingUploadQuota(ctx, conn)
		}
	}
	if length <= 0 || length > uploadquota.MaxLedgerBytes {
		return uploadquota.Ledger{}, uploadquota.ErrInvalid
	}
	// Bound the actual reply as well as STRLEN: a corrupt external writer could
	// replace the value between the length check and this command. Decode rejects
	// the extra byte when the stored ledger exceeds the maximum.
	data, err := redigo.Bytes(redigo.DoContext(conn, ctx, "GETRANGE", p.dbPrefix+keyUploadQuota, 0, uploadquota.MaxLedgerBytes))
	if errors.Is(err, redigo.ErrNil) {
		return uploadquota.Ledger{}, p.missingUploadQuota(ctx, conn)
	}
	if err != nil {
		return uploadquota.Ledger{}, err
	}
	if len(data) == 0 {
		exists, err := redigo.Int(redigo.DoContext(conn, ctx, "EXISTS", p.dbPrefix+keyUploadQuota))
		if err != nil {
			return uploadquota.Ledger{}, err
		}
		if exists == 0 {
			return uploadquota.Ledger{}, p.missingUploadQuota(ctx, conn)
		}
	}
	return uploadquota.Decode(data)
}

// Only complete absence can represent an old schema without quota structures.
// One missing key is a partial/corrupt upgrade, never an empty upload ledger.
func (p DatabaseProvider) missingUploadQuota(ctx context.Context, conn redigo.Conn) error {
	for _, key := range []string{keyUploadQuota, keyMetadataRevision} {
		kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", p.dbPrefix+key))
		if err != nil {
			return err
		}
		if kind != "none" {
			return uploadquota.ErrSchema
		}
	}
	return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
}

func (p DatabaseProvider) quotaMetadata(ctx context.Context, conn redigo.Conn) (map[string]models.File, error) {
	files := make(map[string]models.File)
	prefix := p.dbPrefix + prefixMetaData
	// Redis prefixes are configuration strings, not glob expressions.
	pattern := strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(prefix) + "*"
	cursor := "0"
	for {
		reply, err := redigo.Values(redigo.DoContext(conn, ctx, "SCAN", cursor, "MATCH", pattern, "COUNT", 100))
		if err != nil {
			return nil, err
		}
		if len(reply) != 2 {
			return nil, uploadquota.ErrInvalid
		}
		cursor, err = redigo.String(reply[0], nil)
		if err != nil {
			return nil, err
		}
		keys, err := redigo.Strings(reply[1], nil)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, prefix) {
				return nil, uploadquota.ErrInvalid
			}
			fields, err := redigo.Values(redigo.DoContext(conn, ctx, "HMGET", key, "UserId", "SizeBytes", "FileRequestId"))
			if err != nil {
				return nil, err
			}
			if len(fields) != 3 {
				return nil, uploadquota.ErrInvalid
			}
			// A concurrent delete invalidates WATCH; retry instead of treating it
			// as zero usage. Missing fields without a write are corrupt metadata.
			owner, ownerErr := redigo.Int(fields[0], nil)
			size, sizeErr := redigo.Int64(fields[1], nil)
			if ownerErr != nil || sizeErr != nil {
				exists, err := redigo.Int(redigo.DoContext(conn, ctx, "EXISTS", key))
				if err != nil {
					return nil, err
				}
				if exists == 0 {
					continue
				}
				return nil, fmt.Errorf("%w: metadata quota fields", uploadquota.ErrInvalid)
			}
			requestID := ""
			if fields[2] != nil {
				requestID, err = redigo.String(fields[2], nil)
				if err != nil {
					return nil, err
				}
			}
			id := strings.TrimPrefix(key, prefix)
			files[id] = models.File{Id: id, UserId: owner, SizeBytes: size, UploadRequestId: requestID}
		}
		if cursor == "0" {
			return files, nil
		}
	}
}

func (p DatabaseProvider) initUploadQuota() error {
	data, err := uploadquota.Encode(uploadquota.EmptyLedger())
	if err != nil {
		return err
	}
	conn := p.pool.Get()
	defer conn.Close()
	if _, err = conn.Do("MULTI"); err != nil {
		return err
	}
	if _, err = conn.Do("SETNX", p.dbPrefix+keyUploadQuota, data); err != nil {
		_, _ = conn.Do("DISCARD")
		return err
	}
	if _, err = conn.Do("SETNX", p.dbPrefix+keyMetadataRevision, rand.Text()); err != nil {
		_, _ = conn.Do("DISCARD")
		return err
	}
	reply, err := conn.Do("EXEC")
	if err != nil {
		return err
	}
	if err = checkQuotaExec(reply, 2); err != nil {
		return err
	}
	_, err = p.readUploadQuota(context.Background(), conn)
	return err
}
