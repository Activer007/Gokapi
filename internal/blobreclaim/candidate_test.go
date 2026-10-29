package blobreclaim

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func TestCandidateRecordBoundary(t *testing.T) {
	for _, locator := range [][2]string{{"abc123", ""}, {"key\x00suffix", "bucket"}, {strings.Repeat("h", 128), strings.Repeat("b", 1024)}} {
		candidate, err := New(locator[0], locator[1])
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := Encode(candidate)
		if err != nil || len(encoded) > MaxRecordBytes {
			t.Fatal("valid record not bounded", err)
		}
		decoded, err := Decode(encoded)
		if err != nil || decoded != candidate {
			t.Fatal("locator/token did not survive", err)
		}
		for _, malformed := range [][]byte{encoded[:len(encoded)-1], append(bytes.Clone(encoded), 0), append([]byte{2}, encoded[1:]...), bytes.Repeat([]byte{0}, MaxRecordBytes+1)} {
			if _, err := Decode(malformed); !errors.Is(err, uploadquota.ErrInvalid) {
				t.Fatal("malformed record accepted")
			}
		}
		refreshed, err := New(locator[0], locator[1])
		if err != nil || refreshed.Key() != candidate.Key() || refreshed.Token == candidate.Token {
			t.Fatal("new intent did not retain locator and refresh token")
		}
	}
	for _, locator := range [][2]string{{"../outside", ""}, {".", ""}, {"", "bucket"}, {strings.Repeat("h", 129), "bucket"}, {"key", strings.Repeat("b", 1025)}} {
		if _, err := New(locator[0], locator[1]); !errors.Is(err, uploadquota.ErrInvalid) {
			t.Fatal("invalid locator accepted")
		}
	}
	first, _ := New("c", "a\x00b")
	second, _ := New("b\x00c", "a")
	if first.Key() == second.Key() {
		t.Fatal("separator alias merged distinct S3 locators")
	}
}
