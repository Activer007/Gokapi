//go:build test

package sqlite

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

func sec030SQLiteBlobGob(t *testing.T, values ...models.EncryptionInfo) []byte {
	t.Helper()
	var data bytes.Buffer
	encoder := gob.NewEncoder(&data)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	return data.Bytes()
}

func TestSEC030WholeBlobSQLiteLocatorAndReferences(t *testing.T) {
	db := newQuotaDatabase(t)
	ctx := context.Background()
	local := models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{1}, 32), Nonce: []byte{1, 2}}
	remote := models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{2}, 32), Nonce: []byte{3, 4}}
	for _, file := range []models.File{
		{Id: "local-a", UserId: 1, SHA1: "shared", Encryption: local},
		{Id: "local-b", UserId: 2, SHA1: "shared", Encryption: local},
		{Id: "remote", UserId: 1, SHA1: "shared", AwsBucket: "bucket-a", Encryption: remote},
	} {
		db.SaveMetaData(file)
	}
	for _, tc := range []struct {
		bucket string
		want   models.EncryptionInfo
	}{{"", local}, {"bucket-a", remote}} {
		got, found, err := db.ReadBlobEncryption(ctx, "shared", tc.bucket)
		if err != nil || !found || !reflect.DeepEqual(got, tc.want) {
			t.Fatal("blob lookup mixed storage buckets or disagreed with identical references", err)
		}
	}
	for _, locator := range [][2]string{{"missing", ""}, {"shared", "other-bucket"}} {
		got, found, err := db.ReadBlobEncryption(ctx, locator[0], locator[1])
		if err != nil || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
			t.Fatal("missing blob locator returned encryption information", err)
		}
	}
	db.SaveMetaData(models.File{Id: "conflicting", UserId: 3, SHA1: "shared", Encryption: remote})
	got, found, err := db.ReadBlobEncryption(ctx, "shared", "")
	if !errors.Is(err, uploadquota.ErrConflict) || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
		t.Fatal("conflicting blob references did not fail closed")
	}
}

func TestSEC030WholeBlobSQLiteBoundedProjection(t *testing.T) {
	db := newQuotaDatabase(t)
	want := models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2, 3}}
	db.SaveMetaData(models.File{Id: "projection", UserId: 1, SHA1: "projection-blob", Encryption: want})
	// These unrelated columns are deliberately large or not decodable as File.
	// A full metadata scan would fail; the encryption projection must not need it.
	if _, err := db.sqliteDb.Exec(`UPDATE FileMetaData SET Name = zeroblob(1048576),
		PasswordHash = zeroblob(1048576), SizeBytes = 'synthetic-not-an-integer' WHERE Id = 'projection'`); err != nil {
		t.Fatal(err)
	}
	got, found, err := db.ReadBlobEncryption(context.Background(), "projection-blob", "")
	if err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Fatal("encryption lookup loaded or parsed unrelated metadata fields", err)
	}
}

func TestSEC030WholeBlobSQLiteRejectsCorruptEncoding(t *testing.T) {
	want := models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1}}
	valid := sec030SQLiteBlobGob(t, want)
	for _, tc := range []struct {
		name string
		data any
	}{
		{"empty", []byte{}},
		{"corrupt", []byte{0xff, 0x01}},
		{"oversized-blob", bytes.Repeat([]byte{1}, 4097)},
		{"oversized-text-with-nul", string(append([]byte{'x', 0}, bytes.Repeat([]byte{'z'}, 8192)...))},
		{"trailing-junk", append(append([]byte{}, valid...), 0xff)},
		{"trailing-zero", append(append([]byte{}, valid...), 0)},
		{"second-value", sec030SQLiteBlobGob(t, want, want)},
		{"oversized-key", sec030SQLiteBlobGob(t, models.EncryptionInfo{DecryptionKey: bytes.Repeat([]byte{1}, 129)})},
		{"oversized-nonce", sec030SQLiteBlobGob(t, models.EncryptionInfo{Nonce: bytes.Repeat([]byte{1}, 129)})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newQuotaDatabase(t)
			db.SaveMetaData(models.File{Id: "bad-encoding", UserId: 1, SHA1: "bad-blob"})
			if _, err := db.sqliteDb.Exec("UPDATE FileMetaData SET Encryption = ? WHERE Id = ?", tc.data, "bad-encoding"); err != nil {
				t.Fatal(err)
			}
			got, found, err := db.ReadBlobEncryption(context.Background(), "bad-blob", "")
			if err == nil || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
				t.Fatal("malformed encryption metadata returned usable information")
			}
		})
	}
}

func TestSEC030WholeBlobSQLiteInputAndCancellation(t *testing.T) {
	db := newQuotaDatabase(t)
	for _, tc := range []struct {
		ctx          context.Context
		hash, bucket string
	}{{nil, "hash", ""}, {context.Background(), "", ""},
		{context.Background(), strings.Repeat("h", 129), ""},
		{context.Background(), "hash", strings.Repeat("b", 1025)}} {
		if _, found, err := db.ReadBlobEncryption(tc.ctx, tc.hash, tc.bucket); !errors.Is(err, uploadquota.ErrInvalid) || found {
			t.Fatal("invalid blob locator was not rejected")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, found, err := db.ReadBlobEncryption(ctx, "hash", ""); !errors.Is(err, context.Canceled) || found {
		t.Fatal("canceled blob lookup did not preserve context cancellation")
	}
	if _, found, err := (DatabaseProvider{}).ReadBlobEncryption(context.Background(), "hash", ""); !errors.Is(err, uploadquota.ErrSchema) || found {
		t.Fatal("uninitialized SQLite provider did not fail closed")
	}
}
