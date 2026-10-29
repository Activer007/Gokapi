// Package blobreclaim defines the small durable locator record used by replacement
// and GC. It contains no share identity, credentials or encryption material.
package blobreclaim

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

const (
	MaxCandidates  = 4096
	MaxRecordBytes = 1 + 2 + 2 + 128 + 1024 + 16
	recordVersion  = 1
)

// Candidate is an intent to inspect one old locator, not permission to delete
// it. A new token replaces the previous intent even for the same locator.
type Candidate struct {
	SHA1      string
	AwsBucket string
	Token     string
}

func New(hash, bucket string) (Candidate, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return Candidate{}, err
	}
	candidate := Candidate{SHA1: hash, AwsBucket: bucket, Token: hex.EncodeToString(token[:])}
	if err := candidate.Validate(); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}

func (candidate Candidate) Validate() error {
	if len(candidate.SHA1) == 0 || len(candidate.SHA1) > 128 || len(candidate.AwsBucket) > 1024 ||
		(candidate.AwsBucket == "" && !helper.IsSafeLocalBlobName(candidate.SHA1)) || len(candidate.Token) != 32 {
		return uploadquota.ErrInvalid
	}
	token, err := hex.DecodeString(candidate.Token)
	if err != nil || len(token) != 16 || hex.EncodeToString(token) != candidate.Token {
		return uploadquota.ErrInvalid
	}
	return nil
}

// Key deduplicates by the exact bucket/hash tuple, without ambiguous separators.
// It is only a record identity, not a digest of file contents.
func (candidate Candidate) Key() string {
	encoded := make([]byte, 4+len(candidate.SHA1)+len(candidate.AwsBucket))
	binary.BigEndian.PutUint16(encoded, uint16(len(candidate.SHA1)))
	binary.BigEndian.PutUint16(encoded[2:], uint16(len(candidate.AwsBucket)))
	copy(encoded[4:], candidate.SHA1)
	copy(encoded[4+len(candidate.SHA1):], candidate.AwsBucket)
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

// Encode uses length-prefixed bytes so S3 keys retain their exact identity. The
// fixed token and checked lengths bound both persistent and decoded memory.
func Encode(candidate Candidate) ([]byte, error) {
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	token, _ := hex.DecodeString(candidate.Token)
	encoded := make([]byte, 5+len(candidate.SHA1)+len(candidate.AwsBucket)+len(token))
	encoded[0] = recordVersion
	binary.BigEndian.PutUint16(encoded[1:], uint16(len(candidate.SHA1)))
	binary.BigEndian.PutUint16(encoded[3:], uint16(len(candidate.AwsBucket)))
	copy(encoded[5:], candidate.SHA1)
	copy(encoded[5+len(candidate.SHA1):], candidate.AwsBucket)
	copy(encoded[len(encoded)-16:], token)
	return encoded, nil
}

func Decode(encoded []byte) (Candidate, error) {
	if len(encoded) < 22 || len(encoded) > MaxRecordBytes || encoded[0] != recordVersion {
		return Candidate{}, uploadquota.ErrInvalid
	}
	hashLength := int(binary.BigEndian.Uint16(encoded[1:]))
	bucketLength := int(binary.BigEndian.Uint16(encoded[3:]))
	if hashLength == 0 || hashLength > 128 || bucketLength > 1024 || len(encoded) != 5+hashLength+bucketLength+16 {
		return Candidate{}, uploadquota.ErrInvalid
	}
	candidate := Candidate{SHA1: string(encoded[5 : 5+hashLength]), AwsBucket: string(encoded[5+hashLength : len(encoded)-16]),
		Token: hex.EncodeToString(encoded[len(encoded)-16:])}
	if err := candidate.Validate(); err != nil {
		return Candidate{}, err
	}
	return candidate, nil
}
