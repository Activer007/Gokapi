package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
)

// Collection credentials carry 128 random bits in a 22-character URL-safe token.
// Ordinary API credentials retain LengthApiKey and their existing behavior.
func generateFileRequestKey(userID int, requestID string) (models.ApiKey, error) {
	return createFileRequestKey(userID, requestID, rand.Read, database.InsertApiKey)
}

func createFileRequestKey(userID int, requestID string, random func([]byte) (int, error), insert func(models.ApiKey) (bool, error)) (models.ApiKey, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var bytes [16]byte
		if n, err := random(bytes[:]); err != nil {
			return models.ApiKey{}, err
		} else if n != len(bytes) {
			return models.ApiKey{}, io.ErrUnexpectedEOF
		}
		key := models.ApiKey{Id: base64.RawURLEncoding.EncodeToString(bytes[:]),
			PublicId: helper.GenerateRandomString(LengthPublicId), FriendlyName: "File Request Public Access",
			UserId: userID, UploadRequestId: requestID, Permissions: models.ApiPermNone}
		created, err := insert(key)
		if err != nil {
			return models.ApiKey{}, err
		}
		if created {
			return key, nil
		}
	}
	return models.ApiKey{}, errors.New("unable to allocate file request credential")
}
