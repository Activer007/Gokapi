//go:build test && security_recovery

package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestSEC030RecoverySQLiteReadMetadata(t *testing.T) {
	p := newQuotaDatabase(t)
	file := models.File{Id: "recovery-file", UserId: 1, Name: "synthetic.bin", Size: "3 B", SizeBytes: 3, SHA1: "synthetic-blob", PasswordHash: "synthetic-hash", ContentType: "application/octet-stream", UnlimitedTime: true, Encryption: models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2}}}
	p.SaveMetaData(file)
	if got, found, err := p.ReadRecoveryFile(context.Background(), file.Id); err != nil || !found || !reflect.DeepEqual(got, file) {
		t.Fatal("full recovery metadata changed", err)
	}
	if _, found, err := p.ReadRecoveryFile(context.Background(), "missing"); err != nil || found {
		t.Fatal("missing recovery metadata was not empty", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, found, err := p.ReadRecoveryFile(ctx, file.Id); !errors.Is(err, context.Canceled) || found {
		t.Fatal("cancelled recovery read was accepted", err)
	}
	if _, _, err := p.ReadRecoveryFile(nil, file.Id); !errors.Is(err, uploadquota.ErrInvalid) {
		t.Fatal("nil recovery context was accepted", err)
	}
	if _, err := p.sqliteDb.Exec("UPDATE FileMetaData SET Encryption = ? WHERE Id = ?", []byte("invalid-gob"), file.Id); err != nil {
		t.Fatal(err)
	}
	if _, found, err := p.ReadRecoveryFile(context.Background(), file.Id); !errors.Is(err, uploadquota.ErrInvalid) || found {
		t.Fatal("corrupt recovery encryption was accepted", err)
	}
}
