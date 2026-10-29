package storage

import (
	"context"
	"time"

	"github.com/Activer007/Gokapi/internal/configuration/database"
	"github.com/Activer007/Gokapi/internal/models"
	"github.com/Activer007/Gokapi/internal/uploadquota"
)

// TransferFileOwnerContext accepts an authorized source snapshot. It creates
// no new blob reference and updates only ownership within quota admission.
func TransferFileOwnerContext(ctx context.Context, source models.File, target int) (models.File, error) {
	if ctx == nil || target <= 0 {
		return models.File{}, uploadquota.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	limits, err := metadataLimits()
	if err != nil {
		return models.File{}, err
	}
	_, err = database.ApplyUploadQuota(ctx, uploadquota.Change{
		Action: uploadquota.TransferOwner, Source: &source,
		Upload: uploadquota.Reservation{ID: source.Id, UserID: target},
		Limits: limits, Now: time.Now().Unix(),
	})
	if err != nil {
		return models.File{}, err
	}
	source.UserId = target
	return source, nil
}
