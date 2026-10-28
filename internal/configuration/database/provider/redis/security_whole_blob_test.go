//go:build test

package redis

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/forceu/gokapi/internal/models"
	"github.com/forceu/gokapi/internal/uploadquota"
)

func sec030RedisBlobGob(t *testing.T, values ...models.EncryptionInfo) []byte {
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

func TestSEC030WholeBlobRedisLocatorAndReferences(t *testing.T) {
	s := sec030RedisNew(t)
	ctx := context.Background()
	local := models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{1}, 32), Nonce: []byte{1, 2}}
	remote := models.EncryptionInfo{IsEncrypted: true, DecryptionKey: bytes.Repeat([]byte{2}, 32), Nonce: []byte{3, 4}}
	for _, file := range []models.File{
		{Id: "local-a", UserId: 1, SHA1: "shared", Encryption: local},
		{Id: "local-b", UserId: 2, SHA1: "shared", Encryption: local},
		{Id: "remote", UserId: 1, SHA1: "shared", AwsBucket: "bucket-a", Encryption: remote},
	} {
		s.db.SaveMetaData(file)
	}
	for _, tc := range []struct {
		bucket string
		want   models.EncryptionInfo
	}{{"", local}, {"bucket-a", remote}} {
		got, found, err := s.db.ReadBlobEncryption(ctx, "shared", tc.bucket)
		if err != nil || !found || !reflect.DeepEqual(got, tc.want) {
			t.Fatal("blob lookup mixed storage buckets or disagreed with identical references", err)
		}
	}
	for _, locator := range [][2]string{{"missing", ""}, {"shared", "other-bucket"}} {
		got, found, err := s.db.ReadBlobEncryption(ctx, locator[0], locator[1])
		if err != nil || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
			t.Fatal("missing blob locator returned encryption information", err)
		}
	}
	s.db.SaveMetaData(models.File{Id: "conflicting", UserId: 3, SHA1: "shared", Encryption: remote})
	got, found, err := s.db.ReadBlobEncryption(ctx, "shared", "")
	if !errors.Is(err, uploadquota.ErrConflict) || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
		t.Fatal("conflicting blob references did not fail closed")
	}
}

func TestSEC030WholeBlobRedisBoundedProjectionAndField(t *testing.T) {
	s := sec030RedisNew(t)
	want := models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1, 2, 3}}
	key := s.config.RedisPrefix + prefixMetaData + "projection"
	sec030RedisDo(t, s.conn, "HSET", key, "SHA1", "projection-blob", "AwsBucket", "",
		"EncryptionRedis", sec030RedisBlobGob(t, want), "Encryption", "synthetic-invalid-wrong-field",
		"Name", strings.Repeat("n", 1048576), "PasswordHash", strings.Repeat("p", 1048576),
		"SizeBytes", "synthetic-not-an-integer")
	got, found, err := s.db.ReadBlobEncryption(context.Background(), "projection-blob", "")
	if err != nil || !found || !reflect.DeepEqual(got, want) {
		t.Fatal("lookup used the wrong Redis encryption field or full metadata projection", err)
	}
}

func TestSEC030WholeBlobRedisRejectsCorruptEncoding(t *testing.T) {
	want := models.EncryptionInfo{IsEncrypted: true, Nonce: []byte{1}}
	valid := sec030RedisBlobGob(t, want)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"corrupt", []byte{0xff, 0x01}},
		{"oversized", bytes.Repeat([]byte{1}, 4097)},
		{"trailing-junk", append(append([]byte{}, valid...), 0xff)},
		{"trailing-zero", append(append([]byte{}, valid...), 0)},
		{"second-value", sec030RedisBlobGob(t, want, want)},
		{"oversized-key", sec030RedisBlobGob(t, models.EncryptionInfo{DecryptionKey: bytes.Repeat([]byte{1}, 129)})},
		{"oversized-nonce", sec030RedisBlobGob(t, models.EncryptionInfo{Nonce: bytes.Repeat([]byte{1}, 129)})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sec030RedisNew(t)
			key := s.config.RedisPrefix + prefixMetaData + "bad-encoding"
			sec030RedisDo(t, s.conn, "HSET", key, "SHA1", "bad-blob", "AwsBucket", "", "EncryptionRedis", tc.data)
			got, found, err := s.db.ReadBlobEncryption(context.Background(), "bad-blob", "")
			if err == nil || found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
				t.Fatal("malformed encryption metadata returned usable information")
			}
		})
	}
}

func TestSEC030WholeBlobRedisLegacyUnencryptedField(t *testing.T) {
	s := sec030RedisNew(t)
	key := s.config.RedisPrefix + prefixMetaData + "legacy"
	sec030RedisDo(t, s.conn, "HSET", key, "SHA1", "legacy-blob", "AwsBucket", "")
	// Existing Redis metadata decoding treats an absent EncryptionRedis field
	// as plaintext. This is a compatibility characterization, not a claim that
	// the getter authenticates malformed historical encryption metadata.
	got, found, err := s.db.ReadBlobEncryption(context.Background(), "legacy-blob", "")
	if err != nil || !found || !reflect.DeepEqual(got, models.EncryptionInfo{}) {
		t.Fatal("checked lookup changed the existing absent-field plaintext convention", err)
	}
}

func TestSEC030WholeBlobRedisInputAndCancellation(t *testing.T) {
	s := sec030RedisNew(t)
	for _, tc := range []struct {
		ctx          context.Context
		hash, bucket string
	}{{nil, "hash", ""}, {context.Background(), "", ""},
		{context.Background(), strings.Repeat("h", 129), ""},
		{context.Background(), "hash", strings.Repeat("b", 1025)}} {
		if _, found, err := s.db.ReadBlobEncryption(tc.ctx, tc.hash, tc.bucket); !errors.Is(err, uploadquota.ErrInvalid) || found {
			t.Fatal("invalid blob locator was not rejected")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, found, err := s.db.ReadBlobEncryption(ctx, "hash", ""); !errors.Is(err, context.Canceled) || found {
		t.Fatal("canceled blob lookup did not preserve context cancellation")
	}
}

func TestSEC030WholeBlobRedisUnavailableProvider(t *testing.T) {
	defer func() {
		if recover() != nil {
			t.Fatal("uninitialized Redis provider panicked instead of returning a schema error")
		}
	}()
	if _, found, err := (DatabaseProvider{}).ReadBlobEncryption(context.Background(), "hash", ""); !errors.Is(err, uploadquota.ErrSchema) || found {
		t.Fatal("uninitialized Redis provider did not fail closed")
	}
}
