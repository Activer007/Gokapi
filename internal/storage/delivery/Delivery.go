// Package delivery organizes existing public shares without issuing a new
// download permission or changing their metadata, quota, or blob references.
package delivery

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Activer007/SupaShare/internal/authorization"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
)

const MaxFiles = 20

var (
	ErrInvalid         = errors.New("invalid delivery input")
	ErrFileUnavailable = errors.New("delivery requires available ordinary shares owned by the creator")
	ErrNotFound        = errors.New("delivery unavailable")
)

// Input contains only presentation data and existing ordinary file IDs.
type Input struct {
	Title     string   `json:"title"`
	Note      string   `json:"note"`
	FileIDs   []string `json:"fileIds"`
	ExpiresAt int64    `json:"expiresAt"`
}

// PublicFile intentionally excludes ownership, internal storage and encryption
// data. A URL leads to the original password/expiry/count-enforcing download UI.
type PublicFile struct {
	Name             string
	Size             string
	Available        bool
	PasswordRequired bool
	URL              string
}

func ValidToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}

func shareable(file models.File, ownerID int, now int64) bool {
	return ownerID > 0 && file.UserId == ownerID && !file.IsFileRequest() &&
		!file.Encryption.IsEndToEndEncrypted && !file.IsPendingForDeletion() && !storage.IsExpiredFile(file, now)
}

// Create validates every source before inserting one record. Sources can change
// later; PublicFiles reads their current state again on each page request.
func Create(ctx context.Context, ownerID int, input Input) (models.Delivery, error) {
	if ctx == nil || ownerID <= 0 {
		return models.Delivery{}, ErrInvalid
	}
	now := time.Now().Unix()
	input.Title, input.Note = strings.TrimSpace(input.Title), strings.TrimSpace(input.Note)
	if input.Title == "" || utf8.RuneCountInString(input.Title) > 200 || utf8.RuneCountInString(input.Note) > 2000 ||
		len(input.FileIDs) < 2 || len(input.FileIDs) > MaxFiles || input.ExpiresAt < 0 || (input.ExpiresAt > 0 && input.ExpiresAt <= now) {
		return models.Delivery{}, ErrInvalid
	}
	ids := make([]string, 0, len(input.FileIDs))
	seen := make(map[string]bool, len(input.FileIDs))
	for _, id := range input.FileIDs {
		if id == "" || len(id) > 128 || strings.IndexFunc(id, func(c rune) bool {
			return !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-')
		}) >= 0 {
			return models.Delivery{}, ErrInvalid
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		file, found, err := database.ReadRecoveryFile(ctx, id)
		if err != nil {
			return models.Delivery{}, err
		}
		if !found || !shareable(file, ownerID, now) {
			return models.Delivery{}, ErrFileUnavailable
		}
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		return models.Delivery{}, ErrInvalid
	}
	d := models.Delivery{OwnerID: ownerID, Title: input.Title, Note: input.Note, FileIDs: ids, CreatedAt: now, ExpiresAt: input.ExpiresAt}
	// A create-only provider primitive prevents even a token collision from
	// replacing a different owner's capability.
	for range 3 {
		var entropy [32]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return models.Delivery{}, err
		}
		d.Token = base64.RawURLEncoding.EncodeToString(entropy[:])
		inserted, err := database.InsertDelivery(ctx, d)
		if err != nil {
			return models.Delivery{}, err
		}
		if inserted {
			return d, nil
		}
	}
	return models.Delivery{}, errors.New("could not create delivery")
}

// GetPublic does not distinguish unknown, revoked and expired capabilities.
func GetPublic(ctx context.Context, token string) (models.Delivery, error) {
	if ctx == nil || !ValidToken(token) {
		return models.Delivery{}, ErrNotFound
	}
	d, found, err := database.ReadDelivery(ctx, token)
	if err != nil {
		return models.Delivery{}, err
	}
	if !found || d.RevokedAt != 0 || (d.ExpiresAt > 0 && d.ExpiresAt <= time.Now().Unix()) {
		return models.Delivery{}, ErrNotFound
	}
	owner, found := database.GetUser(d.OwnerID)
	if !found || !authorization.CanGrantAPI(owner, models.ApiPermUpload) {
		return models.Delivery{}, ErrNotFound
	}
	return d, nil
}

// PublicFiles never caches authorization or download URLs. A missing source
// leaves an unavailable placeholder, so other list members remain usable.
func PublicFiles(ctx context.Context, d models.Delivery, serverURL string, includeFilename bool) ([]PublicFile, error) {
	if ctx == nil || len(d.FileIDs) > MaxFiles {
		return nil, ErrInvalid
	}
	files := make([]PublicFile, 0, len(d.FileIDs))
	now := time.Now().Unix()
	for _, id := range d.FileIDs {
		file, found, err := database.ReadRecoveryFile(ctx, id)
		if err != nil {
			return nil, err
		}
		item := PublicFile{}
		// Never disclose the new owner's or collection's metadata after a source
		// is reassigned. Only the original owner's ordinary share is projectable.
		if found && file.UserId == d.OwnerID && !file.IsFileRequest() && !file.Encryption.IsEndToEndEncrypted {
			item.Name, item.Size = file.Name, file.Size
			if shareable(file, d.OwnerID, now) {
				info, err := file.ToFileApiOutput(serverURL, includeFilename)
				if err != nil {
					return nil, err
				}
				item.Available, item.PasswordRequired, item.URL = true, file.PasswordHash != "", info.UrlDownload
			}
		}
		files = append(files, item)
	}
	return files, nil
}
