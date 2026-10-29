package redis

import (
	"context"
	"crypto/rand"
	"reflect"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

// ApplyFileState is the checked metadata-only state transition entry point.
// WATCH also covers counters which need not change the global revision. Only
// the selected hash fields are written; unknown fields remain intact.
func (p DatabaseProvider) ApplyFileState(ctx context.Context, patch models.FileStatePatch) (models.File, bool, error) {
	if ctx == nil {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return models.File{}, false, err
	}
	if !validFileStatePatch(patch) {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	if p.pool == nil {
		return models.File{}, false, uploadquota.ErrSchema
	}
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return models.File{}, false, err
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
	metadataKey, revisionKey := p.dbPrefix+prefixMetaData+patch.Expected.Id, p.dbPrefix+keyMetadataRevision
	if _, err := redigo.DoContext(conn, ctx, "WATCH", metadataKey, revisionKey); err != nil {
		return models.File{}, false, err
	}
	current, found, err := p.readRecoveryFileConn(ctx, conn, patch.Expected.Id)
	if err != nil || !found {
		return models.File{}, false, err
	}
	if !reflect.DeepEqual(current, patch.Expected) {
		return models.File{}, false, uploadquota.ErrConflict
	}
	kind, err := redigo.String(redigo.DoContext(conn, ctx, "TYPE", revisionKey))
	if err != nil {
		return models.File{}, false, err
	}
	if kind != "none" && kind != "string" {
		return models.File{}, false, uploadquota.ErrInvalid
	}
	args := redigo.Args{}.Add(metadataKey)
	switch patch.Operation {
	case models.FileStateExpire:
		current.ExpireAt, current.UnlimitedTime, current.PendingDeletion = 0, false, 0
		args = args.Add("ExpireAt", 0, "UnlimitedTime", 0, "PendingDeletion", 0)
	case models.FileStateSchedule:
		current.PendingDeletion = patch.PendingDeletion
		args = args.Add("PendingDeletion", current.PendingDeletion)
	case models.FileStateCancel:
		current.PendingDeletion = 0
		args = args.Add("PendingDeletion", 0)
	case models.FileStatePassword:
		current.PasswordHash = patch.PasswordHash
		args = args.Add("PasswordHash", current.PasswordHash)
	}
	if _, err := redigo.DoContext(conn, ctx, "MULTI"); err != nil {
		return models.File{}, false, err
	}
	multi = true
	if _, err := redigo.DoContext(conn, ctx, "SET", revisionKey, rand.Text()); err != nil {
		return models.File{}, false, err
	}
	if _, err := redigo.DoContext(conn, ctx, "HSET", args...); err != nil {
		return models.File{}, false, err
	}
	reply, err := redigo.DoContext(conn, ctx, "EXEC")
	if err != nil {
		return models.File{}, false, err
	}
	multi = false
	if reply == nil {
		return models.File{}, false, uploadquota.ErrConflict
	}
	if err := checkQuotaExec(reply, 2); err != nil {
		return models.File{}, false, err
	}
	return current, true, nil
}

func validFileStatePatch(patch models.FileStatePatch) bool {
	if !recoveryFileID.MatchString(patch.Expected.Id) {
		return false
	}
	switch patch.Operation {
	case models.FileStateExpire, models.FileStateCancel:
		return patch.PendingDeletion == 0 && patch.PasswordHash == ""
	case models.FileStateSchedule:
		return patch.PendingDeletion > 0 && patch.PasswordHash == ""
	case models.FileStatePassword:
		return patch.PendingDeletion == 0 && patch.PasswordHash != "" && len(patch.PasswordHash) <= uploadquota.MaxMetadataTextBytes
	default:
		return false
	}
}
