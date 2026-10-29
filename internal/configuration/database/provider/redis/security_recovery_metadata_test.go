//go:build test && security_recovery

package redis

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

func TestSEC030RecoveryRedisReadMetadata(t *testing.T) {
	s := sec030RedisNew(t)
	file := models.File{Id: "recovery-file", UserId: 1, Name: "synthetic.bin", Size: "3 B", SizeBytes: 3, SHA1: "synthetic-blob", PasswordHash: "synthetic-hash", ContentType: "application/octet-stream", UnlimitedTime: true, Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2}}}
	s.db.SaveMetaData(file)
	if got, found, err := s.db.ReadRecoveryFile(context.Background(), file.Id); err != nil || !found || !reflect.DeepEqual(got, file) {
		t.Fatal("full recovery metadata changed", err)
	}
	if _, found, err := s.db.ReadRecoveryFile(context.Background(), "missing"); err != nil || found {
		t.Fatal("missing recovery metadata was not empty", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, found, err := s.db.ReadRecoveryFile(ctx, file.Id); !errors.Is(err, context.Canceled) || found {
		t.Fatal("cancelled recovery read was accepted", err)
	}
	if _, _, err := s.db.ReadRecoveryFile(nil, file.Id); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("nil recovery context was accepted", err)
	}
	sec030RedisDo(t, s.conn, "HSET", s.config.RedisPrefix+prefixMetaData+file.Id, "EncryptionRedis", []byte("invalid-gob"))
	if _, found, err := s.db.ReadRecoveryFile(context.Background(), file.Id); !errors.Is(err, uploadquota.ErrInvalid) || found {
		t.Fatal("corrupt recovery encryption was accepted", err)
	}
}
