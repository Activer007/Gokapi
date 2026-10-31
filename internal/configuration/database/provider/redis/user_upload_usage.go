package redis

import (
	"context"
	"errors"
	"time"

	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const userUsageReadAttempts = 8

// ReadUserUploadUsage returns no partial usage. The two existing revision keys
// protect the checked ledger and metadata scan; a successful read never writes
// either key or changes a reservation, including an expired active one.
func (p DatabaseProvider) ReadUserUploadUsage(ctx context.Context, userID int) (uploadquota.Usage, error) {
	if ctx == nil || userID <= 0 {
		return uploadquota.Usage{}, uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if p.pool == nil {
		return uploadquota.Usage{}, uploadquota.ErrSchema
	}
	for attempt := 0; attempt < userUsageReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return uploadquota.Usage{}, err
		}
		usage, retry, err := p.userUploadUsageAttempt(ctx, userID)
		if err != nil || !retry {
			return usage, err
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return uploadquota.Usage{}, ctx.Err()
		case <-timer.C:
		}
	}
	return uploadquota.Usage{}, uploadquota.ErrRetry
}

func (p DatabaseProvider) userUploadUsageAttempt(ctx context.Context, userID int) (uploadquota.Usage, bool, error) {
	conn, err := p.pool.GetContext(ctx)
	if err != nil {
		return uploadquota.Usage{}, false, err
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
	if _, err := redigo.DoContext(conn, ctx, "WATCH", p.dbPrefix+keyUploadQuota, p.dbPrefix+keyMetadataRevision); err != nil {
		return uploadquota.Usage{}, false, err
	}
	if _, err := p.readUploadQuota(ctx, conn); err != nil {
		return uploadquota.Usage{}, false, err
	}
	files, err := p.quotaMetadata(ctx, conn)
	if err != nil {
		return uploadquota.Usage{}, false, err
	}
	snapshot, err := uploadquota.Measure(files)
	if err != nil {
		return uploadquota.Usage{}, false, errors.Join(uploadquota.ErrInvalid, err)
	}
	if _, err := redigo.DoContext(conn, ctx, "MULTI"); err != nil {
		return uploadquota.Usage{}, false, err
	}
	multi = true
	// PING is a read-only transaction member. EXEC validates WATCH without the
	// SET operations used by the separate upload-admission write transaction.
	if _, err := redigo.DoContext(conn, ctx, "PING"); err != nil {
		return uploadquota.Usage{}, false, err
	}
	reply, err := redigo.DoContext(conn, ctx, "EXEC")
	if err != nil {
		return uploadquota.Usage{}, false, err
	}
	multi = false
	if reply == nil {
		return uploadquota.Usage{}, true, nil
	}
	if err := checkQuotaExec(reply, 1); err != nil {
		return uploadquota.Usage{}, false, err
	}
	return snapshot.Users[userID], false, nil
}
