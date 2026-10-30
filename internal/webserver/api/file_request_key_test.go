//go:build test && !integration

package api

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestFileRequestKeyRetriesCollisionsAndRetainsScope(t *testing.T) {
	attempts := 0
	key, err := createFileRequestKey(5, "collection", func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(attempts + 1)
		}
		return len(b), nil
	}, func(key models.ApiKey) (bool, error) {
		attempts++
		return attempts == 2, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(key.Id)
	if err != nil || len(decoded) != 16 || len(key.Id) != 22 || attempts != 2 ||
		key.UserId != 5 || key.UploadRequestId != "collection" || key.Permissions != models.ApiPermNone {
		t.Fatalf("invalid collection credential: key=%+v attempts=%d err=%v", key, attempts, err)
	}
	ordinary := generateNewKey(false, idUser, "ordinary length test", "")
	t.Cleanup(func() { database.DeleteApiKey(ordinary.Id) })
	if len(ordinary.Id) != 30 {
		t.Fatal("ordinary API key length changed")
	}
}

func TestFileRequestKeyCreationFailsClosed(t *testing.T) {
	for _, failure := range []string{"random", "storage", "collisions"} {
		t.Run(failure, func(t *testing.T) {
			inserts := 0
			key, err := createFileRequestKey(5, "collection", func(b []byte) (int, error) {
				if failure == "random" {
					return 0, errors.New("random unavailable")
				}
				return len(b), nil
			}, func(models.ApiKey) (bool, error) {
				inserts++
				if failure == "storage" {
					return false, errors.New("storage unavailable")
				}
				return false, nil
			})
			if err == nil || key.Id != "" || (failure == "random" && inserts != 0) || inserts > 5 {
				t.Fatalf("credential creation did not fail closed: key=%+v err=%v inserts=%d", key, err, inserts)
			}
		})
	}
}
